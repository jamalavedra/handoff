package core_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joalavedra/handoff/internal/core"
)

// fakeComposio is Composio's API and one session MCP server: sessions per user, sign-in links,
// toolkits with their connections, and deleting a connection. connect marks an app connected for
// a user, as finishing Composio's sign-in page would.
type fakeComposio struct {
	srv      *httptest.Server
	mu       sync.Mutex
	sessions map[string]string          // session id -> user id
	accounts map[string]map[string]bool // user id -> connected toolkits
	bodies   []map[string]any           // session create requests
	links    []map[string]any           // link requests
}

func newFakeComposio(t *testing.T) *fakeComposio {
	t.Helper()
	f := &fakeComposio{sessions: map[string]string{}, accounts: map[string]map[string]bool{}}
	server := mcp.NewServer(&mcp.Implementation{Name: "composio", Version: "1"}, nil)
	type q struct {
		Query string `json:"query"`
	}
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(server, &mcp.Tool{Name: "GMAIL_FETCH_EMAILS", Description: "Fetch emails", Annotations: ro},
		func(_ context.Context, _ *mcp.CallToolRequest, in q) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "From: Ana — Invoice #42 is due Friday (" + in.Query + ")"}}}, nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "GMAIL_SEND_EMAIL", Description: "Send an email"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ q) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "GOOGLECALENDAR_EVENTS_LIST", Description: "List events"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ q) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Standup 9:00"}}}, nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "COMPOSIO_SEARCH_TOOLS", Description: "Search tools", Annotations: ro},
		func(_ context.Context, _ *mcp.CallToolRequest, _ q) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "ck-test" {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/mcp/") {
			mcpHandler.ServeHTTP(w, r)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tool_router/session":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.bodies = append(f.bodies, body)
			id := "trs_" + string(rune('a'+len(f.sessions)))
			f.sessions[id] = body["user_id"].(string)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"session_id": id, "mcp": map[string]any{"type": "http", "url": f.srv.URL + "/mcp/" + id}})
		case len(parts) == 4 && parts[0] == "tool_router" && parts[3] == "link":
			user, ok := f.sessions[parts[2]]
			if !ok {
				http.Error(w, `{"error":{"message":"no session"}}`, http.StatusNotFound)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			body["user"] = user
			f.links = append(f.links, body)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"link_token": "lt", "redirect_url": "https://connect.composio.dev/link/ln_1", "connected_account_id": "ca_pending"})
		case len(parts) == 4 && parts[0] == "tool_router" && parts[3] == "toolkits":
			user, ok := f.sessions[parts[2]]
			if !ok {
				http.Error(w, `{"error":{"message":"no session"}}`, http.StatusNotFound)
				return
			}
			var items []map[string]any
			for _, slug := range strings.Split(r.URL.Query().Get("toolkits"), ",") {
				var ca any
				if f.accounts[user][slug] {
					ca = map[string]any{"id": "ca_" + slug + "_" + user, "status": "ACTIVE"}
				}
				items = append(items, map[string]any{"slug": slug, "name": slug, "meta": map[string]any{"logo": "https://logos.composio.dev/" + slug, "description": slug}, "connected_account": ca})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total_items": len(items), "total_pages": 1, "current_page": 1})
		case r.Method == http.MethodDelete && len(parts) == 2 && parts[0] == "connected_accounts":
			for user, apps := range f.accounts {
				for slug := range apps {
					if parts[1] == "ca_"+slug+"_"+user {
						delete(apps, slug)
						_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
						return
					}
				}
			}
			http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
		default:
			http.Error(w, `{"error":{"message":"unexpected"}}`, http.StatusBadRequest)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeComposio) connect(user, app string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.accounts[user] == nil {
		f.accounts[user] = map[string]bool{}
	}
	f.accounts[user][app] = true
}

func appsByName(out core.AppsOut) map[string]core.AppOut {
	m := map[string]core.AppOut{}
	for _, a := range out.Apps {
		m[a.Slug] = a
	}
	return m
}

func TestApps(t *testing.T) {
	fake := newFakeComposio(t)
	model, reqs := scriptedModel(t,
		calls(toolCall("c1", "ext_apps__gmail_fetch_emails", map[string]any{"query": "invoice"})),
		say("Ana says invoice #42 is due Friday."),
		say("Nothing connected for you yet."),
	)
	e, admin, bot := assistantEnv(t, model, "apps")
	user := "handoff:" + admin.Org.ID + ":" + admin.Member.ID

	// Without a key there's nothing to connect; agents never connect apps.
	if out, err := e.s.ListApps(e.ctx, admin, struct{}{}); err != nil || out.Enabled || len(out.Apps) != 0 {
		t.Fatalf("apps without a key: %+v %v", out, err)
	}
	e.s.Cfg.ComposioAPIKey, e.s.Cfg.ComposioURL = "ck-test", fake.srv.URL
	if _, err := e.s.ListApps(e.ctx, bot, struct{}{}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("agent listed apps: %v", err)
	}

	// The first look makes the person's session: the catalog's read-only tools, no Composio helpers.
	out, err := e.s.ListApps(e.ctx, admin, struct{}{})
	if err != nil || !out.Enabled || len(out.Apps) != 7 || appsByName(out)["gmail"].Connected {
		t.Fatalf("apps: %+v %v", out, err)
	}
	body := fake.bodies[0]
	if body["user_id"] != user || body["tags"].(map[string]any)["enable"].([]any)[0] != "readOnlyHint" ||
		body["manage_connections"].(map[string]any)["enable"] != false || body["workbench"].(map[string]any)["enable"] != false {
		t.Fatalf("session: %v", body)
	}

	// Connecting returns Composio's page and comes back to Settings → Apps on this workspace only.
	if _, err := e.s.ConnectApp(e.ctx, admin, core.ConnectAppIn{App: "gmail", Origin: "https://evil.example"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("foreign origin accepted: %v", err)
	}
	if _, err := e.s.ConnectApp(e.ctx, admin, core.ConnectAppIn{App: "myspace", Origin: "http://apps.handoff.test"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("unknown app accepted: %v", err)
	}
	link, err := e.s.ConnectApp(e.ctx, admin, core.ConnectAppIn{App: "gmail", Origin: "http://apps.handoff.test"})
	if err != nil || link.RedirectURL != "https://connect.composio.dev/link/ln_1" {
		t.Fatalf("connect: %+v %v", link, err)
	}
	if l := fake.links[0]; l["toolkit"] != "gmail" || l["callback_url"] != "http://apps.handoff.test/settings/apps?app=Gmail" || l["user"] != user {
		t.Fatalf("link: %v", l)
	}

	// Signed in to Gmail and Calendar: only tools that read are kept, never Composio's own.
	fake.connect(user, "gmail")
	fake.connect(user, "googlecalendar")
	out, err = e.s.ListApps(e.ctx, admin, struct{}{})
	apps := appsByName(out)
	if err != nil || !apps["gmail"].Connected || apps["gmail"].Tools != 1 || !apps["googlecalendar"].Connected || apps["googlecalendar"].Tools != 1 || apps["slack"].Connected {
		t.Fatalf("connected apps: %+v %v", out, err)
	}

	// The person's assistant reads their email; sending isn't offered.
	chat, err := e.s.AssistantChat(e.ctx, admin, ask("anything about invoices in my email?"))
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Steps) != 1 || chat.Steps[0].Summary != "Gmail: GMAIL_FETCH_EMAILS · invoice" || chat.Steps[0].Error != "" {
		t.Fatalf("steps: %+v", chat.Steps)
	}
	var offered []string
	for _, tl := range reqs()[0].Tools {
		if strings.HasPrefix(tl.Function.Name, "ext_apps__") {
			offered = append(offered, tl.Function.Name)
		}
	}
	slices.Sort(offered)
	if !slices.Equal(offered, []string{"ext_apps__gmail_fetch_emails", "ext_apps__googlecalendar_events_list"}) {
		t.Fatalf("offered: %v", offered)
	}
	if res := toolResult(t, reqs(), "c1"); !strings.Contains(res, "Invoice #42") {
		t.Fatalf("result: %s", res)
	}
	if sys := reqs()[0].Messages[0]["content"].(string); !strings.Contains(sys, "Gmail, Google Calendar") {
		t.Fatalf("prompt doesn't say whose apps: %s", sys)
	}

	// Someone else's chat never gets them.
	if _, err := e.s.Invite(e.ctx, admin, core.InviteIn{Email: "bob@apps.test"}); err != nil {
		t.Fatal(err)
	}
	bob, err := e.s.MemberForLogin(e.ctx, admin.Org, "bob@apps.test", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	bp, _ := e.s.Principal(e.ctx, admin.Org, bob.ID, "web")
	if _, err := e.s.AssistantChat(e.ctx, bp, ask("what's in my inbox?")); err != nil {
		t.Fatal(err)
	}
	for _, tl := range reqs()[2].Tools {
		if strings.HasPrefix(tl.Function.Name, "ext_apps__") {
			t.Fatalf("bob was offered %s", tl.Function.Name)
		}
	}

	// A session Composio forgot is made again; the connections are the user's, so they remain.
	fake.mu.Lock()
	clear(fake.sessions)
	fake.mu.Unlock()
	if out, err = e.s.ListApps(e.ctx, admin, struct{}{}); err != nil || !appsByName(out)["gmail"].Connected || appsByName(out)["gmail"].Tools != 1 {
		t.Fatalf("after a lost session: %+v %v", out, err)
	}

	// Disconnecting removes the account at Composio and its tools.
	if out, err = e.s.DisconnectApp(e.ctx, admin, core.AppIn{App: "gmail"}); err != nil || appsByName(out)["gmail"].Connected || appsByName(out)["gmail"].Tools != 0 || !appsByName(out)["googlecalendar"].Connected {
		t.Fatalf("disconnect: %+v %v", out, err)
	}
	if _, err := e.s.DisconnectApp(e.ctx, admin, core.AppIn{App: "gmail"}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("disconnected twice: %v", err)
	}
}
