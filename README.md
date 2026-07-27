## atlas

a minimal coding agent harness built in golang. without any framework.

## features

- 100% written in go
- bubble tea minimal tui
- supports poolside laguna s-2.1
- read, edit, delete, and list file tools
- bash execution tool
- web search and web fetch tools using the tavily api

next plans include building evaluations, adding traces, improving observability, and implementing sandboxing.

## project structure

```text
main.go                 starts the application
internal/agent/         chat loop, poolside requests, and tool-call handling
internal/config/        default application settings
internal/session/       saved conversation storage and restoration
internal/tools/         tool descriptions and filesystem/bash implementations
internal/tui/           bubble tea terminal interface
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
- `/help` — show the available commands

## tools

- `read_file` — read a file
- `list_files` — recursively list a directory
- `edit_file` — replace an exact string or create a new file
- `delete_file` — delete a file
- `bash` — execute any bash command in the working directory
- `web_search` — search the web through the tavily api
- `web_fetch` — extract a web page as markdown through the tavily api

the filesystem tools reject path traversal and protect `.env` from reads, edits, and deletion. the bash tool has unrestricted shell access and can bypass those protections.
