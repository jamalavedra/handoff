package core

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Replies in a task's chat: when a person @mentions the workspace's own agent (the one with a model,
// set up in Settings → Triage) or writes on a task that agent owns, the agent reads the task and the
// conversation, does what was asked with the assistant's tools, acting for that person, and answers
// in the chat. Work it produces goes into the task's details, as everywhere else.

type replyJob struct{ org, task, author string }

func (s *Service) enqueueReply(j replyJob) {
	if s.replyJobs == nil {
		return
	}
	select {
	case s.replyJobs <- j:
	default:
		slog.Warn("chat reply dropped: queue full", "task", j.task)
	}
}

const chatReplyNote = `You are replying in the chat of the task %q (id %s). Your final answer is posted there as your message, so write it to the people in that chat.
- The task's details below are the work so far (often yours). Use them: answer questions about them directly, compare, recommend, explain.
- If the message asks you to add, include, write, update, expand or fix something in the task (research, a comparison, a draft, more detail, a new section), you must call update_task on this task before replying: append to the details, or replace them with confirm when you are rewriting work you wrote. Then reply in one or two sentences saying what you added; don't repeat the content in the reply.
- Use your own knowledge. When something can't be known exactly from here (current prices, stock, the latest news), give your best estimate from what you know, clearly marked as approximate and as of when, and say where to confirm it. For prices, give the launch or typical street prices you know, in the local currency (e.g. "approx. €799 at launch, Oct 2024; often less now"). Never reply only that you lack real-time data.
- Don't post comments with the comment tool; your reply is the comment.

The task's details:
%s`

func (s *Service) replyOne(ctx context.Context, j replyJob) {
	org, err := s.OrgByID(ctx, j.org)
	if err != nil {
		slog.Error("chat reply", "task", j.task, "err", err)
		return
	}
	person, err := s.Principal(ctx, org, j.author, "web")
	if err != nil {
		return
	}
	var agentID, title, body string
	type line struct {
		author, name, kind, body string
	}
	var chat []line
	err = s.DB.InOrg(ctx, j.org, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT agent_id::text FROM triage_settings`).Scan(&agentID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT title, body FROM tasks WHERE id = $1`, j.task).Scan(&title, &body); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT c.author_id::text, m.name, m.kind, c.body FROM comments c
			JOIN members m ON m.org_id = c.org_id AND m.id = c.author_id
			WHERE c.task_id = $1 ORDER BY c.created_at DESC LIMIT 20`, j.task)
		if err != nil {
			return err
		}
		chat, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (line, error) {
			var l line
			return l, r.Scan(&l.author, &l.name, &l.kind, &l.body)
		})
		return err
	})
	if err != nil {
		slog.Error("chat reply", "task", j.task, "err", err)
		return
	}
	// Oldest first; the agent's own messages are its side of the conversation, everyone else's are named.
	var msgs []AssistantMessage
	for i := len(chat) - 1; i >= 0; i-- {
		l := chat[i]
		role, text := "user", l.name+": "+l.body
		if l.author == agentID {
			role, text = "assistant", l.body
		}
		if n := len(msgs); n > 0 && msgs[n-1].Role == role {
			msgs[n-1].Content += "\n\n" + text
		} else {
			msgs = append(msgs, AssistantMessage{Role: role, Content: text})
		}
	}
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "user" {
		return
	}
	for i := range msgs {
		msgs[i].Content = clipBytes(msgs[i].Content, 7000)
	}

	// Show the agent typing while it works.
	typer, err := s.Principal(ctx, org, agentID, "triage")
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(4 * time.Second)
		defer tick.Stop()
		for {
			_, _ = s.WatchTask(ctx, typer, WatchTaskIn{ID: j.task, Typing: true})
			select {
			case <-done:
				_, _ = s.WatchTask(ctx, typer, WatchTaskIn{ID: j.task, Leave: true})
				return
			case <-tick.C:
			}
		}
	}()
	out, err := s.AssistantChat(ctx, person, AssistantChatIn{
		Messages: msgs, Task: j.task,
		Turn: "chat-" + j.task + "-" + strconv.Itoa(len(chat)),
		note: fmt.Sprintf(chatReplyNote, title, j.task, detailsForPrompt(body)),
	})
	close(done)
	reply := strings.TrimSpace(out.Reply)
	if err != nil || out.Error != "" {
		msg := out.Error
		if err != nil {
			msg = err.Error()
		}
		reply = "I couldn't finish that: " + clip(msg, 300)
	}
	if reply == "" {
		return
	}
	// Posted as the agent, for the person it answered.
	agent, err := s.Principal(ctx, org, agentID, ViaAssistant)
	if err != nil {
		return
	}
	agent.Owner = person.Member
	if agent.within, err = s.VisibleProjects(ctx, person); err != nil {
		return
	}
	if _, err := s.Comment(ctx, agent, CommentIn{ID: j.task, Body: reply}); err != nil {
		slog.Error("chat reply: post", "task", j.task, "err", err)
	}
}

// detailsForPrompt is the task body as the agent sees it in a chat reply: clipped, and fenced so it
// reads as content rather than instructions.
func detailsForPrompt(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "(empty)"
	}
	return "<<<\n" + clipBytes(body, 12000) + "\n>>>"
}
