# The agent harness: memory, playbooks, explicit proactivity, health

- Status: proposed
- Written: 2026-10-19, against `a4e178f`
- Reference: [OpenMausBot](https://github.com/milind-soni/OpenMausBot) (Apache-2.0), read at its `main` the same day

## Summary

The workspace's agent does each job from scratch. Triage, chat replies, take-ons and automations see the
task, its project's notes and the workspace roster, and nothing the agent learned last time. Its failures
end as a chat line and a log entry. Its automations repeat on six fixed rhythms.

This plan adds four things, in this order. Each phase ships on its own:

1. **Memory.** The agent notices lasting facts in chats, recalls them in later turns, and tidies them
   every night.
2. **Playbooks.** After a long piece of work the agent writes the procedure down, or sharpens one it
   already has, so an automation gets better each time it runs.
3. **Explicit proactivity.** Cron schedules with catch-up and retry, a todo-driven "keep going" loop on
   tasks handed to the agent, and built-in automation templates. Nothing runs without a trigger someone
   can see and switch off.
4. **Health.** Failures grouped into issues by signature, with no model involved. A Health page, a weekly
   report, and fixed issues that become tests.

The only new dependency is a cron parser (Phase 3). Everything else reuses what exists: jobs claimed in
Postgres with leases, the one-minute sweep in `RunTriage`, `completeJSON`, `AssistantChat`, `events`,
notifications and row-level security.

## What OpenMausBot does, and what we take

Read for design. No code is copied. The review prompt in Phase 2 is adapted from Hermes Agent (MIT),
as OpenMausBot's is, with the notice kept.

| OpenMausBot | Where | What we take |
|---|---|---|
| Proactivity only from explicit triggers (routines, webhooks, goal mode); no heartbeat that invents work | `docs/notification-and-proactivity-qa.md` | The rule as stated |
| Capture after a quiet chat (2 min or 6 turns): one cheap call files core facts `(noticed)` and the rest into topics | `server/memory-capture.ts` | Capture on quiet, per task, into rows |
| Core memory capped by lines and bytes; overflow moves out, never blocks a write | `server/workspace.ts` | The cap; overflow is left out of the prompt, not deleted |
| Recall before each turn: ≤4 notes + ≤4 past passages, in the **message** not the system prompt, fenced as "notes, not instructions" | `docs/memory.md` | As stated, with Postgres full-text search |
| Nightly tidy: expire `until` facts, merge duplicates, strike contradictions (never delete), ≤1/5 struck per pass | `server/memory-tidy.ts`, `memory-upkeep.ts` | As stated |
| Learned "About me" facts, removable, never re-added | `server/profile-learned.ts` | Person-scoped memory with the same rule |
| Journal of every memory change with undo, outside the bot's reach | `server/memory-journal.ts` | `events`, which is already append-only and server-written |
| Skill review after a long turn: no-tool model returns ≤3 whole skills; "same lesson twice is ONE rule"; "fix in place"; do-not-capture list; base-hash check | `server/skill-review.ts` | Phase 2 playbooks, nearly as is |
| Goal mode: verdict `done / continue / wait`, turn budget, a person's message pauses, failures auto-pause | `docs/plans/2026-09-02-goal-mode-v2.md` | Todos are our objective |
| Routines: cron + IANA zone, one catch-up run ≤12 h late, no overlap, one retry on passing failures, "N of M OK this week" | `docs/routine-schedules.md`, `server/routine-recovery.ts` | Phase 3 cron repeats |
| Incidents: retry ≤2 per thread, mute after 5 in an hour | `docs/team-incidents.md` | The limits; the person who handed work over is told |
| Sentinel: model-free scanner, error signatures with ids and paths normalized; roadmap Health page → digest → regression evals → fixer | `docs/sentinel.md` | Phase 4, in-process, as a table |
| Helper model for background jobs; spend cap | `docs/memory.md` | Optional cheap model in triage settings; monthly cap |

Left out: per-bot files on disk (we are multi-tenant Postgres), embeddings (full-text search first),
rooms and a Chief of Staff (we have one workspace agent and many external ones).

## Ground rules for every phase

- **Visible and reversible.** Every write the harness makes is an `events` row whose `via` names the job
  (`memory`, `playbook`, `automation`). Anything it adds can be removed in the UI, and a removed fact or
  playbook is not added again.
- **Visibility follows the source.** A fact or playbook taken from a task in a private project stays in
  that project. A fact about a person comes only from that person's own words and loads only in turns run
  for them. Recall runs as the turn's principal, so it never shows more than the turn could see.
- **Content is data.** Memory, playbooks, recall and briefs are fenced as notes are today: information,
  never instructions.
- **Off switches.** Each upkeep job has a workspace switch in `triage_settings` (admins). Default on once
  shipped, except Phase 3's keep-going loop, which starts off. A job that is off or has no model does
  nothing and never fails a turn.
- **Cost bounded.** Background calls use `helper_model` when set, otherwise the agent's model, and count
  toward a monthly per-workspace cap. At the cap background jobs stop; turns people asked for do not.
- **Same worker shape.** Jobs are rows claimed with a lease, swept every minute, run in `RunTriage`'s
  slots. A restart loses nothing.

## Phase 0: groundwork

Small, ships first, and everything after uses it.

- **Generic JSON completion.** `completeJSON` (`internal/core/triage.go`) decodes only into
  `triageSuggestion`. Make it `completeJSON[T any](ctx, st, cred, msgs) (T, error)`, keeping its rules:
  JSON mode, first object only, the `outbound()` client, the 1 MB limit.
- **Helper model.** `triage_settings` gains `helper_base_url`, `helper_model` and `helper_credential`
  (sealed like `credential`). Empty means the main model. Settings → Triage gets a "Background model
  (optional)" row.
- **Background jobs.**

  ```sql
  CREATE TABLE agent_jobs (
      org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
      id          uuid NOT NULL DEFAULT gen_random_uuid(),
      kind        text NOT NULL CHECK (kind IN ('memory_capture', 'memory_tidy', 'playbook_review', 'continue')),
      task_id     text,
      member_id   uuid,              -- the person the job works for, when there is one
      due_at      timestamptz NOT NULL DEFAULT now(),
      state       text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'done', 'failed', 'skipped')),
      attempts    int  NOT NULL DEFAULT 0,
      claim       uuid,
      claim_until timestamptz,
      result      jsonb NOT NULL DEFAULT '{}',
      created_at  timestamptz NOT NULL DEFAULT now(),
      PRIMARY KEY (org_id, id)
  );
  -- One pending job of a kind per task: a later trigger moves due_at instead of queuing another.
  CREATE UNIQUE INDEX agent_jobs_one ON agent_jobs (org_id, kind, coalesce(task_id, '')) WHERE state = 'pending';
  ```

  RLS like every table. The minute sweep in `RunTriage` also claims due `agent_jobs`. Debounce is an
  upsert that pushes `due_at` later.
- **Spend.** Each model response's `usage` is added to `agent_usage (org_id, month, background bool,
  input_tokens, output_tokens, calls)`. An optional `monthly_token_cap` lives in triage settings. The
  admin page shows this month's usage.

Tests: generic decode (prose after the JSON, no JSON); helper fallback; debounce (two triggers make one
row with the later `due_at`); the cap stops background jobs only.

## Phase 1: memory

### Data

```sql
CREATE TABLE agent_memory (
    org_id         uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    id             uuid NOT NULL DEFAULT gen_random_uuid(),
    scope          text NOT NULL CHECK (scope IN ('workspace', 'project', 'person')),
    project_id     uuid,            -- scope = project
    member_id      uuid,            -- scope = person: whom it is about, and the only one it loads for
    text           text NOT NULL CHECK (length(text) <= 500),
    source_task    text,
    source_comment uuid,
    noticed        bool NOT NULL DEFAULT true,   -- written by capture, not asked for
    pinned         bool NOT NULL DEFAULT false,  -- loads first; the cap and the tidy never move it
    until_date     date,                         -- stops loading the day after
    superseded_at  timestamptz,
    superseded_by  uuid,
    removed_at     timestamptz,                  -- a person took it out: never added again
    created_by     uuid NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    tsv            tsvector GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED,
    PRIMARY KEY (org_id, id),
    CHECK ((scope = 'project') = (project_id IS NOT NULL)),
    CHECK ((scope = 'person') = (member_id IS NOT NULL))
);
CREATE INDEX agent_memory_tsv ON agent_memory USING gin (tsv);
```

Plus GIN indexes on `to_tsvector('simple', body)` for `notes` and `comments`, for recall.

Who sees what: workspace facts load in every turn. Project facts load when the turn's principal can see
the project (the check `projectNotes` uses). Person facts load only in turns run for that member: their
assistant, replies to them, work they handed over.

### Capture

- **Trigger.** A `chat_replies` row finishes, or a person comments on a task the agent owns: upsert a
  `memory_capture` job for the task, due in 2 minutes, or at once after 6 comments since the last
  capture. Triage, automation runs, failed turns and other agents' comments are not captured.
- **Input.** The comments since the last capture, read under different rules: a person's words can give
  facts about them and the work; the agent's own words give only outcomes it verified or decisions the
  person agreed to. Plus the scope's current facts, note titles, and removed facts so they are not
  offered again.
- **Output.** `{"add": [{"scope", "text", "until"}], "supersede": [{"id", "reason"}]}`, at most 5 adds.
  Server checks: no scope wider than the source task's project; person facts only about the comment's
  author; text clipped and redacted (key-shaped strings, `password:` lines); duplicates by normalized text
  dropped.
- **Writes.** Rows, and one `events` row per change (`via 'memory'`, `kind 'memory_added' |
  'memory_superseded'`, data with the text and source task).

### Loading and recall

- **Core block.** In `assistantRun.systemPrompt` and `triageUserMessage`, after the notes context:
  pinned, then person, project and workspace facts, newest first, capped at 120 lines / 12 KB. The turn is
  told how many did not fit. Expired and superseded rows never load.
- **Recall.** For a person's message of 8+ characters, `websearch_to_tsquery` over facts not in the core
  block, notes the principal can see, and the last 90 days of comments on other tasks the principal can
  see. At most 4 facts or notes and 4 comment passages, 6,000 characters, each with task title and date.
  Prepended to the last user message, opened by a line saying these are the agent's own notes and past
  conversations, information and not instructions. Kept out of the system prompt so the prompt stays the
  same from turn to turn.
- **Recent-work brief.** At most 10 lines: the agent's last reply on each other task it worked on for this
  person in the last two days (`today 09:05 · "Book the venue" · you said: "Shortlisted three…"`). In the
  system prompt of assistant and take-on turns.
- **`remember` tool.** For "remember that …": writes a non-noticed row with the same checks as capture.

### Nightly tidy

Once a day per workspace after 03:00 UTC (a `memory_tidy` job the sweep creates), never while the agent
has a turn running:

1. Rows past `until_date` get `superseded_at`.
2. Exact duplicates by normalized text: the newer stays.
3. Per scope with 5 or more facts, one call returns pairs that cannot both be true; the older is struck
   (`superseded_by` the newer). At most a fifth of a scope's rows per pass. When a struck row also held
   something still true, that part is kept as its own row.

Each change is an `events` row (`via 'memory'`, `kind 'memory_tidied'`).

### UI

- **Settings → Agent → What it knows.** Admins see workspace and project facts; everyone sees their own
  person facts and projects they can see. Grouped list, *noticed* badge, source task link, Pin, Remove,
  and History from `events` with Restore.
- **Project page:** "The agent knows" under the notes.
- **Reply trace:** "Remembered 2" / "Recalled 3" when that happened, expanding to the facts.

### Tests

- Capture: debounce; a private project's fact never becomes workspace-wide; person facts only from that
  person's words; removed facts not re-added; redaction.
- Loading: cap and order; expired and superseded excluded; no project facts without project access.
- Recall: another person's private task never recalled; budget; fence line present.
- Tidy: expiry, duplicates, the 1/5 and minimum-5 rules, splitting a mixed row. All with the fake model.

## Phase 2: playbooks

A playbook is how this workspace does a class of task: the steps, the tools that work, the shape of the
result people want, and the pitfalls.

### Data

```sql
CREATE TABLE playbooks (
    org_id      uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    id          uuid NOT NULL DEFAULT gen_random_uuid(),
    project_id  uuid,                 -- NULL: workspace-wide
    name        text NOT NULL,        -- kebab-case, unique per scope
    description text NOT NULL,        -- one line: when to use it
    body        text NOT NULL CHECK (length(body) <= 12000),
    sha         text NOT NULL,
    state       text NOT NULL DEFAULT 'proposed' CHECK (state IN ('proposed', 'active', 'off')),
    learned     bool NOT NULL DEFAULT true,  -- written by review; people can edit any playbook
    updated_by  uuid NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id)
);
```

Body sections: **When to use**, **Procedure**, **Pitfalls**, **Verification**.

### Review

- **Trigger.** A take-on, automation run or chat reply ends with 5 or more tool steps (counted from the
  reply's `activity`), or loaded a playbook: upsert a `playbook_review` job for the task, due once the
  turn settles. A per-task step counter resets when a playbook changes, so a long thread is reviewed once
  per stretch of work.
- **Input.** The task; a bounded transcript (recent comments in full, older ones summarized, 40,000
  characters, as `reviewTranscript` in OpenMausBot's `skill-review.ts`); the playbook index; full bodies
  of playbooks loaded or matching by name (30,000 characters).
- **Prompt.** Adapted from Hermes Agent `agent/background_review.py` (MIT, © 2025 Nous Research) via
  OpenMausBot; keep the notice in a comment. The rules that matter:
  - The same lesson learned twice is one rule: sharpen the existing sentence instead of adding a copy.
  - Fix a playbook in place when it is wrong. Never append "UPDATE: actually…".
  - A pitfall is a general rule plus one clause of why. No dates, no story of this task.
  - **Do not capture:** failures that depend on setup (capture the fix instead), claims that a tool does
    not work, errors that went away, one-off tasks, unresolved attempts, secrets.
- **Output.** `{"changes": [{"action": "create"|"update", "name", "description", "body", "gist"}], "note"}`,
  at most 3. Server checks, as `parseSkillReview` does: update only playbooks shown to the reviewer and
  only while `sha` still matches; create only under free names; one change per playbook; a playbook from
  a private project's task stays in that project.
- **Apply.** An update to an active learned playbook applies at once, with an `events` row and undo. A new
  playbook arrives `proposed`: admins get an `approval` notification and a card in Settings → Agent →
  Playbooks with the gist and body. People can edit, turn off or delete any playbook.
- **Backoff.** A failed review retries after 15 minutes; after 3 failures in a row its steps are dropped.

### Use

- The system prompt lists active playbooks the turn can see, `name: description`, at most 40 or 2,000
  characters. A `get_playbook(name)` tool loads one; the reply trace says "Followed *weekly-competitor-scan*".
- An automation's task can name a playbook in its text; triage and take-on load it directly.

### Tests

Trigger threshold and counter reset; a stale `sha` refused; a private project's playbook invisible
elsewhere; proposal flow (approve, decline, edit); fake-model fixtures where nothing should be captured
(a one-off task, an unresolved failure).

## Phase 3: explicit proactivity

The agent never starts work no one set up. Everything here is a repeating task, a hand-off or a trigger
someone configured, visible on the task and stoppable there.

### Cron repeats

- `tasks` gains `repeat_cron text`, `repeat_tz text` (IANA, required with cron) and `repeat_every
  interval`; `repeat` gains `'cron'` and `'interval'`. Five fields, minute resolution, no macros. Parse
  with a maintained Go cron library (e.g. `github.com/robfig/cron/v3`, MIT; check `L` and `#` support or
  pick one that has it) and use the same parser for previews.
- **A time, not just a date.** Cron needs a time of day: cron tasks also set `due_time timestamptz`, which
  the webhook tick's "due" check and take-on read. `due_at` keeps its meaning.
- The repeat menu adds "Custom schedule…" with the next three runs in the chosen zone. `create_task` and
  `update_task` take `repeat_cron` and `repeat_tz`; the agent confirms with the next three runs first.
- **Catch-up.** A run up to 12 hours late runs once. Older missed runs leave one `automation_missed`
  event and the series moves on without replaying each one.
- **No overlap.** An occurrence whose agent is still on the previous one waits (one pending take-on per
  task already holds).
- **One automatic retry** for passing failures (timeout, 5xx, rate limit), after 15 seconds (a minute
  after a rate limit), with the same turn id so finished writes are not repeated. No retry for a bad
  credential, a revoked agent or the spend cap; the owner is told once per cause until a run succeeds.
- Automations show "4 of 5 runs OK this week", counted from `events`.

### Keep going: todos as the goal

When a task handed to the agent (take-on) still has open todos after a turn:

1. If the agent did not call `ask_person` and nobody commented during the turn, a small verdict call
   returns `{"verdict": "done"|"continue"|"wait", "why"}`.
2. `continue` upserts a `continue` job that runs another take-on turn with a plain message ("Carry on with
   the next open todo."). `wait` parks with a one-line comment. `done` stops.
3. **Budget:** 8 turns per hand-off, then a comment saying so and a hand-back with "Keep going" as an
   answer.
4. **A person's comment preempts:** it gets a normal reply and cancels the pending `continue`.
5. **Failures:** two failed turns in a row stop the loop with a hand-back. Trouble with the verdict call
   counts as `wait`.

A workspace switch, off for the first release; the task's ⋯ menu shows "Agent keeps going" while active.

### Templates

Built-in automations are ordinary repeating tasks assigned to the agent, created from a gallery in
Automations, so anyone can read, edit, pause or delete them:

- **Morning brief** (weekdays 08:30, creator's zone): due today, overdue, waiting on you, and what the
  agent finished since yesterday, as a comment plus an `agent_done` notification.
- **Weekly review** (Friday 16:00): what moved, what's stuck (in progress, unchanged for 7 days), and
  suggestions as todos. It changes no other task without asking.
- **Inbox sweep** (with Gmail connected through Apps): emails that ask for something become proposed
  tasks for that person, read-only like Apps today.

### Event triggers

A Composio trigger or an inbound webhook (`POST /api/hooks/<token>`, signed like outgoing webhooks)
creates a task in a chosen project, optionally assigned to the agent, and triage runs as for any new task.
Rate-limited per trigger.

### Tests

Cron and preview across a DST change; the catch-up window; no duplicate occurrence after a reopen
(`next_id` still holds); one retry, passing causes only; keep-going budget, preemption by a comment, the
two-failure stop; templates create ordinary tasks.

## Phase 4: health

### Issues

```sql
CREATE TABLE agent_issues (
    org_id       uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    sig          text NOT NULL,       -- kind + ':' + 16 hex of sha256(tool + normalized error)
    kind         text NOT NULL CHECK (kind IN ('tool_error', 'turn_failed', 'triage_failed', 'webhook_failed', 'claim_lapsed', 'automation_failed')),
    title        text NOT NULL,       -- the normalized error, never raw content
    tool         text NOT NULL DEFAULT '',
    count        int  NOT NULL DEFAULT 1,
    wasted_turns int  NOT NULL DEFAULT 0,
    first_at     timestamptz NOT NULL DEFAULT now(),
    last_at      timestamptz NOT NULL DEFAULT now(),
    last_task    text,                -- evidence, shown only to people who can see the task
    state        text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'muted', 'fixed')),
    PRIMARY KEY (org_id, sig)
);
```

- **Normalize before hashing**, as Sentinel does: paths, numbers, UUIDs, hex, URLs, emails and quoted
  values become placeholders, so "no task T-12" and "no task T-104" are one issue.
- **Fed from where failures already are:** a step with `Error` in a turn's activity, a turn with
  `out.Error`, `triage_failed` events, `webhook_deliveries.failed_at`, a lapsed `triage_claim_until` or
  `chat_replies.claim_until`, a failed automation. An upsert after the failure that never fails the caller.
  No model.
- **Limits:** a task whose turns fail 5 times in an hour is not retried by the harness until the hour
  passes, and the person who handed it over is told once.

### Surfaces

- **Admin → Health:** open issues ordered by wasted turns, with counts, first and last seen, the last task
  (if visible), Mute and Mark fixed. A fixed issue that comes back reopens and says so.
- **Weekly report:** a Health template in the Phase 3 gallery that posts the top issues each Monday.

### Making fixes stick

- An issue fixed in Handoff's own code gets a regression test in the PR that fixes it; the PR template
  asks for it.
- Later, only once that works: a scheduled agent takes the top open issues that are Handoff bugs (not a
  workspace's setup), reproduces each against a fake-model instance and opens a PR for review. It never
  merges.

## Repo hygiene that goes with this

- **`AGENTS.md`** at the root: how to run tests (`HANDOFF_TEST_DATABASE_URL`, `make check`); migrations
  are numbered and never edited after release; every table gets RLS; comments only for what code cannot
  say; verify against a disposable database, never a live workspace.
- **`docs/verification.md`:** driving a turn end to end with the fake model, for people and coding agents
  before they say a change to the agent works.

## Order of work

| # | Slice | Needs |
|---|---|---|
| 0a | Generic `completeJSON`, helper model settings | — |
| 0b | `agent_jobs` and the sweep; usage counter and cap | 0a |
| 1a | `agent_memory`, `remember`, core block in prompts, panel (list, pin, remove) | 0b |
| 1b | Capture | 1a |
| 1c | Recall and the recent-work brief | 1a |
| 1d | Nightly tidy; history and restore in the panel | 1b |
| 2a | `playbooks`, index in prompts, `get_playbook`, editor | 0b |
| 2b | Review and proposals | 2a |
| 3a | Cron and interval repeats, due time, catch-up, retry, run stats | cron library |
| 3b | Keep-going loop | 0b |
| 3c | Templates; inbound triggers | 3a |
| 4a | `agent_issues`, feeders, retry and mute limits | — |
| 4b | Health page, weekly report | 4a, 3c |
| — | `AGENTS.md`, `docs/verification.md` | — |

Each row is one PR with its migration, tests and README lines.

## Open questions

1. Can a person fact reach a shared task chat ("Ana is vegetarian" when planning a team dinner)?
   Proposal: only when the person can see the task and marked the fact shareable; default no.
2. Are new playbooks active at once on single-workspace self-hosts? Proposal: still proposed, one tap.
3. Cron zone default: the creator's browser zone or a workspace zone? There is no workspace zone yet.
4. Once proven, is keep-going on by default for hand-offs, or opt-in per task?
5. Full-text search: `simple` works across languages but doesn't stem. Per-workspace language, or
   `simple` now and trigrams later?
6. Is a token cap enough, or do admins want a money cap (needs a price table per model)?

## Progress

- **0a, 0b: done** on `agent/background-jobs` (PR merges first): `internal/core/model.go` (every model call,
  usage counted), `background.go` (background model, monthly cap, `backgroundModel()`), `jobs.go`
  (`agent_jobs` queue: `enqueueJob`, `jobKinds` registry, retries, run inside `RunTriage`), migration 025,
  Admin → Triage section.
- **1a: started** on `agent/memory`, stacked on `agent/background-jobs`. Only migration
  `026_agent_memory.sql` exists (applies cleanly; `go test ./internal/db` passes). Next steps for 1a:
  1. `internal/core/memory.go`: `Remember` / `ListMemory` / `PinMemory` / `RemoveMemory` service calls
     with the visibility rules in Phase 1 (project facts follow `c.canSeeProject`; person facts only for
     that member; removing a fact sets `removed_at`, never deletes).
  2. A fenced memory block (`memoryContext`, modelled on `notesContext` in `notes.go`) added in
     `assistantRun.systemPrompt` (`assistant.go`) and `triageUserMessage` (`triage.go`), capped at
     120 lines / 12 KB, pinned first, expired and superseded rows excluded.
  3. A `remember` assistant tool in `assistantTools` (`assistant.go`), plus web-only tools in
     `internal/tools/tools.go`.
  4. Memory events must **not** go into `events` with task ids: `Activity` (`members.go`) shows events
     with `task_id IS NULL` to every member, so person facts would leak there. Either keep memory
     history in `agent_memory` itself (it already keeps removed and superseded rows) or filter those
     kinds out of `Activity`.
  5. Settings page "What the agent knows" (`web/src/pages/`, tab in `settings.tsx`).
  6. Tests next to `assistant_test.go` (use `scriptedModel` / `assistantEnv`).
