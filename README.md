# Handoff

One task list for a company's people and agents.

![A task in Handoff: its todos, and its chat with the workspace's agent working through them](docs/screenshot.png)

People use the web app. Agents (Claude, ChatGPT, Cursor, Codex, internal bots) connect to the same list over remote MCP or a JSON API. Every agent is a named member with an owner and its own permissions, and every change records who made it, for whom, and through which channel.

Each company gets a workspace at `acme.<your-domain>` (or its own domain), or you self-host a single one.

## What's in it

**Tasks and notes**
- **Tasks** have a title, a line or two of text and **todos**, plus status (Open, In progress, Done), due date, assignee, priority, labels, repeats and dependencies. Finished work is Done; work that won't happen is archived from the ⋯ menu.
- **Projects** are company-wide or private, with a list and a board. Each project has **notes**: markdown documents with what the team knows about it (background, preferences, decisions). Notes only exist inside a project.
- **Chat** on every task, with @mentions, quote-replies, reactions and voice input.

**The workspace's agent**
- An admin picks an agent and any OpenAI-compatible model (a provider, a gateway like OpenRouter, or a local server). It then:
  - **triages** every new task: tidies the title, fills in what the text implies (due date, project, labels), splits multi-step work into todos, and does what research it can;
  - **answers in task chats** when mentioned, and **works on tasks handed to it**, ticking todos with a one-line result. When it needs a decision it **hands the task back** to you with tappable answers, and picks it up again when you reply;
  - **runs automations**: repeating tasks assigned to it, on schedule or with Run now.
- It reads the project's notes as context, and writes a note only when asked.
- **Background jobs**, work nobody waits for, run on the server with retries, and can use a separate, cheaper **background model**. A **monthly token cap** pauses them once the workspace's model calls reach it (they resume next month, or when the cap is raised); triage, chat and the assistant keep working. Admin → Triage shows this month's usage.
- **Its work is visible**: live steps while it works, a trace under each reply ("Looked up Web search · 3 steps"), 👀 → ✅ on the message it answered, and a notification when it's done.
- **Connections** to remote MCP servers give it tools: a free web search in one click, or your own servers with OAuth sign-in. Read-only tools are on by default; destructive ones can't be switched on.
- **Blocks** let it show **images** and **maps** under its messages. The server checks what's shown: a picture is the cited page's own image, served through this server; places are located with OpenStreetMap. Each block kind is a small plugin (`internal/core/blocks.go` and `web/src/blocks/`).
- Everyone also gets a private **assistant** (⌘J) that acts as that agent for them and can only see and change what they can.

**For agents and admins**
- **Agents** connect with OAuth 2.1 (dynamic registration, PKCE) or an API key. Per agent: which projects it sees, what it may do (`read`, `create`, `update`, `close`, `delete`), and what needs a person's approval.
- **Coordination borrowed from [beads](https://github.com/gastownhall/beads)**: a ready list, atomic claims with leases so two agents never take the same task, dependencies, and an append-only history. Beads JSONL imports and exports.
- **Webhooks** deliver an agent's notifications (assigned, mentioned, approval, due) as signed POSTs: `X-Handoff-Signature: t=<unix>,v1=<hex HMAC-SHA256 of "t.body">`, retried with backoff.
- **Isolation**: each workspace is separated by Postgres row-level security under a non-superuser role, not just by `WHERE` clauses. Credentials and tokens are encrypted with `HANDOFF_SECRET`.

## Agent tools (MCP)

MCP endpoint: `https://acme.<domain>/mcp` (streamable HTTP). Add it as a custom connector in Claude or ChatGPT and sign in; headless agents send `Authorization: Bearer hnd_...`. Every tool is also `POST /api/tools/<name>` with a JSON body.

`get_context` · `list_ready` · `search_tasks` · `get_task` · `create_tasks` · `update_task` · `claim_task` · `release_task` · `close_task` · `comment` · `react` · `add_todos` · `update_todo` · `remove_todo` · `list_notes` · `get_note` · `create_note` · `update_note` · `trash_note` · `restore_note` · `link_tasks` · `trash_task` · `restore_task` · `list_projects` · `list_members` · `inbox` · `mark_read` · `what_changed` · `watch_task` · `activity`

Each workspace serves agent docs at `/llms.txt` and `/llms-full.txt`, with its real addresses and a tool reference generated from the live registry. The Agents page has a prompt to paste into any agent that connects it and gets it started.

## Run it locally

Needs Go 1.25+, Node 22 and Docker.

```sh
make dev-db            # Postgres on localhost:55432
make web               # build the web app (embedded into the binary)
make dev               # http://handoff.localhost:8080
```

Open http://handoff.localhost:8080, create a workspace (e.g. `acme`) and use the sign-in link shown on screen (dev mode shows links instead of emailing them). For UI work, `cd web && npm run dev` and open `http://acme.handoff.localhost:5173`; it proxies the API to `:8080`.

Tests need a Postgres they can create databases in:

```sh
HANDOFF_TEST_DATABASE_URL=postgres://postgres:pg@localhost:55432/postgres?sslmode=disable go test ./...
```

## Deploy

```sh
cp .env.example .env   # set HANDOFF_BASE_URL, HANDOFF_SECRET, POSTGRES_PASSWORD, SMTP
docker compose up -d
```

Point `tasks.example.com` and `*.tasks.example.com` at the server. Caddy issues a certificate for each workspace host on first visit, but only for hosts Handoff confirms are real workspaces. For a custom domain, the company sets it on the Admin page and adds a CNAME to its subdomain.

| Variable | |
|---|---|
| `DATABASE_URL` | Postgres URL. The server creates and uses a restricted `handoff_app` role for all queries. |
| `HANDOFF_BASE_URL` | Public URL, e.g. `https://tasks.example.com`. Workspaces are `https://<slug>.tasks.example.com`. |
| `HANDOFF_SECRET` | 32+ random characters, the same on every server and kept across restarts; the server won't start without it outside dev mode. |
| `HANDOFF_SINGLE_ORG`, `HANDOFF_SINGLE_ORG_NAME`, `HANDOFF_ADMIN_EMAIL` | Self-host one workspace directly on `HANDOFF_BASE_URL`. |
| `HANDOFF_ALLOW_SIGNUP` | Whether anyone can create a workspace on the apex (default: on, unless single-org). |
| `HANDOFF_SMTP_URL`, `HANDOFF_MAIL_FROM` | Email for sign-in links and invites. Without SMTP, links are logged. |
| `HANDOFF_GOOGLE_CLIENT_ID`, `HANDOFF_GOOGLE_CLIENT_SECRET` | Optional Google sign-in; redirect URI `<HANDOFF_BASE_URL>/auth/google/callback`. |
| `HANDOFF_COMPOSIO_API_KEY` | Optional [Composio](https://composio.dev) project key. Turns on Settings → Apps, where each person connects their own Gmail, Google Calendar, Drive, Slack, GitHub, Notion or Linear. The agent reads them when it works for that person; anything it would send, post or create (an email, a message, an event, an issue) waits as a card until that person taps to approve it. |
| `HANDOFF_COMPOSIO_X_AUTH_CONFIG` | Optional. The id of a Composio auth config for X, made with your own X developer app (Composio has no shared one). Adds X to Settings → Apps: reading mentions and profiles, and posts and direct messages on approval. |
| `HANDOFF_GEOCODER_URL` | Nominatim-compatible geocoder for map and route blocks (default: OpenStreetMap's). |
| `HANDOFF_ROUTER_URL` | OSRM router for route blocks, with `/routed-foot`, `/routed-bike` and `/routed-car` profiles (default: `https://routing.openstreetmap.de`). Point both at your own servers to keep places in-house. |
| `HANDOFF_DEV` | Shows sign-in links on screen. Never in production. |

On multi-workspace servers, model, webhook and connection URLs may not be private addresses.

## Beads

```sh
handoff bd import -org acme -as you@acme.com -project Engineering < .beads/issues.jsonl
handoff bd export -org acme -as you@acme.com > issues.jsonl
```

Or `POST /api/beads/import` and `GET /api/beads/export` with an agent key. Imports are idempotent (beads IDs are kept as `external_id`) and keep dependencies, comments and closed status.

## Layout

```
cmd/handoff        server and CLI
internal/db        Postgres, migrations, RLS role
internal/core      workspaces, members, agents, tasks, notes, the agent, blocks
internal/auth      sessions, sign-in links, API keys, OAuth
internal/tools     the tool registry shared by MCP and /api/tools
internal/server    HTTP: routing by host, OAuth, MCP, web API, live updates
internal/beads     beads JSONL bridge
web/               React + TanStack Router + Tailwind + shadcn/ui app
android/           native Android app (Kotlin + Jetpack Compose)
desktop/           Electron desktop app — see desktop/README.md
deploy/            Caddyfile
```

## License

MIT
