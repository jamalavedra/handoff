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
	"time"

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
	mcp.AddTool(server, &mcp.Tool{Name: "GMAIL_GET_PROFILE", Description: "Read-only, but not one the catalog chose", Annotations: ro},
		func(_ context.Context, _ *mcp.CallToolRequest, _ q) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
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
		body["manage_connections"].(map[string]any)["enable"] != false || body["workbench"].(map[string]any)["enable"] != false ||
		body["tools"].(map[string]any)["gmail"].(map[string]any)["enable"].([]any)[0] != "GMAIL_FETCH_EMAILS" {
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
	if err != nil || !apps["gmail"].Connected || len(apps["gmail"].Tools) != 1 || !apps["googlecalendar"].Connected || len(apps["googlecalendar"].Tools) != 1 || apps["slack"].Connected {
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
	if out, err = e.s.ListApps(e.ctx, admin, struct{}{}); err != nil || !appsByName(out)["gmail"].Connected || len(appsByName(out)["gmail"].Tools) != 1 {
		t.Fatalf("after a lost session: %+v %v", out, err)
	}

	// Disconnecting removes the account at Composio and its tools.
	if out, err = e.s.DisconnectApp(e.ctx, admin, core.AppIn{App: "gmail"}); err != nil || appsByName(out)["gmail"].Connected || len(appsByName(out)["gmail"].Tools) != 0 || !appsByName(out)["googlecalendar"].Connected {
		t.Fatalf("disconnect: %+v %v", out, err)
	}
	if _, err := e.s.DisconnectApp(e.ctx, admin, core.AppIn{App: "gmail"}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("disconnected twice: %v", err)
	}
}

// The case from the field: "Check my invoices in inbox" as a task, then "@agent my gmail inbox" in its
// chat. Triage and the reply both read the asker's Gmail; nobody else's turns get it.
func TestAppsOnTasks(t *testing.T) {
	fake := newFakeComposio(t)
	var mu sync.Mutex
	var got []chatReq
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		sys, _ := req.Messages[0]["content"].(string)
		last := req.Messages[len(req.Messages)-1]
		offers := slices.ContainsFunc(req.Tools, func(tl struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}) bool {
			return tl.Function.Name == "ext_apps__gmail_fetch_emails"
		})
		msg := map[string]any{"content": `{"title":"Check invoices in my inbox"}`}
		switch {
		case last["role"] == "tool" && strings.HasPrefix(sys, "You triage"):
			msg = map[string]any{"content": "Found it."}
		case last["role"] == "tool":
			msg = map[string]any{"content": "Ana's invoice #42 is due Friday."}
		case offers:
			msg = map[string]any{"content": nil, "tool_calls": []map[string]any{toolCall("g1", "ext_apps__gmail_fetch_emails", map[string]any{"query": "invoice"})}}
		case !strings.HasPrefix(sys, "You triage"):
			msg = map[string]any{"content": "I can't see your inbox."}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": msg}}})
	}))
	t.Cleanup(model.Close)
	e, admin, bot := assistantEnv(t, model, "apptask")
	e.s.Cfg.ComposioAPIKey, e.s.Cfg.ComposioURL = "ck-test", fake.srv.URL
	fake.connect("handoff:"+admin.Org.ID+":"+admin.Member.ID, "gmail")
	if _, err := e.s.ListApps(e.ctx, admin, struct{}{}); err != nil {
		t.Fatal(err)
	}
	offered := func(from int) (triage, other []bool) {
		mu.Lock()
		defer mu.Unlock()
		for _, r := range got[from:] {
			sys, _ := r.Messages[0]["content"].(string)
			has := false
			for _, tl := range r.Tools {
				has = has || strings.HasPrefix(tl.Function.Name, "ext_apps__")
			}
			if strings.HasPrefix(sys, "You triage") {
				triage = append(triage, has)
			} else {
				other = append(other, has)
			}
		}
		return
	}

	// Triage of the task its author wrote looks in their inbox.
	task := e.create(t, admin, core.NewTask{Title: "Check my invoices in inbox"})[0]
	waitTriage(t, e, admin, task.ID)
	if tr, _ := offered(0); len(tr) == 0 || !tr[0] {
		t.Fatalf("triage wasn't offered the author's gmail: %v", tr)
	}

	// Asked in the task's chat, the agent reads it and answers there.
	n := len(got)
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot my gmail inbox I have connected"}); err != nil {
		t.Fatal(err)
	}
	var reply core.CommentOut
	for range 200 {
		d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		if c := d.Comments; len(c) > 0 && c[len(c)-1].Author.ID == bot.Member.ID && strings.Contains(c[len(c)-1].Body, "invoice") {
			reply = c[len(c)-1]
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(reply.Body, "#42") || len(reply.Lookups) != 1 || reply.Lookups[0].Source != "Gmail" {
		t.Fatalf("reply: %+v", reply)
	}
	if sys := got[n].Messages[0]["content"].(string); !strings.Contains(sys, "everyone who can see it reads it") {
		t.Fatalf("shared prompt doesn't warn about the audience: %s", sys)
	}

	// Bob, without apps, asks on the same task: his turn gets no one's gmail.
	if _, err := e.s.Invite(e.ctx, admin, core.InviteIn{Email: "bob@apptask.test"}); err != nil {
		t.Fatal(err)
	}
	bob, err := e.s.MemberForLogin(e.ctx, admin.Org, "bob@apptask.test", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	bp, _ := e.s.Principal(e.ctx, admin.Org, bob.ID, "web")
	n = len(got)
	if _, err := e.s.Comment(e.ctx, bp, core.CommentIn{ID: task.ID, Body: "@Acme Bot and my inbox?"}); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		if c := d.Comments; c[len(c)-1].Author.ID == bot.Member.ID && strings.Contains(c[len(c)-1].Body, "can't see") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, other := offered(n)
	if len(other) == 0 || slices.Contains(other, true) {
		t.Fatalf("bob's turn was offered app tools: %v", other)
	}
}
