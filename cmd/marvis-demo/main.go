package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/force1267/big-brain/pkg/bb"
)

var (
	ModelCheap string = "cheap"
	ModelSmart        = "smart"
	ModelFast         = "fast"
)

var (
	FlowIntentDiscovery string = "IntentDiscovery"
	FlowTalking                = "Talking"
	FlowRemember               = "Remember"
	FlowRecall                 = "Recall"
	FlowList                   = "List"
	FlowHouse                  = "House"
	FlowExtra                  = "Extra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// bb.WithModel binds one model to one or more string tags, so flows can fetch
	// it by tag (bb.NewModel(ModelCheap)) instead of re-specifying name/params every time.
	// define the model once here; every tagged flow reuses it, and swapping the backing
	// model later is a one-line change that all of them follow. one model can answer to
	// several tags — "fast" and "cheap" point at the same small model until you split them.
	// (intent-discovery and talk below deliberately build their models inline instead —
	// the demo shows both styles; tagging is a convenience, not a requirement.)
	var register bb.RegisterModel = bb.WithModel(
		bb.NewModel().WithName("google/gemma-4-e4b").WithThink(false).WithTemprature(0.3),
	)
	register = register.WithTag(
		ModelCheap,
		ModelFast,
	)

	// there is a concept of default model, and flows using that model as their default models. (flows can have a default model for their agents; flow.WithModel just like agent.WithModel)
	// the first call to bb.WithModel sets a default model that all the flows use as their default model.
	// the bb.WithDefaultModel(bb.NewModel().) works like bb.WithModel but it does not tag and only sets the default model.
	// the order of models used by an agent:
	// 1- agent.WithModel() sets it
	// 2- flow.WithModel() on the flow the agent is running in
	// 3- flow's default model set by bb.WithDefaultModel()
	// 4- the first model registered by bb.WithModel

	talking, err := flowTalk(ctx)
	if err != nil {
	}

	remembering, err := flowRemember(ctx)
	if err != nil {
	}

	recalling, err := flowRecall(ctx)
	if err != nil {
	}

	listing, err := flowList(ctx)
	if err != nil {
	}

	house, err := flowHouse(ctx)
	if err != nil {
	}

	intentDiscovery, err := flowDiscoverIntent(ctx)
	if err != nil {
	}

	// a Select groups flows to be selected by agents of the flow before them.
	// and agent can call Select and pass a string. each flow must have a call to WithId to set an Id.
	// the last call to Select inside a flow selects the next flow from the group defined.
	// bb.Select ignores (and warning logs) any flow that didn't call WithId and is not selectable.
	var capabilities bb.Flow = bb.Select(talking, remembering, recalling, listing, house)
	// there exist other grouping strategies:
	// bb.All   // gives separate chat contents to all flows. all the replies from all of the flows gets added to the final output. it ends when all of them are ended.
	// bb.One   // gives separate chat contents to all flows. when one of the flows ends, the ctx of all others (with their agents) gets Done. the replies of the one that ended first gets added to the final output.
	// bb.Group // gives the same chat content to all flows. its the same chat content, any Reply on the chat is immediately accessibile by all flows. it ends when all of the group agents are ended.

	// bb.Respond is a prebuilt bb.Flow that replays the last message in the chat content it receives to the user.
	// there exists another types of outgoing prebuilt Flow: bb.Notify(...)
	var flow bb.Flow = intentDiscovery.Next(capabilities).Next(bb.Respond) // .Next(...) this can continue after a reply was sent to user
	// a flow is a durable execution, marked, pausable and resumable.
	// if the process crashes, it can continue from the start of a flow it was running

	// it serves the combined flow in openapi and anthropic API style.
	// ctx drives graceful shutdown; opts (bb.Addr/bb.Store/bb.Trace/bb.Workers)
	// override the zero-config defaults (in-mem store, :8080, jsonl trace, BIG_BRAIN_* env).
	// Serve is also the single point where flow/agent wiring errors surface.
	if err := bb.Serve(ctx, flow); err != nil {
		fmt.Println(err)
	}
	// multiple flows can be served
	// bb.WithFlow(flow).As("exmaple/marvis-demo").WithFlow(anotherFlow).As("google/gemma-4-e4b").Serve()
	// var register bb.RegisterFlow = bb.WithFlow() // registers a flow as default flow. requests not mentioning a model name uses this flow
	// bb.WithFlow().As("model/name") // a flow can be named. not twice .As("model-1").As("model-2") is a type error
	// var regsiterAnother bb.RegisterNamedFlow = bb.WithFlow().As("model/name")
	// bb.RegisterFlow can not be chained with another WithFlow because having two default models is not possible.
	// both bb.RegisterFlow and bb.RegisterNamedFlow can be chained to .Serve(ctx)
	// there is a bb.WithDefaultFlow(flow) (without .As()) that returns the same chain WithFlow()
	// the order of default flow is:
	// 1- bb.Serve(ctx, defaultFlow) was intentionally called with a default flow
	// 2- last WithDefaultFlow()
	// 3- last WithFlow() without .As()
	// 4- last WithFlow().As() if no unnamed WithFlow or WithDefaultFlow exists

	// multiple bb.WithFlow() works the same
	// bb.WithFlow(flow1).As("flow/1")
	// bb.WithFlow(flow2).As("flow/2")
	// bb.Serve(ctx) // serve can be run without default flow

	// the Serve also serves diagnostics, tracing and debbugging endpoints too
	// visualization tools and developer tools that we are going to develop can use them
}

type intent struct {
	Intent string `json:"intent"` // one of the possible intent keys
	Reason string `json:"reason"` // brief justification for the choice
}

type flowDesc struct {
	Id   string
	Desc string
}

func flowDiscoverIntent(ctx context.Context) (bb.Flow, error) {
	possibleIntents := map[string]flowDesc{
		"talk":     flowDesc{Id: FlowTalking, Desc: "the user just wants to talk"},
		"remember": flowDesc{Id: FlowRemember, Desc: "the user wants you too remember a fact"},
		"recall":   flowDesc{Id: FlowRecall, Desc: "the user wants you too recall a remembered fact"},
		"list":     flowDesc{Id: FlowList, Desc: "the user wants you to maintain a list. create it, or add an item to already created one, or remove an item, or modify some items."},
		"house":    flowDesc{Id: FlowHouse, Desc: "the user wants to controll the smart house system or use its capabilities. reading a sensor, set something, check something using the house system."},
		"extra":    flowDesc{Id: FlowExtra, Desc: "the user clearly wants you to do something, but its not any of the listed capabilities here. and its not just talking. the thing they want is something new that you might need to want to learn"},
	}

	roleStr := "You are an intent discovery helper. User says something and you will figure out what they want from a list of possible intents.\n" +
		"The list of possible intents:\n" +
		""
	for name, fd := range possibleIntents {
		roleStr += "- " + name + ": " + fd.Desc + "\n"
	}

	var model bb.Model = bb.NewModel().
		WithName("google/gemma-4-e4b").
		WithThink(false).
		WithTemprature(0.5)
	// the WithX builders only record data; they never return an error and never
	// return a nil model mid-chain (a nil would just nil-panic the next call).
	// an invalid config (unknown name, unreachable endpoint) is recorded and
	// surfaces once, at bb.Serve, together with every other wiring error.

	// other way of getting a model
	// var (
	// 	fast = "fast"
	// 	cheap = "cheap"
	//  smart = "smart"
	// ) string
	// define your models like above, then register:
	// bb.RegisterModel(model, fast, smart)
	// bb.RegisterModel(anotherModel, cheap)
	// then
	// var model bb.Model := bb.NewModel(fast, smart) // tries to find the model registerd before with all the tags

	role := bb.Role(roleStr)
	schema := bb.Schema[intent]()

	var agent bb.Agent = bb.NewAgent().
		WithModel(model).
		WithRole(role).
		WithSchema(schema).
		// Selects declares, at build time, every flow id this agent's turns may Select.
		// it is optional: without it, a bad Select id is still caught loudly at request time
		// against the linked Select group. WITH it, Serve also verifies at STARTUP that this
		// declared set is a subset of the downstream group's ids — so a typo'd or stale id in a
		// branch that testing never exercised fails at boot, not in production.
		Selects(FlowTalking, FlowRemember, FlowRecall, FlowList, FlowHouse, FlowExtra).
		// the message arrives from the Flow
		OnMessage(func(ctx context.Context, turn bb.Turn) error {
			// turn is the agent live, acting on this incoming message. it can Add/Ask/Reply/Select
			// but NOT reconfigure the agent (no WithModel/WithRole here) — self-modification at
			// runtime is impossible by construction. the incoming messages are turn.Messages.
			// ctx is for this turn running in the flow on this message. after this function returns the ctx is done.
			// the ctx passed to the flowDiscoverIntent outer function is the process' context. the ctx in this function is a child of the outer ctx.
			// the engine or flow can end the agent by closing the ctx. the agent implementation must respect that. for example cancel an http call by passing the context to the call.

			var reply bb.Reply
			turn.Add(turn.Last())    // adds the latest incoming message (turn.Last()) to this turn's chat.
			reply, err := turn.Ask() // sends the turn's chat to the model
			// can be both in a single call
			// reply, err := turn.AskWith(turn.Last())
			if err != nil {
				// schema-mismatch is owned HERE, by Ask: the agent holds the schema
				// (WithSchema), so it validates the reply against it and this is the
				// single place that failure surfaces. Extract below is therefore a
				// pure typed getter that cannot error — no duplicated error path.
				return fmt.Errorf("error at discover agent ask: %w", err)
			}
			// turn.Request().MaxTokens or turn.Request().Temperature to get params that was sent alongside user's original request.

			// var response string = reply.ReadAll() // the whole reply, blocking until it is complete
			// var tokens <-chan string = reply.Stream() // the SAME reply live, token by token, from the start
			// reply.ReadAll() and reply.Stream() coexist — you are never forced to choose: stream the tokens to
			// the user AND still ReadAll() the whole thing (e.g. to remember it). Read() gives the next available chunk.
			// (this discovery agent has a schema, so its reply is always buffered whole to validate — Stream() here
			// just yields that finished JSON once. live token streaming is for the schema-less terminal reply; see flowTalk.)
			// var media []string = reply.ListMedia() // a list of media sent alongside reply
			// var image []byte = reply.Media("some-image")
			var response intent = bb.Extract[intent](reply) // reads one full message after it is completed, extracts it

			// here interaction with agent can continue, that is why it is called an agent
			// newMsg := bb.NewMessage("but what about that other thing").As("system") // or "user" or anything you want. next agents can identify who sent the message
			// newReply, _ := turn.Ask(newMsg)

			// turn.Select(SomeFlowId) // this works with bb.Select, if the next flow is not a Select, this will not have any effect
			// validation: at request time Select(id) is checked against the ids of the Select group this flow was linked to via Next — an unknown id is a loud error, not a silent misroute.
			// that runtime check needs no declaration. to also catch a bad id at STARTUP (before any request), an agent can optionally declare its exits: bb.NewAgent().Selects(FlowTalking, FlowRemember, ...).
			// then Serve verifies the declared set is a subset of the group's ids at boot. optional because it repeats the switch targets — unless one capability map drives both, so declare only when you want the boot-time guarantee.
			// within one agent, calling Select multiple times is last-wins in program order (deterministic, like setting a variable).
			// across agents that run concurrently (bb.All/bb.Group), two different Selects is a wiring error the flow surfaces loudly, NOT a silent last-writer race.
			// selecting the same id from multiple agents is fine. to make a specific agent's choice win, order them with bb.Checkpoint()/bb.Wait(cp).
			switch response.Intent {
			case "talk":
				turn.Select(FlowTalking)
			case "remember":
				turn.Select(FlowRemember)
			case "recall":
				turn.Select(FlowRecall)
			case "list":
				turn.Select(FlowList)
			case "house":
				turn.Select(FlowHouse)
			default:
				// not one of the wired capabilities — "extra" territory, explored later
				turn.Select(FlowExtra)
			}
			turn.Reply(fmt.Sprintf("the intent agent selected %s flow to continue", response.Intent)) // adds the reply to the flow's chat
			// an agent might not Reply. no chat gets added to the flow. the flow continues with chat content just like when it started.
			// the agent can Reply many times. all of them gets added to the flow's chat

			return nil // after this return, the flow knows not to wait for the agent.
		})

	var flow bb.Flow = bb.NewFlow().WithAgent(agent)
	// the discovery flow has one agent
	// a flow can have more than one agent. they all get the incoming message
	// var flow bb.Flow = bb.NewFlow().WithAgent(router, summarizer, recognizer, parentalControl)
	// different agents can pause and wait for eachother. cp := bb.Checkpoint(), bb.Wait(cp), bb.Reached(cp)
	// for example parentalControl can wait for recognizer and summarizer if it finds something the child should't do
	// router can use recognizer to route to admin and diagnostics capabilities only the developer or the smart home company's technician can access

	// multiple agents can turn.Reply(...), all of them gets added to the flows' chat and gets sent to the next flow.

	// a flow can also have a model. bb.NewFlow().WithModel(model)
	// this way the agent without a model (bb.NewAgent().OnMessage(func ...)) inherits the flow's model
	// and also, if a flow doesn't define a model, by default its model becomes the first bb.RegisterModel that happens
	return flow, nil
}

func flowTalk(ctx context.Context) (bb.Flow, error) {
	var model bb.Model = bb.NewModel().
		WithName("google/gemma-4-e4b").
		WithThink(false).
		WithTemprature(0.7)

	role := bb.Role("You are Marvis, a warm and concise home assistant. Just chat with the user.")

	// no schema, no OnMessage: a plain agent asks the model and replies. when this flow is the
	// TERMINAL one (its reply is the user's answer — the flow right before bb.Respond, or the last
	// in the chain) AND the client asked for streaming, a default agent like this streams its reply
	// to the user token by token automatically. nothing to write. otherwise it buffers. talk sits
	// before Respond, so it is terminal.
	var agent bb.Agent = bb.NewAgent().
		WithModel(model).
		WithRole(role)
	// to stream from your OWN OnMessage handler, tee the incoming tokens into the outgoing channel
	// turn.Stream() hands you:
	//   reply, _ := turn.Ask()
	//   if out, ok := turn.Stream(); ok {          // ok only when this turn is terminal + client wants SSE
	//       for tok := range reply.Stream() {      // live model tokens
	//           out <- tok                          // forward (or transform / inject as you like)
	//       }
	//       close(out)                              // the framework delivers to the user AND captures the
	//                                               // whole message into the flow's chat for you (for Respond/
	//                                               // Notify downstream and for durability) — do NOT also turn.Reply it.
	//       return reply.Err()                      // a mid-stream model error lands here (no HTTP status left once
	//                                               // tokens are flowing; Serve emits an SSE error frame)
	//   }
	//   turn.Reply(reply.ReadAll())                 // buffered fallback: non-terminal, or a non-streaming request
	// turn.Stream() is CLAIM-ONCE: only the first agent in the terminal flow that calls it gets ok=true; any other
	// (a sibling in bb.All/bb.Group, a non-terminal agent) gets ok=false and should turn.Reply normally. this is how
	// concurrent agents never interleave two token streams to the one user.
	// why terminal-only: a durable flow hands COMPLETE messages to the next flow (that is what the checkpoint saves),
	// so a live stream cannot cross a flow boundary and stay resumable. the durable chat is always whole; the live
	// stream is a parallel tee that only exists at the terminus.

	// return ..., nil
	var flow bb.Flow = bb.NewFlow().WithId(FlowTalking).WithAgent(agent)
	return flow, nil
}

func flowRemember(ctx context.Context) (bb.Flow, error) {
	// NewModel(ModelCheap) returns a builder seeded from the model registered under "cheap"
	// in main — not a frozen instance. it is still a builder, so a flow can override any
	// setting on top of the registered defaults, e.g.
	//   bb.NewModel(ModelCheap).WithTemprature(0.9)
	// to keep the shared backing model but make just this flow more creative. NewModel()
	// with no tag is the same builder starting from blank instead of a registered base.
	var model bb.Model = bb.NewModel(ModelCheap).WithTemprature(0.9)

	role := bb.Role("You are Marvis' memory keeper. The user is telling you a fact to remember. Restate the fact you will remember, briefly.")

	var agent bb.Agent = bb.NewAgent().
		WithModel(model).
		WithRole(role)

	var flow bb.Flow = bb.NewFlow().WithId(FlowRemember).WithAgent(agent)
	return flow, nil
}

func flowRecall(ctx context.Context) (bb.Flow, error) {
	var model bb.Model = bb.NewModel(ModelCheap) // the model registered under "cheap" in main

	role := bb.Role("You are Marvis recalling what you know. The user is asking about a fact they told you before. Answer from memory, and say so if you don't know it.")

	var agent bb.Agent = bb.NewAgent().
		WithModel(model).
		WithRole(role)

	var flow bb.Flow = bb.NewFlow().WithId(FlowRecall).WithAgent(agent)
	return flow, nil
}

func flowList(ctx context.Context) (bb.Flow, error) {
	var model bb.Model = bb.NewModel(ModelCheap) // the model registered under "cheap" in main

	role := bb.Role("You are Marvis maintaining the user's lists. They may create a list, add, remove, or modify items. Confirm the change you made.")

	var agent bb.Agent = bb.NewAgent().
		WithModel(model).
		WithRole(role)

	var flow bb.Flow = bb.NewFlow().WithId(FlowList).WithAgent(agent)
	return flow, nil
}

func flowHouse(ctx context.Context) (bb.Flow, error) {
	var model bb.Model = bb.NewModel(ModelCheap) // the model registered under "cheap" in main

	role := bb.Role("You are Marvis controlling the smart house. The user wants to read a sensor, set a device, or check something. Report what you did or read.")

	var agent bb.Agent = bb.NewAgent().
		WithModel(model).
		WithRole(role)

	var flow bb.Flow = bb.NewFlow().WithId(FlowHouse).WithAgent(agent)
	return flow, nil
}

// the flow for "extra" leave it empty. I want to explore it myself.
