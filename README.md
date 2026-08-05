## atlas

a minimal coding agent harness built in golang. wip.

## features

- 100% written in go
- bubble tea minimal tui
- streaming reasoning and response output
- supports openrouter
- read, edit, delete, and list file tools
- bash execution tool
- web search and web fetch tools using tavily

## project structure

```text
main.go                 starts the application
internal/agent/         chat loop, openrouter requests, and tool-call handling
internal/config/        default application settings
internal/session/       saved conversation storage and restoration
internal/tools/         tool descriptions and filesystem/bash implementations
internal/tui/           bubble tea terminal interface
```

`internal` packages keep implementation details private to this application. `main.go` stays at the root, so you can still run the project with `go run .`.

## install

create the shared configuration file at `%userprofile%\.atlas\.env`:

```powershell
New-Item -ItemType Directory -Force "$HOME\.atlas"
Copy-Item .env.example "$HOME\.atlas\.env"
notepad "$HOME\.atlas\.env"
```

set `OPENROUTER_API_KEY` and `TAVILY_API_KEY` in that file, then install atlas:

```bash
go install .
```

make sure `$(go env GOPATH)/bin` is on your `PATH`. atlas can then be started from
any project directory:

```bash
cd ../other-project
atlas
```

the directory where `atlas` is launched is its workspace. atlas loads configuration
from the process environment, the workspace `.env`, then `$HOME/.atlas/.env`. for
local development, run:

```bash
go run .
```

## configuration

all application settings are defined in `internal/config/config.go`:

- model: `poolside/laguna-s-2.1:free`
- base url: `https://openrouter.ai/api/v1`
- reasoning effort: `high`
- maximum completion tokens: `4096`
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
