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
internal/evals/         eval suites, deterministic graders, and reports
internal/session/       saved conversation storage and restoration
internal/tools/         tool descriptions and filesystem/bash implementations
internal/tui/           bubble tea terminal interface
cmd/eval/               headless eval command
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

## evals

atlas includes a small headless eval harness. each trial starts with a fresh chat
conversation and uses the same model and system prompt as the interactive agent.
filesystem and bash tools run in a fresh [modal sandbox](https://modal.com/docs/guide/sandboxes).
web search and fetch remain explicit tavily-backed tools on the atlas host.

the harness first makes a local staging copy of the current workspace, excluding
`.git`, `.env`, `.gocache`, `sessions`, `eval-reports`, symbolic links, and
executable files. it uploads that filtered copy to `/workspace` in modal. the
remote sandbox and local staging copy are discarded after each trial, leaving
the source workspace unchanged. sandbox commands receive no atlas, openrouter,
or tavily secrets. sandbox outbound networking stays enabled so repository build
tools can download dependencies.

configure modal with `MODAL_TOKEN_ID` and `MODAL_TOKEN_SECRET` in `.env` or a
modal profile, then run the capability suite:

```bash
go run ./cmd/eval -suite evals/capabilities.json -report eval-reports/capabilities.json
```

override the number of repeated trials or the per-trial timeout:

```bash
go run ./cmd/eval -suite evals/capabilities.json -trials 1 -timeout 10m
```

the default sandbox image is `golang:1.26-bookworm`. override the modal app or
image when needed with `-modal-app` and `-modal-image`.

the included suite contains five focused tasks. each task has one clear outcome
and uses three to five tools; together they cover every atlas tool.

a suite is a json file containing tasks and graders:

```json
{
  "name": "atlas-capabilities",
  "kind": "capability",
  "trials": 2,
  "tasks": [
    {
      "name": "identity",
      "input": "reply with exactly: atlas",
      "graders": [
        { "type": "equals", "value": "atlas" }
      ]
    }
  ]
}
```

the built-in grader types are `contains`, `not_contains`, `equals`, `regex`,
`tool_called`, and `llm_judge`. every grader on a task must pass. reports include
each output, tool trajectory, grader decision, latency, overall pass rate, pass@k,
and pass^k.

`llm_judge` uses the same model configured for atlas through the same openrouter
client. give it one focused rubric:

```json
{
  "type": "llm_judge",
  "rubric": "the answer must be correct, concise, and grounded in the tool result"
}
```

## tools

- `read_file` — read a file
- `list_files` — recursively list a directory
- `edit_file` — replace an exact string or create a new file
- `delete_file` — delete a file
- `bash` — execute any bash command in the working directory
- `web_search` — search the web through the tavily api
- `web_fetch` — extract a web page as markdown through the tavily api

the filesystem tools reject path traversal and protect `.env` from reads, edits,
and deletion. during evals, bash runs inside the disposable modal container and
can alter only that trial's remote workspace and container. interactive atlas
sessions continue to run bash directly on the host.
