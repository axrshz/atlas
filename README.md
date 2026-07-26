## atlas

A minimal coding agent harness built in Go using Poolside Laguna S-2.1 model. It can read, list, create, edit, and delete files and also run bash commands inside the working directory. This is built solely for my learning purposes.

## project structure

```text
main.go                 starts the application
internal/agent/         chat loop, poolside requests, and tool-call handling
internal/config/        configuration loading and validation
internal/session/       saved conversation storage and restoration
internal/tools/         tool descriptions and filesystem/bash implementations
```

`internal` packages keep implementation details private to this application. `main.go` stays at the root, so you can still run the project with `go run .`.

## run it

create `.env` from `.env.example`, then set `POOLSIDE_API_KEY`.

```powershell
go run .
```

or build a windows executable:

```powershell
go build -o agent.exe .
.\agent.exe
```

## configuration

the agent starts with these defaults:

- model: `poolside/laguna-s-2.1`
- base url: `https://inference.poolside.ai/v1`
- reasoning effort: `high`
- maximum completion tokens: `4096`
- temperature: `0.7`

set `AGENT_CONFIG` to a json file based on `config.json.example` to override any of those values. unspecified json fields retain their defaults.

environment variables take precedence over the json file:

- `AGENT_MODEL`
- `AGENT_BASE_URL`
- `AGENT_REASONING_EFFORT` (`none`, `minimal`, `low`, `medium`, `high`, or `xhigh`)
- `AGENT_MAX_TOKENS`
- `AGENT_TEMPERATURE` (from `0` to `2`)
- `AGENT_SYSTEM_PROMPT`

## sessions

sessions are stored in `./sessions` by default. set `AGENT_SESSIONS_DIR` to use another directory.

the agent resumes the most recently updated session at startup and saves after each user, assistant, and tool message. use these terminal commands outside the model conversation:

- `/new` — start a new session
- `/sessions` — list saved sessions
- `/delete-session <id>` — delete an inactive session
- `/help` — show the commands

## tools

- `read_file` — read a file
- `list_files` — recursively list a directory
- `edit_file` — replace an exact string or create a new file
- `delete_file` — delete a file
- `bash` — run one approved development command: `go test ./...`, `go vet ./...`, `go build ./...`, `gofmt -w *.go`, or `pwd`

all filesystem tools reject path traversal and protect `.env` from reads, edits, and deletion. the bash tool does not receive api keys or other sensitive environment variables.
