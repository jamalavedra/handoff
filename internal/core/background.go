package core

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The background model writes what nobody waits for (memory upkeep, reviews): any OpenAI-compatible
// model, usually a cheaper one than the agent's. Without one, background jobs use the agent's model.
// The monthly token cap stops background jobs once the workspace's model calls this month (all of
// them, background or not) reach it; triage, chat replies and the assistant keep working.

type UsageOut struct {
	Month           string `json:"month" jsonschema:"YYYY-MM (UTC)"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	Calls           int64  `json:"calls"`
	BackgroundCalls int64  `json:"background_calls" jsonschema:"calls made for background jobs, included in calls"`
}

type SetBackgroundModelIn struct {
	BaseURL    string `json:"base_url,omitempty" jsonschema:"OpenAI-compatible API base; empty with an empty model goes back to the agent's own model"`
	Model      string `json:"model,omitempty" jsonschema:"model id as the endpoint names it"`
	Credential string `json:"credential,omitempty" jsonschema:"API key or OAuth access token; empty keeps the saved one, or uses the agent's own key when the base URL is the same"`
}

type SetTokenCapIn struct {
	MonthlyTokenCap int64 `json:"monthly_token_cap" jsonschema:"tokens per month (input + output, all model calls) after which background jobs stop; 0 removes the cap"`
}

// errBackgroundOff: the workspace has no model for background jobs. The job is skipped, never failed.
var errBackgroundOff = errors.New("background jobs are off")

// errBackgroundCapped: this month's tokens reached the cap. It's checked before every background
// model call, so a job stops mid-way too. The job waits, pending, for next month or a higher cap.
var errBackgroundCapped = errors.New("background jobs are paused: this month's tokens reached the cap")

// capPaused is the last_error of a job waiting on the cap, which raising the cap looks for.
const capPaused = "paused: monthly token cap"

// capReached reports whether this month's tokens (all calls) reached the workspace's cap.
func capReached(ctx context.Context, tx pgx.Tx) (bool, error) {
	var reached bool
	err := tx.QueryRow(ctx, `SELECT coalesce((SELECT monthly_token_cap FROM triage_settings) <=
		(SELECT coalesce(sum(input_tokens + output_tokens), 0) FROM agent_usage
			WHERE month = date_trunc('month', now() AT TIME ZONE 'UTC')::date), false)`).Scan(&reached)
	return reached, err
}

// checkCap is chatCompletion's check before a background call: errBackgroundCapped at the cap. Calls
// already in flight when it's reached still finish, so usage can pass the cap by those, not more.
func (s *Service) checkCap(ctx context.Context, org string) error {
	var reached bool
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) (err error) {
		reached, err = capReached(ctx, tx)
		return err
	})
	if err != nil {
		return err
	}
	if reached {
		return errBackgroundCapped
	}
	return nil
}

func (c *call) usageOut() (*UsageOut, error) {
	out := &UsageOut{Month: time.Now().UTC().Format("2006-01")}
	err := c.tx.QueryRow(c.ctx, `SELECT coalesce(sum(input_tokens), 0), coalesce(sum(output_tokens), 0), coalesce(sum(calls), 0),
			coalesce(sum(calls) FILTER (WHERE background), 0)
		FROM agent_usage WHERE month = date_trunc('month', now() AT TIME ZONE 'UTC')::date`).
		Scan(&out.InputTokens, &out.OutputTokens, &out.Calls, &out.BackgroundCalls)
	return out, err
}

func (s *Service) SetBackgroundModel(ctx context.Context, p *Principal, in SetBackgroundModelIn) (TriageOut, error) {
	var out TriageOut
	err := s.run(ctx, p, func(c *call) error {
		if err := p.requireAdmin(); err != nil {
			return err
		}
		var mainURL, savedURL string
		var saved []byte
		err := c.tx.QueryRow(c.ctx, `SELECT base_url, helper_base_url, helper_credential FROM triage_settings FOR UPDATE`).Scan(&mainURL, &savedURL, &saved)
		if errors.Is(err, pgx.ErrNoRows) {
			return errf(ErrInvalid, "turn triage on first: the background model stands in for its model")
		}
		if err != nil {
			return err
		}
		model := strings.TrimSpace(in.Model)
		if strings.TrimSpace(in.BaseURL) == "" && model == "" {
			if _, err := c.tx.Exec(c.ctx, `UPDATE triage_settings SET helper_base_url = '', helper_model = '', helper_credential = NULL,
				updated_by = $1, updated_at = now()`, p.Member.ID); err != nil {
				return err
			}
			out, err = c.triageOut()
			return err
		}
		base, err := triageBaseURL(in.BaseURL)
		if err != nil {
			return err
		}
		if model == "" || len(model) > 200 {
			return errf(ErrInvalid, "model is required")
		}
		var sealed []byte
		switch cred := strings.TrimSpace(in.Credential); {
		case cred != "":
			if sealed, err = s.seal([]byte(cred)); err != nil {
				return err
			}
		case saved != nil && savedURL == base:
			sealed = saved // a saved key only stays with the endpoint it was given for
		case base == mainURL:
			// No key of its own: it uses the agent's, for the agent's endpoint only.
		default:
			return errf(ErrInvalid, "enter the API key or OAuth token for %s", base)
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE triage_settings SET helper_base_url = $1, helper_model = $2, helper_credential = $3,
			updated_by = $4, updated_at = now()`, base, model, sealed, p.Member.ID); err != nil {
			return err
		}
		out, err = c.triageOut()
		return err
	})
	return out, err
}

func (s *Service) SetMonthlyTokenCap(ctx context.Context, p *Principal, in SetTokenCapIn) (TriageOut, error) {
	var out TriageOut
	err := s.run(ctx, p, func(c *call) error {
		if err := p.requireAdmin(); err != nil {
			return err
		}
		if in.MonthlyTokenCap < 0 {
			return errf(ErrInvalid, "monthly_token_cap can't be negative")
		}
		var limit *int64
		if in.MonthlyTokenCap > 0 {
			limit = &in.MonthlyTokenCap
		}
		tag, err := c.tx.Exec(c.ctx, `UPDATE triage_settings SET monthly_token_cap = $1, updated_by = $2, updated_at = now()`, limit, p.Member.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errf(ErrInvalid, "turn triage on first: the cap applies to its model's background jobs")
		}
		// Jobs paused by the old cap get another look now: under the new one they run, and if it's
		// still reached they pause again until next month.
		paused, err := c.tx.Exec(c.ctx, `UPDATE agent_jobs SET due_at = now(), updated_at = now()
			WHERE state = 'pending' AND last_error = $1`, capPaused)
		if err != nil {
			return err
		}
		if paused.RowsAffected() > 0 {
			c.after = append(c.after, c.s.wakeJobs)
		}
		out, err = c.triageOut()
		return err
	})
	return out, err
}

// backgroundModel is the model a background job uses in this workspace, with its key:
// errBackgroundOff when there is none, errBackgroundCapped when this month's tokens reached the cap.
// chatCompletion checks the cap again before each call made with it.
func (s *Service) backgroundModel(ctx context.Context, org string) (*triageSettings, string, error) {
	st := &triageSettings{OrgID: org, Background: true}
	var helperURL, helperModel string
	var helperCred []byte
	var reached bool
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT agent_id::text, base_url, model, credential, helper_base_url, helper_model, helper_credential
			FROM triage_settings`).Scan(&st.AgentID, &st.BaseURL, &st.Model, &st.Credential, &helperURL, &helperModel, &helperCred)
		if err != nil {
			return err
		}
		reached, err = capReached(ctx, tx)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", errBackgroundOff
	}
	if err != nil {
		return nil, "", err
	}
	if reached {
		return nil, "", errBackgroundCapped
	}
	switch {
	case helperModel == "":
	case helperCred != nil:
		st.BaseURL, st.Model, st.Credential = helperURL, helperModel, helperCred
	case helperURL == st.BaseURL:
		st.Model = helperModel
	default:
		// Its key was the agent's, and the agent's endpoint has changed since: that key never goes
		// elsewhere. The agent's own model does the job until an admin sets the background model again.
	}
	cred, err := s.unseal(st.Credential)
	if err != nil {
		return nil, "", errors.New("the saved model credential can't be read; an admin needs to enter it again")
	}
	return st, string(cred), nil
}
