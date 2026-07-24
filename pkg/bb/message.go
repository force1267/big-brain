package bb

import "github.com/force1267/big-brain/pkg/model"

// Message is one chat message (a role and its content) — the unit an agent's
// turn adds, asks with, and receives.
type Message = model.Message

// NewMessage builds a message with the given content, defaulting to the "user"
// role. Change it with As: bb.NewMessage("...").As("system").
func NewMessage(content string) Message { return model.NewMessage(content) }

// Role is a system-persona instruction — sugar for a system-role Message,
// which is how an agent's WithRole prepends it to the conversation.
func Role(text string) Message { return model.NewMessage(text).As("system") }
