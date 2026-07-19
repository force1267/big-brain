package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"
)

// ErrSend wraps failures delivering a notification.
var ErrSend = errors.New("notification send failed")

// Message is one outgoing notification the brain initiates.
type Message struct {
	Speaker string `json:"speaker,omitempty"` // whom it concerns, if anyone
	Text    string `json:"text"`
}

// Channel delivers notifications to the outside world. Channels are an
// extensible family (see PRODUCT.md); the outgoing webhook is v1's one
// built-in member.
type Channel interface {
	Notify(ctx context.Context, m Message) error
}

// Webhook returns the v1 built-in Channel: an HTTP POST of the Message as
// JSON to url. It composes with any relay (Telegram bots, ntfy, ...).
func Webhook(url string) Channel { return webhook(url) }

type webhook string

var _ Channel = webhook("")

func (u webhook) Notify(ctx context.Context, m Message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSend, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, string(u), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSend, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSend, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: status %d", ErrSend, resp.StatusCode)
	}
	return nil
}

// Log returns the fallback Channel used when no webhook is configured: it
// logs the notification so nothing is silently dropped.
func Log() Channel { return logChannel{} }

type logChannel struct{}

var _ Channel = logChannel{}

func (logChannel) Notify(_ context.Context, m Message) error {
	logrus.WithFields(logrus.Fields{"speaker": m.Speaker, "text": m.Text}).
		Info("notification (no channel configured)")
	return nil
}
