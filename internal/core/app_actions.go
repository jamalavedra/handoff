package core

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// App actions: when the agent calls one of an app's actions (send an email, post on X, create an
// event), nothing runs. The call is saved with its exact arguments and shown under the agent's message
// as a card with everything that would go out; it runs only when the person whose account it is taps
// Send, and runs exactly what the card showed. The model never runs one, and nobody else can approve.

// maxActionsPerTurn bounds how many actions one message can prepare.
const maxActionsPerTurn = 3

type ActionField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ActionOut is an action as its card shows it.
type ActionOut struct {
	ID     string        `json:"id"`
	App    string        `json:"app"`
	Title  string        `json:"title"`
	Verb   string        `json:"verb"` // what its approve button says: Send, Post, Save draft…
	Fields []ActionField `json:"fields"`
	Status string        `json:"status"`
	Result string        `json:"result,omitempty"`
	Mine   bool          `json:"mine"` // whether the viewer is the one who can approve it
}

type ActionIDIn struct {
	ID string `json:"id" jsonschema:"the action's id"`
}

type DecideActionIn struct {
	ID      string `json:"id" jsonschema:"the action's id"`
	Approve bool   `json:"approve" jsonschema:"true runs it now; false cancels it"`
}

// actionTitles name the catalog's actions on their cards, and their approve buttons.
var actionTitles = map[string][2]string{
	"GMAIL_SEND_EMAIL":                     {"Send an email", "Send"},
	"GMAIL_REPLY_TO_THREAD":                {"Reply to an email", "Send"},
	"GMAIL_CREATE_EMAIL_DRAFT":             {"Save an email draft", "Save draft"},
	"GOOGLECALENDAR_CREATE_EVENT":          {"Create a calendar event", "Create"},
	"SLACK_SEND_MESSAGE":                   {"Send a Slack message", "Send"},
	"NOTION_CREATE_NOTION_PAGE":            {"Create a Notion page", "Create"},
	"LINEAR_CREATE_LINEAR_ISSUE":           {"Create a Linear issue", "Create"},
	"LINEAR_CREATE_LINEAR_COMMENT":         {"Comment on a Linear issue", "Comment"},
	"TWITTER_CREATION_OF_A_POST":           {"Post on X", "Post"},
	"TWITTER_SEND_A_NEW_MESSAGE_TO_A_USER": {"Send a direct message on X", "Send"},
}

// actionFields lists every argument, so the card hides nothing that would be sent: text as is,
// anything else as JSON.
func actionFields(args map[string]any) []ActionField {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []ActionField{}
	for _, k := range keys {
		var v string
		switch x := args[k].(type) {
		case nil:
			continue
		case string:
			v = x
		default:
			b, _ := json.Marshal(x)
			v = string(b)
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		out = append(out, ActionField{Label: strings.ReplaceAll(k, "_", " "), Value: clip(v, 4000)})
	}
	return out
}

func actionOut(id, tool string, args map[string]any, status, result string, mine bool) ActionOut {
	t := actionTitles[tool]
	if t[0] == "" {
		t = [2]string{strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(tool, strings.ToUpper(appOfTool(tool))+"_"), "_", " ")), "Approve"}
	}
	return ActionOut{ID: id, App: appName(appOfTool(tool)), Title: t[0], Verb: t[1], Fields: actionFields(args), Status: status, Result: result, Mine: mine}
}

// proposeAction saves an action the model asked for and puts its card under the message.
func (a *assistantRun) proposeAction(ctx context.Context, t extTool, args string) string {
	step := AssistantStep{Type: "tool", Tool: t.fn}
	var in map[string]any
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil || in == nil {
		step.Summary, step.Error = "Couldn't prepare "+t.conn+" action", "arguments aren't a JSON object"
		a.add(ctx, step)
		return `{"error":"give the arguments as a JSON object"}`
	}
	// The same action prepared again in this turn (a retry) keeps its first card.
	norm, _ := json.Marshal(in)
	key := "action " + t.tool + " " + string(norm)
	if prev, ok := a.turn.done[key]; ok {
		return prev
	}
	n := 0
	for _, b := range a.out.Blocks {
		if b.Type == "action" {
			n++
		}
	}
	if n >= maxActionsPerTurn {
		step.Summary, step.Error = "Didn't prepare another action", "too many in one message"
		a.add(ctx, step)
		return fmt.Sprintf(`{"error":"at most %d actions per message"}`, maxActionsPerTurn)
	}
	var id string
	err := a.s.DB.InOrg(ctx, a.p.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO app_actions (org_id, member_id, task_id, tool, args) VALUES ($1, $2, nullif($3, ''), $4, $5) RETURNING id::text`,
			a.p.Org.ID, a.p.Member.ID, a.task, t.tool, norm).Scan(&id)
	})
	if err != nil {
		step.Summary, step.Error = "Couldn't prepare "+t.conn+" action", "internal error"
		a.add(ctx, step)
		return `{"error":"couldn't save the action"}`
	}
	card := actionOut(id, t.tool, in, "pending", "", true)
	d, _ := json.Marshal(card)
	a.out.Blocks = append(a.out.Blocks, Block{Type: "action", Data: d})
	step.Summary = "Prepared: " + card.Title + " (" + card.App + ")"
	a.add(ctx, step)
	res := `{"ok":true,"status":"waiting for the person to tap Send on the card under your message; it has NOT been sent. Say in one line that it's ready for their tap."}`
	if a.turn.done == nil {
		a.turn.done = map[string]string{}
	}
	a.turn.done[key] = res
	return res
}

type actionRow struct {
	id, member, task, tool, status, result string
	args                                   map[string]any
}

func scanAction(row pgx.Row) (actionRow, error) {
	var r actionRow
	var raw []byte
	err := row.Scan(&r.id, &r.member, &r.task, &r.tool, &raw, &r.status, &r.result)
	if err == nil {
		err = json.Unmarshal(raw, &r.args)
	}
	return r, err
}

const actionCols = `id::text, member_id::text, coalesce(task_id, ''), tool, args, status, result`

// GetAppAction is an action's card as it stands. Its owner sees it, and so does anyone who can see
// the task it was prepared on.
func (s *Service) GetAppAction(ctx context.Context, p *Principal, in ActionIDIn) (ActionOut, error) {
	var r actionRow
	err := s.DB.InOrg(ctx, p.Org.ID, func(tx pgx.Tx) error {
		var err error
		r, err = scanAction(tx.QueryRow(ctx, `SELECT `+actionCols+` FROM app_actions WHERE id::text = $1`, in.ID))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionOut{}, errf(ErrNotFound, "no such action")
	}
	if err != nil {
		return ActionOut{}, err
	}
	if r.member != p.Member.ID {
		if r.task == "" {
			return ActionOut{}, errf(ErrNotFound, "no such action")
		}
		if _, err := s.GetTask(ctx, p, TaskIDIn{ID: r.task}); err != nil {
			return ActionOut{}, errf(ErrNotFound, "no such action")
		}
	}
	return actionOut(r.id, r.tool, r.args, r.status, r.result, r.member == p.Member.ID), nil
}

// DecideAppAction runs or cancels an action. Only the person whose account it uses can, from the app.
func (s *Service) DecideAppAction(ctx context.Context, p *Principal, in DecideActionIn) (ActionOut, error) {
	if err := p.requirePerson(); err != nil {
		return ActionOut{}, err
	}
	if err := s.requireApps(); err != nil {
		return ActionOut{}, err
	}
	next := "cancelled"
	if in.Approve {
		next = "sending"
	}
	// Claim it first, so a double tap or a second tab can't run it twice.
	var r actionRow
	err := s.DB.InOrg(ctx, p.Org.ID, func(tx pgx.Tx) error {
		var err error
		r, err = scanAction(tx.QueryRow(ctx, `SELECT `+actionCols+` FROM app_actions WHERE id::text = $1 AND member_id = $2 FOR UPDATE`, in.ID, p.Member.ID))
		if err != nil {
			return err
		}
		if r.status != "pending" {
			return errf(ErrConflict, "this action is already %s", r.status)
		}
		_, err = tx.Exec(ctx, `UPDATE app_actions SET status = $2, decided_at = now() WHERE id::text = $1`, in.ID, next)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionOut{}, errf(ErrNotFound, "no such action")
	}
	if err != nil {
		return ActionOut{}, err
	}
	r.status = next
	if in.Approve {
		r.status, r.result = s.runAppAction(ctx, p, r)
		err := s.DB.InOrg(ctx, p.Org.ID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE app_actions SET status = $2, result = $3 WHERE id::text = $1`, r.id, r.status, r.result)
			return err
		})
		if err != nil {
			return ActionOut{}, err
		}
	}
	return actionOut(r.id, r.tool, r.args, r.status, r.result, true), nil
}

// runAppAction runs the saved action through the person's own session, as saved, and says how it went.
func (s *Service) runAppAction(ctx context.Context, p *Principal, r actionRow) (status, result string) {
	if !isAction(r.tool) {
		return "failed", "this action isn't offered any more"
	}
	a, err := s.loadAppSession(ctx, p.Org.ID, p.Member.ID)
	if err != nil || a == nil {
		return "failed", "connect " + appName(appOfTool(r.tool)) + " in Settings → Apps first"
	}
	args, _ := json.Marshal(r.args)
	sessions := extSessions{}
	defer sessions.close()
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	t := extTool{conn: appName(appOfTool(r.tool)), tool: r.tool, server: s.appServer(p.Member.ID, a.mcpURL), action: true}
	text, err := s.callExternal(rctx, sessions, t, args)
	if err != nil {
		return "failed", clip(noURL(err), 300)
	}
	// Composio answers with its own success flag inside the tool's text.
	var res struct {
		Successful *bool  `json:"successful"`
		Error      string `json:"error"`
	}
	if json.Unmarshal([]byte(text), &res) == nil && res.Successful != nil && !*res.Successful {
		return "failed", clip(cmp.Or(res.Error, "the app refused it"), 300)
	}
	return "sent", ""
}
