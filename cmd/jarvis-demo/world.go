package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The dummy house. Sensors drift with the hour so a briefing at 07:00 and one at
// 22:00 don't read identically; devices are a state map; /notify is the sink
// Jarvis speaks into when nobody asked it anything.

type house struct {
	Sensors map[string]string `json:"sensors"`
	Devices map[string]string `json:"devices"`
}

type world struct {
	mu      sync.Mutex
	devices map[string]string
	said    []string // every /notify text, in order — the sink a test reads
	offset  float64  // sensor calibration, BIG_BRAIN_DEMO_TEMP_OFFSET
	now     func() time.Time
}

func newWorld() *world {
	return &world{
		devices: map[string]string{
			"porch light":  "off",
			"living light": "off",
			"heater":       "off",
			"front lock":   "unlocked",
			"fan":          "off",
			"thermostat":   "21",
		},
		// ponytail: a real thermistor reads a degree or two off and every house is
		// different — the knob stays even though the model here is arithmetic.
		offset: envFloat("BIG_BRAIN_DEMO_TEMP_OFFSET", 0),
		now:    time.Now,
	}
}

// heard is a snapshot of every /notify text received so far, in order — what a
// test reads to assert Jarvis actually spoke into the house.
func (w *world) heard() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.said...)
}

// handler is the dummy house's HTTP surface. Callers decide how to serve it:
// main wraps it in a fixed-address *http.Server, a test in an httptest.Server.
func (w *world) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /house", func(rw http.ResponseWriter, r *http.Request) {
		writeJSON(rw, w.snapshot())
	})
	mux.HandleFunc("GET /sensor/{name}", func(rw http.ResponseWriter, r *http.Request) {
		v, ok := w.snapshot().Sensors[r.PathValue("name")]
		if !ok {
			http.Error(rw, "unknown sensor", http.StatusNotFound)
			return
		}
		fmt.Fprint(rw, v)
	})
	mux.HandleFunc("POST /device/{name}", func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.State == "" {
			http.Error(rw, "want {\"state\":...}", http.StatusBadRequest)
			return
		}
		name := r.PathValue("name")
		w.mu.Lock()
		_, known := w.devices[name]
		if known {
			w.devices[name] = body.State
		}
		w.mu.Unlock()
		if !known {
			http.Error(rw, "unknown device", http.StatusNotFound)
			return
		}
		fmt.Fprintf(os.Stderr, "🏠 %s → %s\n", name, body.State)
		fmt.Fprint(rw, "ok")
	})
	mux.HandleFunc("POST /notify", func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		text := strings.TrimSpace(body.Text)
		w.mu.Lock()
		w.said = append(w.said, text)
		w.mu.Unlock()
		fmt.Fprintf(os.Stderr, "🔔 %s\n", text)
		fmt.Fprint(rw, "ok")
	})

	return mux
}

// snapshot derives the sensor readings from the clock and the device states: the
// heater warms the house, an open window cools it, the sun does the rest.
func (w *world) snapshot() house {
	w.mu.Lock()
	defer w.mu.Unlock()

	t := w.now()
	hour := float64(t.Hour()) + float64(t.Minute())/60
	// Coolest around 05:00, warmest around 17:00.
	temp := 20 + 3*math.Sin((hour-11)/24*2*math.Pi) + w.offset
	if w.devices["heater"] == "on" {
		temp += 2.5
	}
	if w.devices["fan"] == "on" {
		temp -= 1.0
	}
	humidity := 52 - (temp - 20)
	light := "dark"
	if hour > 6.5 && hour < 20.5 {
		light = "bright"
	}
	motion := "none"
	if hour > 7 && hour < 23 {
		motion = "detected in living room"
	}
	door := "closed"
	if w.devices["front lock"] == "unlocked" && hour > 8 && hour < 9 {
		door = "ajar"
	}

	devices := make(map[string]string, len(w.devices))
	for k, v := range w.devices {
		devices[k] = v
	}
	return house{
		Sensors: map[string]string{
			"temperature": fmt.Sprintf("%.1f°C", temp),
			"humidity":    fmt.Sprintf("%.0f%%", humidity),
			"door":        door,
			"motion":      motion,
			"daylight":    light,
			"power":       fmt.Sprintf("%d W", 120+340*countOn(devices)),
		},
		Devices: devices,
	}
}

func countOn(devices map[string]string) int {
	n := 0
	for name, state := range devices {
		if name != "thermostat" && (state == "on" || state == "locked") {
			n++
		}
	}
	return n
}

// --- the client side: how the brain touches the house ---

type client struct {
	base string
	http *http.Client
}

func (c *client) snapshot(ctx context.Context) (house, error) {
	var h house
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/house", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return h, fmt.Errorf("read house: %w", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return h, fmt.Errorf("decode house: %w", err)
	}
	return h, nil
}

func (c *client) set(ctx context.Context, device, stateVal string) error {
	body, _ := json.Marshal(map[string]string{"state": stateVal})
	return c.post(ctx, "/device/"+url.PathEscape(device), body)
}

func (c *client) notify(ctx context.Context, text string) error {
	body, _ := json.Marshal(map[string]string{"text": text})
	return c.post(ctx, "/notify", body)
}

func (c *client) post(ctx context.Context, path string, body []byte) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("post %s: %s", path, resp.Status)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func envFloat(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return def
}
