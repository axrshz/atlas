## atlas

a minimal coding agent harness built in golang. wip.

## features

- 100% written in go
- bubble tea minimal tui
- supports openrouter models
- read, edit, delete, and list file tools
- system shell execution tool
- web search and web fetch tools using the tavily api

further plans:

- traces and observability
- sandboxing
- persistent memory
- guardrails
- subagents

## project structure

```text
main.go                 starts the application
internal/agent/         chat loop, openrouter requests, and tool-call handling
internal/config/        default application settings
internal/session/       saved conversation storage and restoration
internal/tools/         tool descriptions and filesystem/shell implementations
internal/tui/           bubble tea terminal interface
```

`internal` packages keep implementation details private to this application. `main.go` stays at the root, so you can still run the project with `go run .`.

## run it

create `.env` from `.env.example`, then set `OPENROUTER_API_KEY`, `OPENROUTER_MODEL`, and `TAVILY_API_KEY`.

```powershell
go run .
```

or build a windows executable:

```powershell
go build -o agent.exe .
.\agent.exe
```

## configuration

all application settings are defined in `internal/config/config.go`:

- model: `openrouter/auto`, overridden by `OPENROUTER_MODEL`
- base url: `https://openrouter.ai/api/v1`
- reasoning effort: `high`
- maximum completion tokens: `4096`
- maximum agent steps per turn: `30`
- temperature: `0.7`
- tool timeout: `2 minutes`
- maximum tool output: `32 KiB`
- sessions directory: `./sessions`

## sessions

sessions are stored in `./sessions`.

the agent resumes the most recently updated session at startup and saves after each user, assistant, and tool message. use these terminal commands outside the model conversation:

- `/new` — start a new session
- `/sessions` — list saved sessions
- `/delete-session <id>` — delete an inactive session
- `/reload` — rebuild and restart the agent after changing the harness
- `/help` — show the available commands

## tools

- `read_file` — read a file
- `list_files` — recursively list a directory
- `edit_file` — replace an exact string or create a new file
- `delete_file` — delete a file
- `bash` — execute a command in the working directory using bash or the native system shell
- `web_search` — search the web through the tavily api
- `web_fetch` — extract a web page as markdown through the tavily api

the filesystem tools reject path traversal and protect `.env` from reads, edits, and deletion. the bash tool has unrestricted shell access and can bypass those protections.

## evaluations

atlas includes a small go evaluation harness with five isolated coding-agent tasks. every task starts in a fresh temporary workspace, records the agent trace and final files, and is graded by the same model configured through `OPENROUTER_MODEL`.

run the suite from the repository root:

```powershell
go run ./cmd/eval
```

cases live in `evals/cases.json`. reports are written to `evals/results/`. the command exits with a non-zero status when any case fails.
