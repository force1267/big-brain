package flow

import "context"

// Notify is a prebuilt outgoing flow: it sends the last message in the chat to
// an external sink (a webhook, a log, ...) and passes the chat through
// unchanged, so it can sit anywhere in a chain. The send is a plain function so
// this package stays free of any particular channel.
func Notify(send func(ctx context.Context, text string) error) Flow {
	return notify{send}
}

type notify struct {
	send func(ctx context.Context, text string) error
}

func (notify) id() string         { return "" }
func (n notify) Next(f Flow) Flow { return then(n, f) }

func (n notify) run(ctx context.Context, in State) (State, error) {
	tracerFrom(ctx).Event(ctx, Event{Kind: "notify"})
	if len(in.Chat) > 0 {
		if err := n.send(ctx, in.Chat[len(in.Chat)-1].Content); err != nil {
			return in, err
		}
	}
	return in, nil
}
