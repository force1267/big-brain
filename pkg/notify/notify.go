package notify

import (
	"context"
	"errors"

	"github.com/sirupsen/logrus"
)

// ErrSend wraps failures delivering a notification.
var ErrSend = errors.New("notification send failed")

// Message is one outgoing notification the brain initiates. Text is
// free-form; any addressing (who it's for) belongs in Text, the brain
// author's convention, not an engine concept.
type Message struct {
	Text string `json:"text"`
}

// Channel delivers notifications to the outside world. Channels are an
// extensible family (see PRODUCT.md); this file holds only the interface
// and the always-available Log fallback — each real implementation gets
// its own file (see webhook.go), so the package listing itself shows what
// "extensible" means here.
type Channel interface {
	Notify(ctx context.Context, m Message) error
}

// Log returns the fallback Channel used when no other channel is
// configured: it logs the notification so nothing is silently dropped.
func Log() Channel { return logChannel{} }

type logChannel struct{}

var _ Channel = logChannel{}

func (logChannel) Notify(_ context.Context, m Message) error {
	logrus.WithField("text", m.Text).Info("notification (no channel configured)")
	return nil
}
