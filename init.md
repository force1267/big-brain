we are creating a project from the ground up, it will be written in go lang

This is essentially a wrapper around one or more LLM, Voice, Vision models. The output of the wrapper is just like standard models we see today. openai api, anthropic compatible, etc...

- we need proper SDKs to consume from and produce to model input outputs
- we need proper SDKs or libraries to provide popular compatible model provider APIs
- we need a popular and battle tested http/net/websocket library for go lang also compatible with serving model APIs
- our technology choice must be opensource, premissble licence, preferably no paid plan behind a paid model company maintaining it

explore these technologies and find the best options that can work toghether and for us

to initiate the project here are some rules that LLMs should follow:
- always log what you do in a file in the project. this is so that other sessions of LLMs runing on this project can undrestand what happened here
- always use local markdown files for textual artifacts that are going to be made, for example the log, the plans, the research. if they produce text, it must be markdown
- pull down the effective go article at https://go.dev/doc/effective_go save it locally and make whatever skill or rule an LLM can undrestand automatically. this must be followed as an absolute rule and with atmost care in this project.
- every go package must have an effective.go file inside it with nothing imported but comment, explaining what it is, what it does, and how its existense is justified by effective go

we are architecting the project:
- everything must be an implementation of an interface
- interfaces can have atmost 3 methods, no more than 2 of them can have active logic, and 1 method can be used for type system compatibility, backward and forward compatibility or type tricks.
- packages as required by effective go are small and focused on a single responsibility. packages that are related to bussiness logic are purely wiring of defined interfaces. in this way we must separate bussiness logic from infrastrucre code
- every package that exports an interface, must also have a mock.go files providing a mock implementation of that interface for dependency injection in testing
- every package must have tests that cover every happy, unhappy, edge case and every branch of code (if, for, functions). code coverage is not enforce but not covering logic and product cases must be prevented. test quality over coverage matters
- use logrus and viper for log and configs, the project must closely follow 12 factor app guides. anything that changes between environments must be derived from env vars.
- every error must be propagated to upper layer with addition of related info from that layer:
    for example package A calls package B and B calls redis
    when B returns "redis error" in two situations, B must have defined two errors (var sit1Err = errors.New("situation 1")) and wrap the internal error (fmt.Errorf("%w: %w", sit1Err, err)) when returns
- use otel for metrics, dummy for local tests, real one for production environment
- every package's metric bearing interfaces must be wrappable in a metrics interface that is preferably defined in telemetry.go and implements that interface and wraps it
    A has an interface (interface Example) that has important metrics
    A's telemetry.go implements a wrapper interface for it (struct MonitoredExample, NewMonitoredExample(e Example))
    When the pakcages, New... function is called, it wraps the real struct with the moniterd one accordingly.
- the pecial init function is used when it is needed
- cmd directory is for producing executables, the main() function is for connecting the executable with the OS, so any initialization must be an internal/ thing not in the main, the main only parses and acts based on command line args and may call the starting points of the program from the internal/
- as said in effective go doc, internal/ is for things that are this project's concern and pkg/ is for exporting things that enable this project to be used as a library. this project apart from serving APIs compatible with model provides, also can be embedded as a library.
- I have other go codebases here that I enjoy working on. I want cross cutting concers and code styles to be influenced by this project: "~/Desktop/projects/gateway/src" read it, see how logs and errors and metrics work, I want good developer experience that it provides but not neccesarily the same interfaces. this project must be a step up, and be better than gateway project

using the rules and guidance above initiate a project friendly to LLMs and enjoyable to maintain
dont create and source files yet, only init the AI Agent coder related files and initial directories, documents, the log, the rules, the initial go files, the cross cutting concerns like logs and otel and config and main. use cli to run go mod and other go commands if needed.

after you finished this step, in the next step, we are going to discuss the project features and discover it as a product and figure out what we are going to develop togheter