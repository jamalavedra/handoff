// Package tools is the single registry of operations. The MCP server exposes
// the agent tools; the web app calls every tool over POST /api/tools/{name}.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joalavedra/handoff/internal/core"
)

type Tool struct {
	Name        string
	Description string
	// Agent tools are exposed over MCP; the rest are web-only.
	Agent       bool
	ReadOnly    bool
	Destructive bool
	InputSchema *jsonschema.Schema
	call        func(ctx context.Context, p *core.Principal, raw json.RawMessage) (any, error)
	add         func(s *mcp.Server, p *core.Principal)
}

// Call runs the tool for the web API.
func (t *Tool) Call(ctx context.Context, p *core.Principal, raw json.RawMessage) (any, error) {
	return t.call(ctx, p, raw)
}

// Public converts internal failures into a generic message so SQL details never reach callers.
func Public(err error) error {
	var ce *core.Error
	if errors.As(err, &ce) {
		return err
	}
	slog.Error("tool failed", "err", err)
	return errors.New("internal error; try again or contact your admin")
}

func def[In, Out any](t Tool, fn func(context.Context, *core.Principal, In) (Out, error)) *Tool {
	t.call = func(ctx context.Context, p *core.Principal, raw json.RawMessage) (any, error) {
		var in In
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, &core.Error{Code: core.ErrInvalid, Msg: "invalid input: " + err.Error()}
			}
		}
		out, err := fn(ctx, p, in)
		if err != nil {
			return nil, Public(err)
		}
		return out, nil
	}
	destr := t.Destructive
	mt := &mcp.Tool{Name: t.Name, Description: t.Description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: t.ReadOnly, DestructiveHint: &destr, OpenWorldHint: new(bool)}}
	var err error
	if t.InputSchema, err = jsonschema.For[In](nil); err != nil {
		panic(t.Name + ": " + err.Error())
	}
	mt.InputSchema = t.InputSchema
	if mt.OutputSchema, err = jsonschema.For[Out](nil); err != nil {
		panic(t.Name + ": " + err.Error())
	}
	t.add = func(s *mcp.Server, p *core.Principal) {
		mcp.AddTool(s, mt, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			out, err := fn(ctx, p, in)
			if err != nil {
				var zero Out
				return nil, zero, Public(err)
			}
			return nil, out, nil
		})
	}
	return &t
}

func All(s *core.Service) []*Tool {
	return []*Tool{
		def(Tool{Name: "get_context", Agent: true, ReadOnly: true, Description: "Call this first. Returns who you are, the workspace, its projects, tasks assigned to you (and to the person you act for), tasks you have claimed, the top ready tasks, and the rules for using Handoff."}, s.GetContext),
		def(Tool{Name: "list_ready", Agent: true, ReadOnly: true, Description: "Tasks that can be started now: open, not claimed by anyone, and with every blocker finished. Sorted by priority then due date."}, s.ListReady),
		def(Tool{Name: "search_tasks", Agent: true, ReadOnly: true, Description: "Find tasks by text, project, status, assignee or label. By default excludes done and archived tasks. Set trashed=true to list the trash."}, s.SearchTasks),
		def(Tool{Name: "get_task", Agent: true, ReadOnly: true, Description: "One task with its text, todos (with their ids and results), dependencies, comments and history of who changed what."}, s.GetTask),
		def(Tool{Name: "list_projects", Agent: true, ReadOnly: true, Description: "Projects you can see, with their open task counts."}, s.ListProjects),
		def(Tool{Name: "list_members", Agent: true, ReadOnly: true, Description: "People and agents in this workspace, for assigning tasks."}, s.ListMembers),
		def(Tool{Name: "create_tasks", Agent: true, Description: "Create 1-50 tasks in one call, each with a short plain-text body and optional todos (its steps). Give each an external_id so retrying never creates duplicates. blocked_by can reference other tasks in the same call by their external_id. Ask the user before creating more than a few."}, s.CreateTasks),
		def(Tool{Name: "update_task", Agent: true, Description: "Change a task's title, status (open/in_progress), priority, assignee, due date, labels or project. body (the task's plain text, a line or two) is appended by default; replacing it needs body_mode=replace and confirm=true. Steps go in todos (add_todos)."}, s.UpdateTask),
		def(Tool{Name: "claim_task", Agent: true, Description: "Take a ready task so nobody else works on it, for lease_minutes (default 60). Fails if someone else holds it or a blocker is unfinished. Claim again to extend."}, s.ClaimTask),
		def(Tool{Name: "release_task", Agent: true, Description: "Give a claimed task back without finishing it, optionally with a note on where you left off."}, s.ReleaseTask),
		def(Tool{Name: "close_task", Agent: true, Description: "Mark a task done (or archived, when it won't be done) with a short reason. Returns tasks that became ready because of it."}, s.CloseTask),
		def(Tool{Name: "react", Agent: true, Description: "Toggle an emoji reaction on a comment, e.g. ✅ to acknowledge a request or 👀 to show you're looking into it. Comment ids come from get_task."}, s.React),
		def(Tool{Name: "comment", Agent: true, Description: "Post in a task's chat: a short status, a question, or an answer about the work. Keep what you found concise (a short intro, bullets or a small table, links), and put a step's outcome in its todo's result. To answer one message, pass its id as reply_to, and the part you mean as quote."}, s.Comment),
		def(Tool{Name: "add_todos", Agent: true, Description: "Add steps to a task's todo list, in order: short actions (at most 8 words), with a deadline after a colon when it matters (\"Order: by Oct 24\")."}, s.AddTodos),
		def(Tool{Name: "update_todo", Agent: true, Description: "Tick or untick a todo, rename it, move it, or set its result: one line on what you found or decided for that step (\"6 ideas (Dolly, Ghost Face…)\"). Details go in your comment."}, s.UpdateTodo),
		def(Tool{Name: "remove_todo", Agent: true, Description: "Remove a todo from its task."}, s.RemoveTodo),
		def(Tool{Name: "create_note", Agent: true, Description: "Write a note in a project: a markdown document the team keeps as context for the project. Only when a person asks you to."}, s.CreateNote),
		def(Tool{Name: "update_note", Agent: true, Description: "Change a note's title or body. The body replaces the whole note by default (send the complete updated document; the previous version stays in its history); mode=append adds to the end. Only when a person asks you to."}, s.UpdateNote),
		def(Tool{Name: "get_note", Agent: true, ReadOnly: true, Description: "One note with its markdown body and its history."}, s.GetNote),
		def(Tool{Name: "list_notes", Agent: true, ReadOnly: true, Description: "Notes in the projects you can see, newest first: the team's context for a project. Filter by project or text; page with offset. Bodies are left out; read one with get_note."}, s.ListNotes),
		def(Tool{Name: "trash_note", Agent: true, Destructive: true, Description: "Move a note to the trash (restorable with restore_note)."}, s.TrashNote),
		def(Tool{Name: "restore_note", Agent: true, Description: "Bring a note back from the trash."}, s.RestoreNote),
		def(Tool{Name: "link_tasks", Agent: true, Description: "Say that one task (blocked) can't start until another (blocker) is finished. Set remove=true to undo."}, s.Link),
		def(Tool{Name: "trash_task", Agent: true, Destructive: true, Description: "Move a task to the trash (restorable for 30 days). Some agents need a person to approve; then this returns pending_approval and nothing is deleted yet."}, s.Trash),
		def(Tool{Name: "restore_task", Agent: true, Description: "Bring a task back from the trash."}, s.Restore),
		def(Tool{Name: "inbox", Agent: true, ReadOnly: true, Description: "Your notifications: @mentions in comments, tasks assigned to you, and approval requests from your agents. Newest first."}, s.Inbox),
		def(Tool{Name: "mark_read", Agent: true, ReadOnly: true, Description: "Mark inbox notifications read, by id or all at once. Opening a task with get_task also marks its notifications read."}, s.MarkRead),
		def(Tool{Name: "what_changed", Agent: true, ReadOnly: true, Description: "What other people and agents changed on tasks you can see since your last call (oldest first). Call it at the start of a session to catch up."}, s.WhatChanged),
		def(Tool{Name: "watch_task", Agent: true, ReadOnly: true, Description: "Show others you have a task open for the next 30 seconds, optionally typing a comment. Returns who else is there."}, s.WatchTask),
		def(Tool{Name: "activity", Agent: true, ReadOnly: true, Description: "Recent changes across the workspace, newest first, optionally only one member's."}, s.Activity),

		def(Tool{Name: "create_project", Description: "Create a project."}, s.CreateProject),
		def(Tool{Name: "update_project", Description: "Rename a project or change its description or emoji."}, s.UpdateProject),
		def(Tool{Name: "create_agent", Description: "Add an agent you own."}, s.CreateAgent),
		def(Tool{Name: "update_agent", Description: "Rename, change permissions of, or revoke an agent."}, s.UpdateAgent),
		def(Tool{Name: "list_approvals", ReadOnly: true, Description: "Pending agent requests you can decide."}, s.ListApprovals),
		def(Tool{Name: "decide_approval", Description: "Approve or reject an agent request."}, s.DecideApproval),
		def(Tool{Name: "set_webhook", Description: "Owners and admins: POST an agent's notifications (assigned, mentioned, due, approvals) to a URL, signed with a secret returned once. Empty url removes it; rotate=true issues a new secret."}, s.SetWebhook),
		def(Tool{Name: "test_webhook", Description: "Owners and admins: send a ping to an agent's webhook and report the answer."}, s.TestWebhook),
		def(Tool{Name: "run_automation", Description: "Run a repeating task assigned to an agent now: the workspace's agent takes it on, or another agent's webhook is called with task.due."}, s.RunAutomation),
		def(Tool{Name: "list_connections", ReadOnly: true, Description: "Admins: remote MCP servers the workspace's agent can use, with their tools."}, s.ListConnections),
		def(Tool{Name: "add_connection", Description: "Admins: connect a remote MCP server (streamable HTTP) for the workspace's agent; its read-only tools are switched on."}, s.AddConnection),
		def(Tool{Name: "refresh_connection", Description: "Admins: list a connection's tools again."}, s.RefreshConnection),
		def(Tool{Name: "set_connection_tools", Description: "Admins: choose which of a connection's tools the agent may use."}, s.SetConnectionTools),
		def(Tool{Name: "start_connection_oauth", Description: "Admins: begin signing in to a remote MCP server with its own OAuth; returns the URL to approve the connection at."}, s.StartConnectionOAuth),
		def(Tool{Name: "remove_connection", Description: "Admins: disconnect a remote MCP server."}, s.RemoveConnection),
		def(Tool{Name: "get_triage", ReadOnly: true, Description: "Admins: which agent and model triage new tasks."}, s.GetTriage),
		def(Tool{Name: "set_triage", Description: "Admins: choose the agent that triages each new task people create, and the OpenAI-compatible model it uses."}, s.SetTriage),
		def(Tool{Name: "test_triage", ReadOnly: true, Description: "Admins: ask a model one small question to check a key and model."}, s.TestTriage),
		def(Tool{Name: "disable_triage", Description: "Admins: stop triaging new tasks."}, s.DisableTriage),
		def(Tool{Name: "set_background_model", Description: "Admins: choose a model (often a cheaper one) for the agent's background jobs; empty base_url and model go back to the agent's own model."}, s.SetBackgroundModel),
		def(Tool{Name: "set_monthly_token_cap", Description: "Admins: stop the agent's background jobs once the workspace's model calls this month reach this many tokens; 0 removes the cap."}, s.SetMonthlyTokenCap),
		def(Tool{Name: "get_assistant", ReadOnly: true, Description: "Whether the in-app assistant is set up, and which agent it acts as."}, s.GetAssistant),
		def(Tool{Name: "assistant_chat", Description: "Ask the workspace's assistant (the triage agent and model) to answer or act, given the conversation so far. It acts as that agent on your behalf."}, s.AssistantChat),
		def(Tool{Name: "invite", Description: "Invite a person by email (admins)."}, s.Invite),
		def(Tool{Name: "update_member", Description: "Change a person's role or remove them (admins)."}, s.UpdateMember),
		def(Tool{Name: "update_org", Description: "Change workspace settings (admins)."}, s.UpdateOrg),
	}
}

// Server builds an MCP server for one authenticated caller.
func Server(all []*Tool, version string, p *core.Principal) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "handoff", Title: "Handoff", Version: version}, &mcp.ServerOptions{
		Instructions: "Handoff is " + p.Org.Name + "'s shared task list for people and agents. Call get_context first and follow its guidelines.",
	})
	for _, t := range all {
		if t.Agent {
			t.add(s, p)
		}
	}
	return s
}

// ByName indexes tools for the web API.
func ByName(all []*Tool) map[string]*Tool {
	m := map[string]*Tool{}
	for _, t := range all {
		m[t.Name] = t
	}
	return m
}
