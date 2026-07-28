// Command jarvis-demo is a home assistant built on pkg/bb, complete enough to
// actually live with: it routes what you say to a capability, controls a house,
// keeps facts and lists across restarts, takes reminders, and runs its own
// morning and goodnight routines without being asked.
//
//	go run ./cmd/jarvis-demo
//
// Then point any OpenAI-compatible client at http://localhost:8080/v1. A dummy
// house runs in-process on :8090, so nothing external is needed. Set
// BIG_BRAIN_API_KEY (plus BIG_BRAIN_BASE_URL / BIG_BRAIN_MODEL) to back it with
// a real model; without one the language flows reply with a canned line and
// every capability falls back to keyword understanding. BIG_BRAIN_DATA=<dir>
// makes memory and reminders survive a restart.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/force1267/big-brain/pkg/bb"
)

// Capability ids: the router's vocabulary and the Select group's membership.
const (
	idTalk     = "talk"
	idRemember = "remember"
	idForget   = "forget"
	idRecall   = "recall"
	idList     = "list"
	idHouse    = "house"
	idBriefing = "briefing"
	idRemind   = "remind"
)

// Deferred bodies. A scheduled flow needs a name to resolve against after a
// restart, so these ids are part of the wiring, not decoration.
const (
	idMorning   = "routine/morning"
	idGoodnight = "routine/goodnight"
	idSweep     = "routine/reminder-sweep"
	idBoot      = "routine/boot"
)

// Model roles. Flow code names a role; deployment decides what backs it.
const (
	mSmart = "smart" // conversation, summaries — the voice of the house
	mFast  = "fast"  // routing and extraction — small, cold, structured
)

const worldAddr = "127.0.0.1:8090"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	w := newWorld()
	houseSrv := &http.Server{Addr: worldAddr, Handler: w.handler(), ReadHeaderTimeout: 5 * time.Second}
	go houseSrv.ListenAndServe()
	defer houseSrv.Shutdown(context.Background())

	registerModels()

	store := openStore()
	j := &jarvis{
		house: &client{base: "http://" + worldAddr, http: &http.Client{Timeout: 5 * time.Second}},
		mem:   openMemory(ctx, store),
	}

	// One request: understand it, do the one thing it asked for, answer, then
	// speak the answer into the house's notification sink.
	brain := j.route().
		Next(bb.Select(
			j.talk(),
			j.remember(),
			j.forget(),
			j.recall(),
			j.lists(),
			j.control(),
			j.briefing(),
			j.remind(),
		).WithModel(bb.NewModel(mFast)).WithId("capabilities")).
		Next(bb.Respond).
		Next(j.speak())

	// Initiative: what Jarvis does when nobody is talking to it.
	j.routines()

	fmt.Fprintf(os.Stderr, "jarvis: house on %s, brain on :8080 (%s)\n", worldAddr, modelNote())
	if err := bb.Serve(ctx, brain,
		bb.Addr(":8080"),
		bb.Trace(bb.JSONL(os.Stderr)),
		bb.Store(store),
	); err != nil {
		fmt.Fprintln(os.Stderr, "jarvis:", err)
		os.Exit(1)
	}
}

func registerModels() {
	key := os.Getenv("BIG_BRAIN_API_KEY")
	if key == "" {
		// No provider: one canned voice. Structured asks fail against it, which is
		// exactly the path the keyword fallbacks below cover.
		bb.WithModel(bb.FixedModel("At your service.")).WithTag(mSmart, mFast)
		return
	}
	bb.WithModel(bb.NewModel().
		WithName(envOr("BIG_BRAIN_MODEL", "gpt-4o-mini")).
		WithTemprature(0.7)).WithTag(mSmart)
	bb.WithModel(bb.NewModel().
		WithName(envOr("BIG_BRAIN_ROUTER_MODEL", envOr("BIG_BRAIN_MODEL", "gpt-4o-mini"))).
		WithThink(false).
		WithTemprature(0)).WithTag(mFast)
}

func openStore() bb.StoreBackend {
	dir := os.Getenv("BIG_BRAIN_DATA")
	if dir == "" {
		return bb.MemStore()
	}
	fs, err := bb.FileStore(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "jarvis: store:", err)
		os.Exit(1)
	}
	return fs
}

// jarvis is the assistant's dependencies: the house it acts on, the memory it
// keeps. Every capability is a method, so each one closes over both.
type jarvis struct {
	house *client
	mem   *memory
}

// --- routing ---

type intent struct {
	Intent string `json:"intent" enum:"talk,remember,forget,recall,list,house,briefing,remind" doc:"the capability that should handle this message"`
	Reason string `json:"reason" doc:"one short clause on why"`
}

// route asks a small model to classify the message, and falls back to keywords
// when there is no model or the model answers badly — a house assistant that
// stops working because a classifier hiccuped is not an assistant.
func (j *jarvis) route() bb.Flow {
	router := bb.NewAgent().
		WithModel(bb.NewModel(mFast)).
		WithRole(bb.Role(strings.Join([]string{
			"You route a home assistant's messages to exactly one capability.",
			"talk: chat, questions, anything conversational.",
			"remember: the user states a fact they want kept.",
			"forget: the user wants a remembered fact dropped.",
			"recall: the user asks what you know or what they told you.",
			"list: create, add to, remove from, or show a list (shopping, todo, ...).",
			"house: read a sensor or change a device (lights, heater, lock, fan, thermostat).",
			"briefing: a summary of the whole house right now.",
			"remind: the user wants to be reminded of something later.",
		}, "\n"))).
		WithSchema(bb.Schema[intent]()).
		Selects(idTalk, idRemember, idForget, idRecall, idList, idHouse, idBriefing, idRemind).
		OnMessage(func(_ context.Context, turn bb.Turn, chat bb.ModelChat) error {
			msg := turn.Last().Content
			reply, err := chat.AskWith(turn.Last())
			if err != nil {
				turn.Select(guess(msg))
				return nil
			}
			turn.Select(valid(bb.Extract[intent](reply).Intent, msg))
			return nil
		})
	return bb.NewFlow().WithAgent(router)
}

var capabilities = map[string]bool{
	idTalk: true, idRemember: true, idForget: true, idRecall: true,
	idList: true, idHouse: true, idBriefing: true, idRemind: true,
}

func valid(id, msg string) string {
	if capabilities[strings.TrimSpace(strings.ToLower(id))] {
		return strings.TrimSpace(strings.ToLower(id))
	}
	return guess(msg)
}

// guess is the modelless router: crude, deterministic, always available.
func guess(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case containsAny(m, "remind me", "reminder", "wake me"):
		return idRemind
	case containsAny(m, "forget", "drop that", "never mind that"):
		return idForget
	case strings.Contains(m, "remember"):
		return idRemember
	case containsAny(m, "what did i", "what do you know", "what do you remember"):
		return idRecall
	case containsAny(m, "list", "shopping", "todo", "to-do", "groceries"):
		return idList
	case containsAny(m, "briefing", "status", "everything", "how's the house", "hows the house"):
		return idBriefing
	case containsAny(m, "turn on", "turn off", "lock", "unlock", "light", "heater", "fan",
		"thermostat", "temperature", "humid", "door", "motion", "power"):
		return idHouse
	default:
		return idTalk
	}
}

// --- capabilities ---

// talk is the voice. It answers with the house and what it remembers already in
// context, so "is it cold?" and "what's the wifi code?" both just work, and it
// streams its answer when the client asked for tokens.
func (j *jarvis) talk() bb.Flow {
	a := bb.NewAgent().
		WithRole(bb.Role("You are Jarvis, the assistant of this house: warm, brief, never chatty. " +
			"Prefer one or two sentences. Use what you know about the house and the household; " +
			"if you do not know something, say so plainly.")).
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			if note := j.context(ctx); note != "" {
				chat.Add(bb.NewMessage(note).As("system"))
			}
			chat.Add(turn.Last())
			reply, err := chat.Ask()
			if err != nil {
				return err
			}
			if out, ok := turn.Stream(); ok {
				for tok := range reply.Stream() {
					out <- tok
				}
				close(out)
				return reply.Err()
			}
			turn.Reply(reply.ReadAll())
			return nil
		})
	return bb.NewFlow().WithAgent(a).WithModel(bb.NewModel(mSmart)).WithId(idTalk)
}

// context is everything Jarvis carries into a conversation: the live house, the
// facts it was told, the lists it keeps, what is still due.
func (j *jarvis) context(ctx context.Context) string {
	var lines []string
	if h, err := j.house.snapshot(ctx); err == nil {
		lines = append(lines, "House now: "+describe(h.Sensors)+".")
		lines = append(lines, "Devices: "+describe(h.Devices)+".")
	}
	if facts := j.mem.facts(); len(facts) > 0 {
		lines = append(lines, "You remember: "+strings.Join(facts, "; ")+".")
	}
	if names := j.mem.listNames(); len(names) > 0 {
		lines = append(lines, "Lists you keep: "+strings.Join(names, ", ")+".")
	}
	if due := j.mem.pending(); len(due) > 0 {
		next := due[0]
		lines = append(lines, fmt.Sprintf("Next reminder: %q at %s.", next.Text, next.Due.Format("15:04")))
	}
	return strings.Join(lines, " ")
}

type factNote struct {
	Fact string `json:"fact" doc:"the fact to keep, rewritten as a short standalone statement in the third person"`
}

// remember distills the message into a keepable fact and stores it. Durable:
// being told something once and losing it is the one failure a memory cannot have.
func (j *jarvis) remember() bb.Flow {
	a := bb.NewAgent().
		WithModel(bb.NewModel(mFast)).
		WithRole(bb.Role("Rewrite what the user wants remembered as one short standalone fact. No commentary.")).
		WithSchema(bb.Schema[factNote]()).
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			fact := strip(turn.Last().Content, "remember that", "remember", "note that", "don't forget")
			if reply, err := chat.AskWith(turn.Last()); err == nil {
				if got := strings.TrimSpace(bb.Extract[factNote](reply).Fact); got != "" {
					fact = got
				}
			}
			if fact == "" {
				turn.Reply("Remember what, exactly?")
				return nil
			}
			j.mem.remember(ctx, fact)
			turn.Reply("Noted: " + fact)
			return nil
		})
	return bb.NewFlow().WithAgent(a).WithId(idRemember).Durable()
}

func (j *jarvis) forget() bb.Flow {
	a := bb.NewAgent().OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
		needle := strip(turn.Last().Content, "forget about", "forget that", "forget")
		if needle == "" {
			turn.Reply("Forget what? Name a word from it.")
			return nil
		}
		if n := j.mem.forget(ctx, needle); n > 0 {
			turn.Reply(fmt.Sprintf("Forgotten (%d %s).", n, plural(n, "fact", "facts")))
			return nil
		}
		turn.Reply("Nothing I know matches that.")
		return nil
	})
	return bb.NewFlow().WithAgent(a).WithId(idForget).Durable()
}

// recall answers from memory in words, rather than dumping the store at the user.
func (j *jarvis) recall() bb.Flow {
	a := bb.NewAgent().
		WithModel(bb.NewModel(mSmart)).
		WithRole(bb.Role("Answer the user's question using only the facts given to you. " +
			"If they do not contain the answer, say you were never told.")).
		OnMessage(func(_ context.Context, turn bb.Turn, chat bb.ModelChat) error {
			facts := j.mem.facts()
			if len(facts) == 0 {
				turn.Reply("You haven't told me anything to keep yet.")
				return nil
			}
			chat.Add(bb.NewMessage("Facts you were told:\n- " + strings.Join(facts, "\n- ")).As("system"))
			reply, err := chat.AskWith(turn.Last())
			if err != nil {
				turn.Reply("Here's what I know: " + strings.Join(facts, "; ") + ".")
				return nil
			}
			turn.Reply(reply.ReadAll())
			return nil
		})
	return bb.NewFlow().WithAgent(a).WithId(idRecall)
}

type listOp struct {
	List string `json:"list" doc:"the list's name, lowercase, e.g. shopping or todo"`
	Op   string `json:"op" enum:"add,remove,show" doc:"what to do with it"`
	Item string `json:"item" doc:"the item, empty when showing"`
}

func (j *jarvis) lists() bb.Flow {
	a := bb.NewAgent().
		WithModel(bb.NewModel(mFast)).
		WithRole(bb.Role("Turn the user's message into one list operation.")).
		WithSchema(bb.Schema[listOp]()).
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			op := guessListOp(turn.Last().Content)
			if reply, err := chat.AskWith(turn.Last()); err == nil {
				got := bb.Extract[listOp](reply)
				if got.List != "" && got.Op != "" {
					op = listOp{List: strings.ToLower(got.List), Op: got.Op, Item: strings.TrimSpace(got.Item)}
				}
			}
			switch {
			case op.Op == "add" && op.Item != "":
				j.mem.listAdd(ctx, op.List, op.Item)
				n := len(j.mem.list(op.List))
				turn.Reply(fmt.Sprintf("Added %s to %s (%d %s).", op.Item, op.List, n, plural(n, "item", "items")))
			case op.Op == "remove" && op.Item != "":
				if j.mem.listRemove(ctx, op.List, op.Item) {
					turn.Reply(fmt.Sprintf("Removed %s from %s.", op.Item, op.List))
					return nil
				}
				turn.Reply(fmt.Sprintf("%s isn't on the %s list.", op.Item, op.List))
			default:
				items := j.mem.list(op.List)
				if len(items) == 0 {
					turn.Reply("The " + op.List + " list is empty.")
					return nil
				}
				turn.Reply(op.List + ": " + strings.Join(items, ", ") + ".")
			}
			return nil
		})
	return bb.NewFlow().WithAgent(a).WithId(idList).Durable()
}

type command struct {
	Device string `json:"device" doc:"the device to change, empty when only reading"`
	State  string `json:"state" doc:"the state to set: on, off, locked, unlocked, or a thermostat number"`
	Sensor string `json:"sensor" doc:"the sensor to read, empty when setting a device"`
}

// control is the hands: it sets a device or reads a sensor, and always answers
// with what the house actually reports back rather than what it was asked to do.
func (j *jarvis) control() bb.Flow {
	a := bb.NewAgent().
		WithModel(bb.NewModel(mFast)).
		WithRole(bb.Role("Turn the request into one house command. " +
			"Devices: porch light, living light, heater, fan, front lock, thermostat. " +
			"Sensors: temperature, humidity, door, motion, daylight, power. " +
			"Set exactly one of device+state or sensor.")).
		WithSchema(bb.Schema[command]()).
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			cmd := guessCommand(turn.Last().Content)
			if reply, err := chat.AskWith(turn.Last()); err == nil {
				if got := bb.Extract[command](reply); got.Device != "" || got.Sensor != "" {
					cmd = got
				}
			}
			if cmd.Device == "" {
				h, err := j.house.snapshot(ctx)
				if err != nil {
					return err
				}
				sensor := cmd.Sensor
				if v, ok := h.Sensors[sensor]; ok {
					turn.Reply(fmt.Sprintf("%s: %s.", sensor, v))
					return nil
				}
				turn.Reply("I don't have a " + sensor + " sensor. I read " + describe(h.Sensors) + ".")
				return nil
			}
			if err := j.house.set(ctx, cmd.Device, cmd.State); err != nil {
				return err
			}
			h, err := j.house.snapshot(ctx)
			if err != nil {
				return err
			}
			turn.Reply(fmt.Sprintf("%s is %s.", cmd.Device, h.Devices[cmd.Device]))
			return nil
		})
	return bb.NewFlow().WithAgent(a).WithId(idHouse)
}

// briefing runs two agents over one shared chat: a reader that posts the raw
// house state, and a narrator that waits for it and turns it into a sentence.
// The narrator sees the reader's reply because bb.Group shares the live chat.
func (j *jarvis) briefing() bb.Flow {
	read := bb.NewCheckpoint()

	reader := bb.NewAgent().OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
		defer bb.Reached(read)
		h, err := j.house.snapshot(ctx)
		if err != nil {
			return err
		}
		turn.Reply("House: " + describe(h.Sensors) + ". Devices: " + describe(h.Devices) + ".")
		return nil
	})

	narrator := bb.NewAgent().
		WithModel(bb.NewModel(mSmart)).
		WithRole(bb.Role("Give a two-sentence spoken briefing of the house. " +
			"Lead with anything worth acting on — an unlocked door, a light left on, an odd reading. " +
			"Then the rest in one breath. No lists, no bullet points.")).
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			if err := bb.Wait(ctx, read); err != nil {
				return err
			}
			chat.Add(turn.Last())
			if extra := j.errands(); extra != "" {
				chat.Add(bb.NewMessage(extra).As("system"))
			}
			reply, err := chat.Ask()
			if err != nil {
				return nil // the reader's raw line is already a serviceable briefing
			}
			turn.Reply(reply.ReadAll())
			return nil
		})

	return bb.Group(
		bb.NewFlow().WithAgent(reader).WithId("briefing/read"),
		bb.NewFlow().WithAgent(narrator).WithId("briefing/say"),
	).WithId(idBriefing)
}

// errands is the household side of a briefing: what is on the lists, what is due.
func (j *jarvis) errands() string {
	var parts []string
	for _, name := range j.mem.listNames() {
		parts = append(parts, fmt.Sprintf("%s list has %d items", name, len(j.mem.list(name))))
	}
	if due := j.mem.pending(); len(due) > 0 {
		parts = append(parts, fmt.Sprintf("%q is due at %s", due[0].Text, due[0].Due.Format("15:04")))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Also: " + strings.Join(parts, ", ") + "."
}

type reminderSpec struct {
	Text    string `json:"text" doc:"what to remind the user of, in the second person"`
	Minutes int    `json:"minutes" doc:"minutes from now until it is due; 0 if an absolute time is given"`
	At      string `json:"at" doc:"absolute local time as HH:MM, empty if minutes is used"`
}

// remind stores a due time. The sweep routine below is what actually fires it,
// so a reminder outlives the request, the process, and a restart.
func (j *jarvis) remind() bb.Flow {
	a := bb.NewAgent().
		WithModel(bb.NewModel(mFast)).
		WithRole(bb.Role("Extract the reminder and when it is due. Use minutes for relative times, at for clock times.")).
		WithSchema(bb.Schema[reminderSpec]()).
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			text, due := guessReminder(turn.Last().Content, time.Now())
			if reply, err := chat.AskWith(turn.Last()); err == nil {
				got := bb.Extract[reminderSpec](reply)
				if got.Text != "" {
					text = got.Text
				}
				if when, ok := resolve(got, time.Now()); ok {
					due = when
				}
			}
			if text == "" {
				turn.Reply("Remind you of what, and when?")
				return nil
			}
			j.mem.schedule(ctx, text, due)
			turn.Reply(fmt.Sprintf("I'll remind you at %s: %s", due.Format("15:04"), text))
			return nil
		})
	return bb.NewFlow().WithAgent(a).WithId(idRemind).Durable()
}

// speak echoes the final answer into the house. It sits after Respond, so the
// user has their reply before the house hears about it.
func (j *jarvis) speak() bb.Flow {
	return bb.Notify(j.house.notify)
}

// --- initiative: the routines nobody asks for ---

// goodnightPlan is seeded into the goodnight trigger, so which devices get shut
// is configuration travelling with the schedule rather than code.
type goodnightPlan struct {
	Off  []string `json:"off"`
	Lock []string `json:"lock"`
}

// boot confirms the house answers, and says so.
func (j *jarvis) boot() bb.Flow {
	return bb.NewFlow().WithAgent(bb.NewAgent().
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			h, err := j.house.snapshot(ctx)
			if err != nil {
				turn.Reply("Jarvis is up, but the house is not answering.")
				return nil
			}
			turn.Reply("Jarvis online. " + describe(h.Sensors) + ".")
			return nil
		})).WithId(idBoot)
}

// sweep fires whatever became due. This is the loop that makes reminders
// real — a cheap sweep instead of a timer per reminder.
func (j *jarvis) sweep() bb.Flow {
	return bb.NewFlow().WithAgent(bb.NewAgent().
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			for _, r := range j.mem.due(ctx, time.Now()) {
				if err := j.house.notify(ctx, "Reminder: "+r.Text); err != nil {
					return err
				}
			}
			return nil
		})).WithId(idSweep).Durable()
}

// morning is the house introducing the day, in its own words.
func (j *jarvis) morning() bb.Flow {
	return bb.NewFlow().WithAgent(bb.NewAgent().
		WithModel(bb.NewModel(mSmart)).
		WithRole(bb.Role("Good morning. In two sentences: the house, then the day's errands. Warm, brief, spoken.")).
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			chat.Add(bb.NewMessage(j.context(ctx) + " " + j.errands()).As("system"))
			chat.Add(bb.NewMessage("Give me the morning briefing."))
			reply, err := chat.Ask()
			if err != nil {
				turn.Reply("Good morning. " + j.context(ctx))
				return nil
			}
			turn.Reply(reply.ReadAll())
			return nil
		})).WithId(idMorning).Durable()
}

// goodnight acts first (locks up, kills the lights), then reports.
func (j *jarvis) goodnight() bb.Flow {
	return bb.NewFlow().WithAgent(bb.NewAgent().
		OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
			plan, ok := bb.Payload[goodnightPlan](turn)
			if !ok {
				plan = goodnightPlan{Off: []string{"porch light", "living light"}, Lock: []string{"front lock"}}
			}
			var did []string
			for _, d := range plan.Off {
				if err := j.house.set(ctx, d, "off"); err != nil {
					return err
				}
				did = append(did, d+" off")
			}
			for _, d := range plan.Lock {
				if err := j.house.set(ctx, d, "locked"); err != nil {
					return err
				}
				did = append(did, d+" locked")
			}
			turn.Reply("Goodnight — " + strings.Join(did, ", ") + ".")
			return nil
		})).WithId(idGoodnight).Durable()
}

// routineIDs is every deferred body id routines() schedules — the vocabulary
// TestEveryRoutineHasAScenario checks the e2e table against.
var routineIDs = []string{idBoot, idSweep, idMorning, idGoodnight}

func (j *jarvis) routines() {
	// At boot: confirm the house answers, and say so.
	bb.Trigger().Next(j.boot().Next(j.speak()))

	// Every minute: fire whatever became due.
	bb.Trigger().Next(bb.Every("* * * * *")).Next(j.sweep())

	// 07:00 — the house introduces the day, in its own words.
	bb.Trigger().Next(bb.Every("0 7 * * *")).Next(j.morning().Next(j.speak()))

	// 22:30 — the goodnight routine acts first, then reports.
	bb.Trigger(bb.WithSeedPayload(goodnightPlan{
		Off:  []string{"porch light", "living light", "fan"},
		Lock: []string{"front lock"},
	})).Next(bb.Every("30 22 * * *")).Next(j.goodnight().Next(j.speak()))

	// A doorbell camera POSTs here whenever it sees someone — the reception
	// half of bb.Payload/bb.Metadata. bb.Payload[T] reads the JSON body (who
	// it saw); bb.Metadata[T] reads the request's headers (a shared-secret
	// signature the camera sends), kept as its own channel rather than merged
	// into the body's fields so a body field named the same as a header can
	// never collide. No top-level Respond in this body, so Serve
	// acks 202 immediately and the announcement runs in the background —
	// don't block the camera on Jarvis talking. Unlike Every/Once, a Webhook
	// body needs no WithId: the endpoint id ("doorbell") is its identity.
	bb.Trigger().Next(bb.Webhook("doorbell")).Next(
		bb.NewFlow().WithAgent(bb.NewAgent().
			OnMessage(func(ctx context.Context, turn bb.Turn, chat bb.ModelChat) error {
				headers, _ := bb.Metadata[map[string]string](turn)
				if headers["X-Doorbell-Signature"] != doorbellSecret {
					return nil // unrecognized sender — nothing to announce
				}
				alert, ok := bb.Payload[doorbellAlert](turn)
				if !ok {
					turn.Reply("Someone's at the door.")
					return nil
				}
				turn.Reply(fmt.Sprintf("%s is at the door (%.0f%% sure).", alert.Visitor, alert.Confidence*100))
				return nil
			})).Next(j.speak()))
}

// doorbellSecret is the shared secret the doorbell camera signs its posts
// with, read back via bb.Metadata[T] — a webhook endpoint has no auth of its
// own (see bb.Webhook's doc), so an app-level check like this is how a body
// decides whether to trust what arrived. Hardcoded for the demo; a real
// deployment reads it from env.
const doorbellSecret = "porch-cam-1"

// doorbellAlert is the doorbell camera's POST body — what it saw, read back
// via bb.Payload[T].
type doorbellAlert struct {
	Visitor    string  `json:"visitor"`
	Confidence float64 `json:"confidence"`
}

// --- keyword fallbacks: what Jarvis understands with no model at all ---

func guessListOp(msg string) listOp {
	m := strings.ToLower(msg)
	list := "todo"
	for _, name := range []string{"shopping", "groceries", "todo", "to-do", "packing", "reading"} {
		if strings.Contains(m, name) {
			list = strings.ReplaceAll(name, "to-do", "todo")
			if list == "groceries" {
				list = "shopping"
			}
			break
		}
	}
	switch {
	case containsAny(m, "remove", "delete", "take off", "cross off"):
		return listOp{List: list, Op: "remove", Item: strip(m, "remove", "delete", "take off", "cross off")}
	case containsAny(m, "add", "put"):
		item := strip(m, "add", "put")
		item = cut(item, " to ", " on ")
		return listOp{List: list, Op: "add", Item: item}
	default:
		return listOp{List: list, Op: "show"}
	}
}

func guessCommand(msg string) command {
	m := strings.ToLower(msg)
	for _, d := range []string{"porch light", "living light", "thermostat", "heater", "fan", "front lock"} {
		if !strings.Contains(m, d) && !(d == "front lock" && containsAny(m, "lock", "unlock", "door lock")) {
			continue
		}
		switch {
		case d == "thermostat":
			if n := regexp.MustCompile(`\d+`).FindString(m); n != "" {
				return command{Device: d, State: n}
			}
		case containsAny(m, "unlock"):
			return command{Device: d, State: "unlocked"}
		case containsAny(m, "lock"):
			return command{Device: d, State: "locked"}
		case strings.Contains(m, "off"):
			return command{Device: d, State: "off"}
		case strings.Contains(m, "on"):
			return command{Device: d, State: "on"}
		}
	}
	if strings.Contains(m, "light") && containsAny(m, "on", "off") {
		state := "on"
		if strings.Contains(m, "off") {
			state = "off"
		}
		return command{Device: "living light", State: state}
	}
	for _, s := range []string{"temperature", "humidity", "door", "motion", "daylight", "power"} {
		if strings.Contains(m, s[:4]) {
			return command{Sensor: s}
		}
	}
	return command{Sensor: "temperature"}
}

// Longest alternative first: "minutes" must not match as "m" and leave "inutes".
var relative = regexp.MustCompile(`in (\d+) ?(minutes|minute|mins|min|hours|hour|hrs|hr|h|m)`)
var clock = regexp.MustCompile(`at (\d{1,2})[:.](\d{2})`)

// guessReminder pulls the text and the due time out of plain speech.
func guessReminder(msg string, now time.Time) (string, time.Time) {
	m := strings.ToLower(msg)
	due := now.Add(10 * time.Minute)
	if g := relative.FindStringSubmatch(m); g != nil {
		n, _ := strconv.Atoi(g[1])
		unit := time.Minute
		if strings.HasPrefix(g[2], "h") {
			unit = time.Hour
		}
		due = now.Add(time.Duration(n) * unit)
		m = strings.Replace(m, g[0], "", 1)
	} else if g := clock.FindStringSubmatch(m); g != nil {
		h, _ := strconv.Atoi(g[1])
		min, _ := strconv.Atoi(g[2])
		due = atClock(now, h, min)
		m = strings.Replace(m, g[0], "", 1)
	}
	text := strip(m, "remind me to", "remind me that", "remind me", "reminder to", "reminder")
	return strings.TrimSpace(strings.Trim(text, " ,.")), due
}

// resolve turns a model's reminder spec into a time, reporting whether it said
// anything usable at all.
func resolve(spec reminderSpec, now time.Time) (time.Time, bool) {
	if spec.Minutes > 0 {
		return now.Add(time.Duration(spec.Minutes) * time.Minute), true
	}
	if g := clock.FindStringSubmatch("at " + spec.At); g != nil {
		h, _ := strconv.Atoi(g[1])
		min, _ := strconv.Atoi(g[2])
		return atClock(now, h, min), true
	}
	return time.Time{}, false
}

// atClock is today at h:m, or tomorrow if that moment has passed.
func atClock(now time.Time, h, m int) time.Time {
	t := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if !t.After(now) {
		t = t.Add(24 * time.Hour)
	}
	return t
}

// --- small helpers ---

// describe renders a map in a fixed, readable order.
func describe(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+" "+m[k])
	}
	return strings.Join(parts, ", ")
}

// strip removes any leading command word and the punctuation after it.
func strip(s string, prefixes ...string) string {
	out := strings.TrimSpace(s)
	for _, p := range prefixes {
		if i := strings.Index(strings.ToLower(out), p); i >= 0 {
			out = out[i+len(p):]
			break
		}
	}
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(out), " :,-."))
}

// cut drops everything from the first separator found onward ("add milk to the
// shopping list" → "milk").
func cut(s string, seps ...string) string {
	for _, sep := range seps {
		if i := strings.Index(s, sep); i > 0 {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func modelNote() string {
	if os.Getenv("BIG_BRAIN_API_KEY") == "" {
		return "no BIG_BRAIN_API_KEY: keyword mode"
	}
	return "model " + envOr("BIG_BRAIN_MODEL", "gpt-4o-mini")
}
