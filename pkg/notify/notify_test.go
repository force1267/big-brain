package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookPostsMessage(t *testing.T) {
	var got Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
	}))
	defer srv.Close()

	err := Webhook(srv.URL).Notify(context.Background(), Message{Speaker: "dad", Text: "done"})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if got.Speaker != "dad" || got.Text != "done" {
		t.Fatalf("got %+v", got)
	}
}

func TestWebhookBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	if err := Webhook(srv.URL).Notify(context.Background(), Message{Text: "x"}); !errors.Is(err, ErrSend) {
		t.Fatalf("err = %v; want ErrSend", err)
	}
}

func TestWebhookUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // now nothing listens
	if err := Webhook(srv.URL).Notify(context.Background(), Message{Text: "x"}); !errors.Is(err, ErrSend) {
		t.Fatalf("err = %v; want ErrSend", err)
	}
}

func TestLogNeverFails(t *testing.T) {
	if err := Log().Notify(context.Background(), Message{Text: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestMockRecords(t *testing.T) {
	m := &Mock{}
	if err := m.Notify(context.Background(), Message{Text: "x"}); err != nil || len(m.Sent) != 1 {
		t.Fatalf("sent %+v, %v", m.Sent, err)
	}
}
