## atlas

A minimal coding agent harness built in Go using Poolside Laguna S-2.1 model. It has a Bubble Tea terminal interface, filesystem and bash tools, and Tavily-powered web search and page fetching. This is built solely for my learning purposes.

Next plans include building an evaluation suite, adding traces and making it observable and implementing sandboxing.

## project structure

```text
main.go                 starts the application
internal/agent/         chat loop, poolside requests, and tool-call handling
internal/config/        default application settings
internal/session/       saved conversation storage and restoration
internal/tools/         tool descriptions and filesystem/bash implementations
internal/tui/           Bubble Tea terminal interface
```

`internal` packages keep implementation details private to this application. `main.go` stays at the root, so you can still run the project with `go run .`.

## run it

create `.env` from `.env.example`, then set `POOLSIDE_API_KEY` and `TAVILY_API_KEY`.

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

- model: `poolside/laguna-s-2.1`
- base url: `https://inference.poolside.ai/v1`
- reasoning effort: `high`
- maximum completion tokens: `4096`
- temperature: `0.7`
- sessions directory: `./sessions`

## sessions

sessions are stored in `./sessions`.

the agent resumes the most recently updated session at startup and saves after each user, assistant, and tool message. use these terminal commands outside the model conversation:

- `/new` — start a new session
- `/sessions` — list saved sessions
- `/delete-session <id>` — delete an inactive session
- `/reload` — rebuild and restart the agent after changing the harness
- `/help` — show the commands

## tools

- `read_file` — read a file
- `list_files` — recursively list a directory
- `edit_file` — replace an exact string or create a new file
- `delete_file` — delete a file
- `bash` — execute any bash command in the working directory
- `web_search` — search the web through Tavily
- `web_fetch` — extract a web page as markdown through Tavily

the filesystem tools reject path traversal and protect `.env` from reads, edits, and deletion. the bash tool has unrestricted shell access and can bypass those protections.
