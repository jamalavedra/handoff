package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/testutil"
)

// modelServer answers every chat completion with content and reports usage; it keeps each request's model and key.
type modelServer struct {
	*httptest.Server
	mu       sync.Mutex
	content  string
	models   []string
	keys     []string
	in, outT int64
}

func newModelServer(t *testing.T, content string) *modelServer {
	t.Helper()
	m := &modelServer{content: content, in: 100, outT: 20}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		m.mu.Lock()
		m.models = append(m.models, body.Model)
		m.keys = append(m.keys, r.Header.Get("Authorization"))
		content := m.content
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
			"usage":   map[string]int64{"prompt_tokens": m.in, "completion_tokens": m.outT},
		})
	}))
	t.Cleanup(m.Close)
	return m
}

func (m *modelServer) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.models)
}

func (m *modelServer) last() (model, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.models) == 0 {
		return "", ""
	}
	return m.models[len(m.models)-1], m.keys[len(m.keys)-1]
}

// bgEnv is a workspace with triage on, pointed at srv.
func bgEnv(t *testing.T, srv *httptest.Server) (*Service, *Principal) {
	t.Helper()
	base, _ := url.Parse("http://handoff.test")
	s := New(testutil.DB(t), &config.Config{BaseURL: base, Secret: []byte("test secret")})
	s.OutboundHTTP = srv.Client()
	ctx := context.Background()
	org, admin, err := s.CreateOrg(ctx, NewOrg{Slug: "bg", Name: "bg", AdminEmail: "admin@bg.test", AdminName: "Admin"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Principal(ctx, org, admin.ID, "web")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAgent(ctx, p, CreateAgentIn{Name: "Bot"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTriage(ctx, p, SetTriageIn{Agent: "Bot", BaseURL: srv.URL + "/v1", Model: "main-model", Credential: "sk-main"}); err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestCompleteJSON(t *testing.T) {
	type answer struct {
		Add []string `json:"add"`
	}
	srv := newModelServer(t, "Sure:\n{\"add\": [\"a\", \"b\"]} and then {\"add\": [\"c\"]}")
	s, p := bgEnv(t, srv.Server)
	st := &triageSettings{BaseURL: srv.URL + "/v1", Model: "m", OrgID: p.Org.ID}
	got, err := completeJSON[answer](t.Context(), s, st, "k", nil)
	if err != nil || strings.Join(got.Add, ",") != "a,b" {
		t.Fatalf("first object only: %+v %v", got, err)
	}
	srv.mu.Lock()
	srv.content = "no json here"
	srv.mu.Unlock()
	if _, err := completeJSON[answer](t.Context(), s, st, "k", nil); err == nil || !strings.Contains(err.Error(), "did not answer with JSON") {
		t.Fatalf("prose: %v", err)
	}

	// Both calls were counted for the workspace, as foreground calls.
	out, err := s.GetTriage(t.Context(), p, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if u := out.Usage; u == nil || u.Calls != 2 || u.InputTokens != 200 || u.OutputTokens != 40 || u.BackgroundCalls != 0 {
		t.Fatalf("usage: %+v", out.Usage)
	}
}

func TestBackgroundModel(t *testing.T) {
	srv := newModelServer(t, `{}`)
	s, p := bgEnv(t, srv.Server)
	ctx := t.Context()
	modelOf := func() (string, string) {
		t.Helper()
		st, cred, err := s.backgroundModel(ctx, p.Org.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Background {
			t.Fatal("not marked background")
		}
		return st.Model, cred
	}

	// None set: the agent's own model and key.
	if m, k := modelOf(); m != "main-model" || k != "sk-main" {
		t.Fatalf("default: %s %s", m, k)
	}
	// Another model on the same endpoint borrows the agent's key.
	if _, err := s.SetBackgroundModel(ctx, p, SetBackgroundModelIn{BaseURL: srv.URL + "/v1/", Model: "cheap"}); err != nil {
		t.Fatal(err)
	}
	if m, k := modelOf(); m != "cheap" || k != "sk-main" {
		t.Fatalf("same endpoint: %s %s", m, k)
	}
	// Another endpoint needs its own key, and the agent's never goes there.
	if _, err := s.SetBackgroundModel(ctx, p, SetBackgroundModelIn{BaseURL: "https://other.example/v1", Model: "x"}); !IsCode(err, ErrInvalid) {
		t.Fatalf("other endpoint without key: %v", err)
	}
	out, err := s.SetBackgroundModel(ctx, p, SetBackgroundModelIn{BaseURL: "https://other.example/v1", Model: "x", Credential: "sk-other"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.BackgroundHasCredential || out.BackgroundModel != "x" {
		t.Fatalf("settings: %+v", out)
	}
	if b, _ := json.Marshal(out); strings.Contains(string(b), "sk-other") {
		t.Fatal("background key leaked")
	}
	if m, k := modelOf(); m != "x" || k != "sk-other" {
		t.Fatalf("own key: %s %s", m, k)
	}
	// Saving again without a key keeps it for that endpoint.
	if _, err := s.SetBackgroundModel(ctx, p, SetBackgroundModelIn{BaseURL: "https://other.example/v1", Model: "y"}); err != nil {
		t.Fatal(err)
	}
	if m, k := modelOf(); m != "y" || k != "sk-other" {
		t.Fatalf("kept key: %s %s", m, k)
	}
	// Back to the same endpoint without a key: borrows the agent's again, own key dropped.
	if _, err := s.SetBackgroundModel(ctx, p, SetBackgroundModelIn{BaseURL: srv.URL + "/v1", Model: "cheap"}); err != nil {
		t.Fatal(err)
	}
	// The agent's endpoint moves: a borrowed key doesn't follow the background model elsewhere.
	if _, err := s.SetTriage(ctx, p, SetTriageIn{Agent: "Bot", BaseURL: "https://new.example/v1", Model: "main-model", Credential: "sk-new"}); err != nil {
		t.Fatal(err)
	}
	if m, k := modelOf(); m != "main-model" || k != "sk-new" {
		t.Fatalf("after the agent moved: %s %s", m, k)
	}
	// Cleared: the agent's model.
	if _, err := s.SetBackgroundModel(ctx, p, SetBackgroundModelIn{}); err != nil {
		t.Fatal(err)
	}
	if m, _ := modelOf(); m != "main-model" {
		t.Fatalf("cleared: %s", m)
	}

	// Admins only.
	agent, err := s.CreateAgent(ctx, p, CreateAgentIn{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	ap, err := s.Principal(ctx, p.Org, agent.ID, "mcp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetBackgroundModel(ctx, ap, SetBackgroundModelIn{}); !IsCode(err, ErrForbidden) {
		t.Fatalf("non-admin: %v", err)
	}
	if _, err := s.SetMonthlyTokenCap(ctx, ap, SetTokenCapIn{MonthlyTokenCap: 1}); !IsCode(err, ErrForbidden) {
		t.Fatalf("non-admin cap: %v", err)
	}
}

func TestTokenCap(t *testing.T) {
	srv := newModelServer(t, `{}`)
	s, p := bgEnv(t, srv.Server)
	ctx := t.Context()
	if _, err := s.SetMonthlyTokenCap(ctx, p, SetTokenCapIn{MonthlyTokenCap: 300}); err != nil {
		t.Fatal(err)
	}
	st, cred, err := s.backgroundModel(ctx, p.Org.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 120 tokens per call: two calls stay under 300, the third reaches it.
	for range 3 {
		if _, err := s.chatCompletion(ctx, st, cred, map[string]any{}); err != nil {
			t.Fatal(err)
		}
	}
	// The same job's next call is refused: the cap is checked before every call, not only the first.
	if _, err := s.chatCompletion(ctx, st, cred, map[string]any{}); !errors.Is(err, errBackgroundCapped) {
		t.Fatalf("next call over the cap: %v", err)
	}
	if srv.calls() != 3 {
		t.Fatalf("the model was called %d times", srv.calls())
	}
	if _, _, err := s.backgroundModel(ctx, p.Org.ID); !errors.Is(err, errBackgroundCapped) {
		t.Fatalf("over the cap: %v", err)
	}
	// Triage, chat and the assistant aren't background work: the cap doesn't stop them.
	if _, err := s.chatCompletion(ctx, &triageSettings{OrgID: p.Org.ID, BaseURL: st.BaseURL, Model: st.Model}, cred, map[string]any{}); err != nil {
		t.Fatalf("foreground call over the cap: %v", err)
	}
	out, err := s.GetTriage(ctx, p, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if out.MonthlyTokenCap != 300 || out.Usage.BackgroundCalls != 3 || out.Usage.Calls != 4 {
		t.Fatalf("settings: %+v %+v", out, out.Usage)
	}
	// Raising it lets them run again; 0 removes it.
	if _, err := s.SetMonthlyTokenCap(ctx, p, SetTokenCapIn{MonthlyTokenCap: 0}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.backgroundModel(ctx, p.Org.ID); err != nil {
		t.Fatalf("cap removed: %v", err)
	}
	if _, err := s.SetMonthlyTokenCap(ctx, p, SetTokenCapIn{MonthlyTokenCap: -1}); !IsCode(err, ErrInvalid) {
		t.Fatalf("negative: %v", err)
	}
	// Without triage there's no background model at all.
	if _, err := s.DisableTriage(ctx, p, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.backgroundModel(ctx, p.Org.ID); !errors.Is(err, errBackgroundOff) {
		t.Fatalf("triage off: %v", err)
	}
}
