# Handoff

One task list for a company's people and agents.

Each company gets a workspace at `acme.<your-domain>` (or its own domain). People use the web app; agents — Claude, ChatGPT, Cursor, Codex, internal bots — connect to the same list over remote MCP or a JSON API. Every agent is a named member with an owner and its own permissions, and every change is recorded with who did it, for whom, and through which channel.

Ideas borrowed from [beads](https://github.com/gastownhall/beads): a **ready** list (open, unclaimed, nothing it waits on is unfinished), **atomic claims** with leases so two agents never grab the same task, **dependencies**, and an append-only history. Beads repos can import/export JSONL.

## What's in it

- **Workspaces**: multi-tenant by subdomain or custom domain, or `HANDOFF_SINGLE_ORG` to self-host one company. Tenant isolation is enforced by Postgres row-level security under a non-superuser role, not just by `WHERE` clauses.
- **People**: email sign-in links, optional Google sign-in, invites, admins, "anyone with @acme.com can join".
- **Agents**: connect with OAuth 2.1 (dynamic client registration, PKCE S256, rotating refresh tokens) — each connection becomes its own agent owned by the person who approved it — or create one on the Agents page and give it an API key. Per agent: which projects it sees, which actions it may take (`read`, `create`, `update`, `close`, `delete`), and which need a person's approval (delete, by default). Disconnecting revokes keys, tokens and claims at once.
- **Tasks**: projects (company-wide or private), priority P0–P4, assignee, due date, labels, comments, dependencies with cycle detection, claims, trash with restore, idempotent creation via `external_id`, and body replacement only with explicit confirmation.
- **Triage**: an admin picks an agent (often the one the team already talks to in chat) and an OpenAI-compatible model — a provider, a gateway like OpenRouter or the Vercel AI Gateway, or a local server — with an API key or OAuth token. Every task a person creates is then read by that agent: it tidies the title, fills in the due date, priority, project and labels the text implies (never what the creator set), suggests an owner if none was picked, and posts a short plan as a comment. No agent configured, no triage. The credential is stored encrypted with `HANDOFF_SECRET`; on multi-workspace servers the model endpoint may not be a private address.
- **Automations (webhooks)**: give an agent a webhook URL on the Agents page and Handoff POSTs its notifications to it — a task assigned to it, an @mention, an approval, or one of its tasks coming due — signed as `X-Handoff-Signature: t=<unix>,v1=<hex HMAC-SHA256 of "t.body">` with a secret shown once, retried with backoff. Assign the agent a recurring task ("every friday") and it runs on that schedule. Webhook and triage URLs may not be private addresses on multi-workspace servers.
- **Recurring tasks**: daily, weekdays, weekly, every 2 weeks, monthly or yearly; completing one creates the next, due one interval later.
- **Web app**: My tasks, Projects, task detail with its timeline, Activity, Agents (connect, permissions, keys, approvals), Trash, Admin (including Triage), a ⌘K command menu and keyboard shortcuts.

## Agent tools (MCP)

`get_context` · `list_ready` · `search_tasks` · `get_task` · `list_projects` · `list_members` · `create_tasks` · `update_task` · `claim_task` · `release_task` · `close_task` · `comment` · `link_tasks` · `trash_task` · `restore_task` · `activity`

MCP endpoint: `https://acme.<domain>/mcp` (streamable HTTP). Add it as a custom connector in Claude or ChatGPT and sign in. Headless agents send `Authorization: Bearer hnd_...`. The same tools are available as `POST /api/tools/<name>` with a JSON body.

Docs written for AI agents are served at `/llms.txt` and `/llms-full.txt` on every workspace host (and the apex), with that workspace's real addresses and a tool reference generated from the live registry.

The Agents page has a paste-into-any-agent onboarding prompt that connects the agent, calls `get_context`, offers to import open to-dos, and tells it to use Handoff from then on.

## Run it locally

Needs Go 1.25+, Node 22, Docker.

```sh
make dev-db            # Postgres on localhost:55432
make web               # build the web app (embedded into the binary)
make dev               # http://handoff.localhost:8080
```

Open http://handoff.localhost:8080, create a workspace (e.g. `acme`), and use the sign-in link shown on screen (dev mode prints links instead of emailing). `*.localhost` resolves to your machine in Chrome and Firefox. For UI work, `cd web && npm run dev` and open `http://acme.handoff.localhost:5173`; it proxies the API to `:8080`.

Tests need a Postgres they can create databases in:

```sh
HANDOFF_TEST_DATABASE_URL=postgres://postgres:pg@localhost:55432/postgres?sslmode=disable go test ./...
```

## Deploy

```sh
cp .env.example .env   # set HANDOFF_BASE_URL, HANDOFF_SECRET, POSTGRES_PASSWORD, SMTP
docker compose up -d
```

Point `tasks.example.com` and `*.tasks.example.com` at the server. Caddy issues a certificate for each workspace host on first visit, but only for hosts Handoff confirms are real workspaces. Custom domains: the company sets it on the Admin page and adds a CNAME to its subdomain.

| Variable | |
|---|---|
| `DATABASE_URL` | Postgres URL. The server creates and uses a restricted `handoff_app` role for all queries. |
| `HANDOFF_BASE_URL` | Public URL, e.g. `https://tasks.example.com`. Workspaces are `https://<slug>.tasks.example.com`. |
| `HANDOFF_SECRET` | Random secret, required in production. |
| `HANDOFF_SINGLE_ORG`, `HANDOFF_SINGLE_ORG_NAME`, `HANDOFF_ADMIN_EMAIL` | Self-host one workspace directly on `HANDOFF_BASE_URL`. |
| `HANDOFF_ALLOW_SIGNUP` | Whether anyone can create a workspace on the apex (default: on, unless single-org). |
| `HANDOFF_SMTP_URL`, `HANDOFF_MAIL_FROM` | Email for sign-in links and invites. Without SMTP, links are logged. |
| `HANDOFF_GOOGLE_CLIENT_ID`, `HANDOFF_GOOGLE_CLIENT_SECRET` | Optional Google sign-in; redirect URI `<HANDOFF_BASE_URL>/auth/google/callback`. |
| `HANDOFF_DEV` | Shows sign-in links on screen. Never in production. |

## Beads

```sh
handoff bd import -org acme -as you@acme.com -project Engineering < .beads/issues.jsonl
handoff bd export -org acme -as you@acme.com > issues.jsonl
```

Or `POST /api/beads/import` / `GET /api/beads/export` with an agent key. Imports are idempotent (beads IDs are stored as `external_id`), keep `blocks` dependencies, comments and closed status. Exports produce beads-format issues with dependencies.

## Layout

```
cmd/handoff        server and CLI
internal/db        Postgres, migrations, RLS role
internal/core      workspaces, members, agents, permissions, tasks
internal/auth      sessions, sign-in links, API keys, OAuth
internal/tools     the tool registry shared by MCP and /api/tools
internal/server    HTTP: routing by host, OAuth, MCP, web API
internal/beads     beads JSONL bridge
web/               React + TanStack Router + Tailwind app
deploy/            Caddyfile
```

## License

MIT
