package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Every call to the workspace's model goes through chatCompletion: one OpenAI-compatible
// /chat/completions request through the guarded outbound client, with what it cost added to the
// workspace's monthly usage.

type modelUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// unreachableError is a model endpoint that couldn't be reached at all. Triage shows why to admins;
// the assistant tells people only that it was unreachable.
type unreachableError struct{ err error }

func (e *unreachableError) Error() string { return "model endpoint unreachable: " + e.err.Error() }
func (e *unreachableError) Unwrap() error { return e.err }

func (s *Service) chatCompletion(ctx context.Context, st *triageSettings, cred string, body map[string]any) (assistantMsg, error) {
	var msg assistantMsg
	body["model"] = st.Model
	req, _ := json.Marshal(body)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, st.BaseURL+"/chat/completions", bytes.NewReader(req))
	if err != nil {
		return msg, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Authorization", "Bearer "+cred)
	resp, err := s.outbound().Do(hr)
	if err != nil {
		return msg, &unreachableError{err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		m := strings.TrimSpace(string(raw))
		if len(m) > 200 {
			m = m[:200]
		}
		return msg, fmt.Errorf("model endpoint returned %d: %s", resp.StatusCode, m)
	}
	var out struct {
		Choices []struct {
			Message assistantMsg `json:"message"`
		} `json:"choices"`
		Usage modelUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return msg, errors.New("model endpoint returned an unexpected response")
	}
	s.recordUsage(ctx, st, out.Usage)
	return out.Choices[0].Message, nil
}

// completeJSON asks the model for its answer in JSON mode and decodes the first JSON object in it.
func completeJSON[T any](ctx context.Context, s *Service, st *triageSettings, cred string, msgs []map[string]any) (T, error) {
	var v T
	msg, err := s.chatCompletion(ctx, st, cred, map[string]any{
		"messages":        msgs,
		"max_tokens":      12000, // room for the work itself, and thinking models spend part of it before answering
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return v, err
	}
	i := strings.Index(msg.Content, "{")
	if i < 0 {
		return v, errors.New("the model did not answer with JSON")
	}
	// Read the first JSON object only: prose after it (even with braces) is ignored.
	if err := json.NewDecoder(strings.NewReader(msg.Content[i:])).Decode(&v); err != nil {
		return v, errors.New("the model's JSON could not be read")
	}
	return v, nil
}

// recordUsage adds one call to the workspace's usage for this month. It never fails the call.
func (s *Service) recordUsage(ctx context.Context, st *triageSettings, u modelUsage) {
	if st.OrgID == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	err := s.DB.InOrg(ctx, st.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO agent_usage (org_id, month, background, input_tokens, output_tokens, calls)
			VALUES ($1, date_trunc('month', now() AT TIME ZONE 'UTC')::date, $2, $3, $4, 1)
			ON CONFLICT (org_id, month, background) DO UPDATE SET input_tokens = agent_usage.input_tokens + excluded.input_tokens,
				output_tokens = agent_usage.output_tokens + excluded.output_tokens, calls = agent_usage.calls + 1`,
			st.OrgID, st.Background, max(u.PromptTokens, 0), max(u.CompletionTokens, 0))
		return err
	})
	if err != nil {
		slog.Error("model usage", "org", st.OrgID, "err", err)
	}
}
