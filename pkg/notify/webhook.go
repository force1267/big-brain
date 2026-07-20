package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Webhook returns the v1 built-in Channel: an HTTP POST of the Message as
// JSON to url. It composes with any relay (Telegram bots, ntfy, ...).
func Webhook(url string) Channel { return Monitored(webhook(url)) }

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
