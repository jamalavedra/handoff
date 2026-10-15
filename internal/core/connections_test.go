package core_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
)

// fakeSearchServer is a remote MCP server with a read-only search tool, an unmarked tool and a
// destructive one, behind a bearer token.
func fakeSearchServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "fake-search", Version: "1"}, nil)
	type q struct {
		Query string `json:"query"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "web_search", Description: "Search the web", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(_ context.Context, _ *mcp.CallToolRequest, in q) (*mcp.CallToolResult, any, error) {
			text := "Pixel 9: 12GB RAM (store.google.com) for " + in.Query
			if host, ok := strings.CutPrefix(in.Query, "site "); ok {
				text = "Top result: http://" + host
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "plain", Description: "No hints"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ q) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
	yes := true
	mcp.AddTool(server, &mcp.Tool{Name: "delete_page", Description: "Deletes a page", Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes}},
		func(_ context.Context, _ *mcp.CallToolRequest, _ q) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-mcp" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConnections(t *testing.T) {
	search := fakeSearchServer(t)
	model, reqs := scriptedModel(t,
		calls(toolCall("c1", "ext_web_search__web_search", map[string]any{"query": "pixel 9 ram"})),
		say("The Pixel 9 has 12GB of RAM."),
	)
	e, admin, bot := assistantEnv(t, model, "conn")

	// Admins only; a wrong key fails up front.
	if _, err := e.s.AddConnection(e.ctx, bot, core.AddConnectionIn{Name: "Web search", URL: search.URL, Auth: "bearer", Secret: "sk-mcp"}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("agent added a connection: %v", err)
	}
	if _, err := e.s.AddConnection(e.ctx, admin, core.AddConnectionIn{Name: "Web search", URL: search.URL, Auth: "bearer", Secret: "wrong"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("wrong key accepted: %v", err)
	}
	c, err := e.s.AddConnection(e.ctx, admin, core.AddConnectionIn{Name: "Web search", URL: search.URL, Auth: "bearer", Secret: "sk-mcp"})
	if err != nil {
		t.Fatal(err)
	}
	enabled := func(c core.ConnectionOut) []string {
		var on []string
		for _, tl := range c.Tools {
			if tl.Enabled {
				on = append(on, tl.Name)
			}
		}
		slices.Sort(on)
		return on
	}
	if len(c.Tools) != 3 || !slices.Equal(enabled(c), []string{"web_search"}) || !c.HasSecret {
		t.Fatalf("connection: %+v", c)
	}
	// Destructive tools stay off; unmarked ones can be switched on deliberately.
	if _, err := e.s.SetConnectionTools(e.ctx, admin, core.SetConnectionToolsIn{ID: c.ID, Enabled: []string{"web_search", "delete_page"}}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("destructive tool switched on: %v", err)
	}
	if c, err = e.s.SetConnectionTools(e.ctx, admin, core.SetConnectionToolsIn{ID: c.ID, Enabled: []string{"web_search", "plain"}}); err != nil || !slices.Equal(enabled(c), []string{"plain", "web_search"}) {
		t.Fatalf("set tools: %+v %v", c, err)
	}

	// The assistant uses the search tool; the step shows it and the result reaches the model.
	out, err := e.s.AssistantChat(e.ctx, admin, ask("how much ram does the pixel 9 have?"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Steps) != 1 || out.Steps[0].Summary != "Web search: web_search · pixel 9 ram" || out.Steps[0].Error != "" {
		t.Fatalf("steps: %+v", out.Steps)
	}
	rs := reqs()
	offered := false
	for _, tl := range rs[0].Tools {
		if tl.Function.Name == "ext_web_search__web_search" {
			offered = true
		}
	}
	last := rs[len(rs)-1].Messages
	if !offered || !strings.Contains(last[len(last)-1]["content"].(string), "12GB RAM") {
		t.Fatalf("tool not offered or result not passed on (offered %v)", offered)
	}

	// Removing it takes the tools away.
	if _, err := e.s.RemoveConnection(e.ctx, admin, core.ConnectionIDIn{ID: c.ID}); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.s.ListConnections(e.ctx, admin, struct{}{}); len(list.Connections) != 0 {
		t.Fatalf("still listed: %+v", list)
	}
}

// fakeOAuthServer is an MCP server that signs in like hosted ones do: protected-resource metadata,
// an authorization server with dynamic registration, PKCE and refresh tokens.
func fakeOAuthServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "fake-internal", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "lookup", Description: "Look up a customer", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	var mu sync.Mutex
	var grants []string
	flaky := true                    // the first code exchange gets a 503, as a busy service would
	challenge := map[string]string{} // client_id -> code challenge, set when the test "approves"
	mux := http.NewServeMux()
	var base string
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base + "/auth"}, "scopes_supported": []string{"mcp:tools"}})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/auth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"authorization_endpoint": base + "/auth/authorize", "token_endpoint": base + "/auth/token", "registration_endpoint": base + "/auth/register"})
	})
	mux.HandleFunc("POST /auth/register", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			RedirectURIs []string `json:"redirect_uris"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if len(in.RedirectURIs) != 1 || !strings.HasSuffix(in.RedirectURIs[0], "/oauth/mcp/callback") {
			http.Error(w, "bad redirect", 400)
			return
		}
		w.WriteHeader(201)
		writeJSON(w, map[string]any{"client_id": "client-1"})
	})
	mux.HandleFunc("POST /approve", func(w http.ResponseWriter, r *http.Request) { // test-only: what the consent screen would record
		mu.Lock()
		challenge[r.FormValue("client_id")] = r.FormValue("code_challenge")
		mu.Unlock()
	})
	mux.HandleFunc("POST /auth/token", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		grants = append(grants, r.FormValue("grant_type"))
		switch r.FormValue("grant_type") {
		case "authorization_code":
			if flaky {
				flaky = false
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
			if r.FormValue("code") != "code-1" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge[r.FormValue("client_id")] {
				w.WriteHeader(400)
				writeJSON(w, map[string]any{"error": "invalid_grant"})
				return
			}
			writeJSON(w, map[string]any{"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 5})
		case "refresh_token":
			if r.FormValue("refresh_token") != "rt-1" {
				w.WriteHeader(400)
				writeJSON(w, map[string]any{"error": "invalid_grant"})
				return
			}
			writeJSON(w, map[string]any{"access_token": "at-2", "expires_in": 3600})
		}
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if a := r.Header.Get("Authorization"); a != "Bearer at-1" && a != "Bearer at-2" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+base+`/.well-known/oauth-protected-resource/mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(mux)
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(grants) }
}

func TestConnectionOAuth(t *testing.T) {
	ext, grants := fakeOAuthServer(t)
	model, _ := scriptedModel(t)
	e, admin, bot := assistantEnv(t, model, "connoauth")
	origin := e.s.Cfg.OrgURL(admin.Org.Slug)
	in := core.StartConnectionOAuthIn{Name: "Internal", URL: ext.URL + "/mcp", Origin: origin}

	if _, err := e.s.StartConnectionOAuth(e.ctx, bot, in); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("agent started a sign-in: %v", err)
	}
	evil := in
	evil.Origin = "https://evil.example"
	if _, err := e.s.StartConnectionOAuth(e.ctx, admin, evil); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("foreign origin accepted: %v", err)
	}
	start, err := e.s.StartConnectionOAuth(e.ctx, admin, in)
	if err != nil {
		t.Fatal(err)
	}
	au, _ := url.Parse(start.AuthorizeURL)
	q := au.Query()
	if au.Path != "/auth/authorize" || q.Get("client_id") != "client-1" || q.Get("code_challenge_method") != "S256" ||
		q.Get("redirect_uri") != origin+"/oauth/mcp/callback" || q.Get("resource") != ext.URL+"/mcp" || q.Get("scope") != "mcp:tools" {
		t.Fatalf("authorize url: %s", start.AuthorizeURL)
	}
	if _, err := http.PostForm(ext.URL+"/approve", url.Values{"client_id": {"client-1"}, "code_challenge": {q.Get("code_challenge")}}); err != nil {
		t.Fatal(err)
	}

	// A wrong state is refused; the right one adds the connection with its read-only tools on.
	if _, err := e.s.FinishConnectionOAuth(e.ctx, admin, "nope", "code-1"); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("unknown state accepted: %v", err)
	}
	// A service hiccup leaves the sign-in in place, so the callback can simply be retried.
	if _, err := e.s.FinishConnectionOAuth(e.ctx, admin, q.Get("state"), "code-1"); !core.IsCode(err, core.ErrInvalid) || !strings.Contains(err.Error(), "try again") {
		t.Fatalf("503 not reported as retryable: %v", err)
	}
	c, err := e.s.FinishConnectionOAuth(e.ctx, admin, q.Get("state"), "code-1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth != "oauth" || !c.HasSecret || len(c.Tools) != 1 || !c.Tools[0].Enabled {
		t.Fatalf("connection: %+v", c)
	}
	if _, err := e.s.FinishConnectionOAuth(e.ctx, admin, q.Get("state"), "code-1"); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("sign-in reused: %v", err)
	}

	// The token is about to expire, so the next use renews it and keeps the refresh token.
	for range 2 {
		if _, err := e.s.RefreshConnection(e.ctx, admin, core.ConnectionIDIn{ID: c.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if g := grants(); !slices.Equal(g, []string{"authorization_code", "authorization_code", "refresh_token"}) {
		t.Fatalf("grants: %v", g)
	}
}

func TestLoopbackCallback(t *testing.T) {
	base, _ := url.Parse("http://handoff.localhost:8080")
	s := core.New(nil, &config.Config{BaseURL: base})
	to, ok := s.LoopbackCallback("localhost:5287", url.Values{"state": {"barcino~abc"}, "code": {"c1"}})
	if !ok || to != "http://barcino.handoff.localhost:5287/oauth/mcp/callback?code=c1&state=barcino~abc" {
		t.Fatalf("bounce: %q %v", to, ok)
	}
	if _, ok := s.LoopbackCallback("localhost:5287", url.Values{"state": {"evil.com/x~abc"}}); ok {
		t.Fatal("bad slug forwarded")
	}
	prod, _ := url.Parse("https://handoff.example")
	if _, ok := core.New(nil, &config.Config{BaseURL: prod}).LoopbackCallback("localhost", url.Values{"state": {"acme~abc"}}); ok {
		t.Fatal("bounce outside local development")
	}
}

func TestChatReplyLooksUpAndReacts(t *testing.T) {
	search := fakeSearchServer(t)
	model, _ := scriptedModel(t,
		calls(toolCall("c1", "ext_web_search__web_search", map[string]any{"query": "pixel 9 ram"})),
		say("12GB, per the store page."),
	)
	e, admin, bot := assistantEnv(t, model, "lookreact")
	task := e.create(t, admin, core.NewTask{Title: "phone specs"})[0]
	waitTriage(t, e, admin, task.ID)
	// Connected after triage, so only the chat reply uses it.
	if _, err := e.s.AddConnection(e.ctx, admin, core.AddConnectionIn{Name: "Web search", URL: search.URL, Auth: "bearer", Secret: "sk-mcp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot how much ram?"}); err != nil {
		t.Fatal(err)
	}
	// The reply carries its lookup, and the question it answered ends with the agent's ✅, the 👀 gone
	// (swapped just after the reply is posted).
	var reply, asked core.CommentOut
	var emojis []string
	for range 200 {
		d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		if n := len(d.Comments); n >= 2 && d.Comments[n-1].Author.ID == bot.Member.ID {
			reply, asked, emojis = d.Comments[n-1], d.Comments[n-2], nil
			for _, r := range asked.Reactions {
				if slices.Contains(r.By, "Acme Bot") {
					emojis = append(emojis, r.Emoji)
				}
			}
			if slices.Equal(emojis, []string{"✅"}) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(reply.Lookups) != 1 || reply.Lookups[0] != (core.Lookup{Source: "Web search", Tool: "web_search", Query: "pixel 9 ram"}) {
		t.Fatalf("lookups on the reply: %+v", reply)
	}
	if !slices.Equal(emojis, []string{"✅"}) {
		t.Fatalf("agent reactions on the question: %+v", asked.Reactions)
	}
}

func TestChatReplyFailureClearsLooking(t *testing.T) {
	model, _ := scriptedModel(t, map[string]any{"_status": 500})
	e, admin, bot := assistantEnv(t, model, "lookfail")
	task := e.create(t, admin, core.NewTask{Title: "anything"})[0]
	waitTriage(t, e, admin, task.ID)
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot help?"}); err != nil {
		t.Fatal(err)
	}
	var d core.TaskDetailOut
	for range 300 {
		d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		if n := len(d.Comments); n > 0 && d.Comments[n-1].Author.ID == bot.Member.ID {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var left []core.ReactionOut
	for range 100 { // the 👀 is cleared just after the reply is posted
		d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		left = nil
		for _, c := range d.Comments {
			if strings.HasPrefix(c.Body, "@Acme Bot") {
				left = c.Reactions
			}
		}
		if len(left) == 0 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("a failed answer left reactions: %+v", left)
}
