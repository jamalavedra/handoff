package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/joalavedra/handoff/internal/core"
)

// /llms.txt and /llms-full.txt are docs written for AI agents. On a workspace
// host they carry that workspace's real addresses; the tool reference is
// generated from the live tool registry so it never drifts.

type docsCtx struct {
	base, mcp, who string
}

func (s *Server) docsCtx(r *http.Request) docsCtx {
	if o := orgFrom(r); o != nil {
		base := s.issuer(r)
		return docsCtx{base: base, mcp: base + "/mcp", who: o.Name + "'s"}
	}
	b := s.Cfg.BaseURL
	return docsCtx{base: b.String(), mcp: b.Scheme + "://<workspace>." + b.Host + "/mcp", who: "a company's"}
}

func writeMarkdown(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte(body))
}

func (d docsCtx) intro() string {
	return fmt.Sprintf(`# Handoff

> Handoff is %s shared task list for people and agents. Agents connect to its MCP server at %s (streamable HTTP; OAuth 2.1 or an API key) and work on the same tasks people see on the web: ready work, atomic claims, dependencies, comments, @mentions and an inbox. Every change is recorded under the member who made it.
`, d.who, d.mcp)
}

func (s *Server) llms(w http.ResponseWriter, r *http.Request) {
	d := s.docsCtx(r)
	writeMarkdown(w, d.intro()+fmt.Sprintf(`
## Docs

- [Everything in one file](%[1]s/llms-full.txt): connecting a client, authentication, concepts, how to work, and every tool with its parameters.

## Quick start

1. Add %[2]s as a remote MCP server (a custom connector in Claude or ChatGPT) and sign in; a person in the workspace approves the connection. Headless agents send `+"`Authorization: Bearer hnd_...`"+` with a key from the Agents page.
2. Call `+"`get_context`"+` first and follow its guidelines.
3. Then: `+"`what_changed`"+` and `+"`inbox`"+` to catch up, `+"`list_ready`"+` to pick work, `+"`claim_task`"+` before starting, tick its todos with `+"`update_todo`"+` (a one-line result each) and write longer work into the task's note with `+"`create_note`"+`, `+"`comment`"+` to talk with people about it, `+"`close_task`"+` when done.
`, d.base, d.mcp))
}

func (s *Server) llmsFull(w http.ResponseWriter, r *http.Request) {
	d := s.docsCtx(r)
	var b strings.Builder
	b.WriteString(d.intro())
	fmt.Fprintf(&b, `
## Connect a client

The MCP endpoint is %[1]s. Clients that support remote MCP with OAuth discover everything from that address.

- Claude (claude.ai, desktop): Settings → Connectors → Add custom connector → paste %[1]s.
- ChatGPT: Settings → Connectors → Create → paste %[1]s.
- Claude Code: `+"`claude mcp add --transport http handoff %[1]s`"+`
- Cursor / VS Code and other JSON configs: `+"`{\"mcpServers\": {\"handoff\": {\"url\": \"%[1]s\"}}}`"+`
- Headless agents and scripts: create a key on the Agents page and send `+"`Authorization: Bearer hnd_...`"+` with every request.

## Authentication

OAuth 2.1 with PKCE (S256) and dynamic client registration:

1. An unauthenticated request to /mcp returns 401 with a `+"`WWW-Authenticate`"+` header pointing at %[2]s/.well-known/oauth-protected-resource.
2. That names the authorization server; its metadata is at %[2]s/.well-known/oauth-authorization-server.
3. Register at %[2]s/oauth/register, send the person to %[2]s/oauth/authorize, exchange the code at %[2]s/oauth/token. Refresh tokens are supported; revoke at %[2]s/oauth/revoke.

The person who approves the connection becomes the agent's owner. The agent appears in the workspace as its own named member.

## REST

Every MCP tool is also `+"`POST %[2]s/api/tools/<name>`"+` with a JSON body of the tool's parameters, `+"`Authorization: Bearer <token or key>`"+` and `+"`X-Handoff: 1`"+`. Errors come back as JSON with an HTTP status: 400 invalid input, 403 not allowed, 404 not found, 409 conflict (for example a task someone else has claimed).

## Concepts

- Workspace: one company, on its own subdomain or domain. Nothing crosses between workspaces.
- Members: people and agents. Each agent has an owner, a set of allowed actions (read, create, update, close, delete) and the projects it may see. Actions an agent isn't allowed fail with 403.
- Projects: every task lives in one. Private projects are visible only to their members.
- Tasks: title, body, status (open, in_progress, blocked, done, cancelled), priority, assignee, due date, labels. IDs look like `+"`acme-4b2i`"+`. Pass an external_id when creating so retries never duplicate.
- Todos: a task's steps, in order. Each can be ticked and carry a one-line result: what was found or decided for that step.
- Notes: markdown documents inside a project, for longer work (research, shortlists, drafts). A note can be written for a task, which links to it. IDs look like `+"`acme-n4b2i`"+`.
- Dependencies: `+"`link_tasks`"+` says a task can't start until its blocker is finished. A task is ready when it is open, unclaimed and every blocker is done.
- Claims: `+"`claim_task`"+` takes a ready task for a lease (default 60 minutes); claim again to extend. Only the claimer, an admin or the agent's owner can release someone else's claim.
- Trash and approvals: `+"`trash_task`"+` moves a task to a 30-day trash. Some agents need a person to approve; the call then returns pending_approval and the owner gets an approval notification.
- Multiplayer: `+"`what_changed`"+` returns what others did since your last call. Tasks have an `+"`unseen`"+` flag when someone else changed them since you last opened them. Reading or claiming a task shows you as present to people viewing it; `+"`watch_task`"+` can also show you typing.
- Inbox: @Name in a comment notifies that member. Being assigned a task, or an agent you own asking for approval, notifies you too. Read it with `+"`inbox`"+`; opening a task with `+"`get_task`"+` marks its mentions and assignments read, and `+"`mark_read`"+` clears the rest.

## How to work

`, d.mcp, d.base)
	b.WriteString(core.Guidelines)
	b.WriteString("\n\n## Tools\n\nGenerated from the live server. Web-only admin tools are not listed.\n")
	for _, t := range s.tools {
		if !t.Agent {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", t.Name, t.Description)
		switch {
		case t.Destructive:
			b.WriteString("\nDestructive.\n")
		case t.ReadOnly:
			b.WriteString("\nRead-only.\n")
		}
		writeParams(&b, t.InputSchema)
	}
	writeMarkdown(w, b.String())
}

func writeParams(b *strings.Builder, sch *jsonschema.Schema) {
	if sch == nil || len(sch.Properties) == 0 {
		b.WriteString("\nNo parameters.\n")
		return
	}
	b.WriteString("\nParameters:\n\n")
	writeProps(b, sch, "")
}

// writeProps lists properties required-first, one level of nested objects deep.
func writeProps(b *strings.Builder, sch *jsonschema.Schema, indent string) {
	req := map[string]bool{}
	for _, n := range sch.Required {
		req[n] = true
	}
	names := make([]string, 0, len(sch.Properties))
	for n := range sch.Properties {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if req[names[i]] != req[names[j]] {
			return req[names[i]]
		}
		return names[i] < names[j]
	})
	for _, n := range names {
		p := sch.Properties[n]
		line := fmt.Sprintf("%s- `%s` (%s", indent, n, schemaType(p))
		if req[n] {
			line += ", required"
		}
		line += ")"
		if p.Description != "" {
			line += ": " + p.Description
		}
		b.WriteString(line + "\n")
		if indent != "" {
			continue
		}
		if p.Items != nil && len(p.Items.Properties) > 0 {
			writeProps(b, p.Items, "  ")
		} else if len(p.Properties) > 0 {
			writeProps(b, p, "  ")
		}
	}
}

func schemaType(p *jsonschema.Schema) string {
	t := p.Type
	if t == "" {
		for _, x := range p.Types {
			if x != "null" {
				t = x
				break
			}
		}
	}
	if t == "array" && p.Items != nil {
		return "array of " + schemaType(p.Items)
	}
	if t == "" {
		return "any"
	}
	return t
}
