package server_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
	"github.com/joalavedra/handoff/internal/mail"
	"github.com/joalavedra/handoff/internal/server"
	"github.com/joalavedra/handoff/internal/testutil"
)

// hostRewriter sends every request to the test server while keeping the
// workspace Host header, so subdomain routing is exercised for real.
type hostRewriter struct{ target *url.URL }

func (h hostRewriter) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = r.URL.Host
	r.URL.Scheme, r.URL.Host = h.target.Scheme, h.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

type fixture struct {
	t      *testing.T
	svc    *core.Service
	client *http.Client
}

func setup(t *testing.T) *fixture {
	d := testutil.DB(t)
	base, _ := url.Parse("http://handoff.test")
	cfg := &config.Config{BaseURL: base, Dev: true, AllowSignup: true, Secret: []byte("test")}
	svc := core.New(d, cfg)
	srv := server.New(cfg, svc, &mail.Mailer{}, fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}, "test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	jar, _ := cookiejar.New(nil)
	return &fixture{t: t, svc: svc, client: &http.Client{Transport: hostRewriter{u}, Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (f *fixture) do(method, u string, body any, hdr map[string]string) (int, map[string]any) {
	f.t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u, rd)
	req.Header.Set("X-Handoff", "1")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &out)
	if loc := resp.Header.Get("Location"); loc != "" {
		out["_location"] = loc
	}
	if a := resp.Header.Get("WWW-Authenticate"); a != "" {
		out["_www"] = a
	}
	return resp.StatusCode, out
}

// signup creates a workspace over HTTP and signs its admin in via the emailed link.
func (f *fixture) signup(slug string) string {
	code, out := f.do("POST", "http://handoff.test/api/signup", map[string]string{"slug": slug, "name": slug, "email": "admin@" + slug + ".test"}, nil)
	if code != 200 {
		f.t.Fatalf("signup: %d %v", code, out)
	}
	link := out["dev_link"].(string)
	code, out = f.do("GET", link, nil, nil)
	if code != 303 || out["_location"] != "/" {
		f.t.Fatalf("verify: %d %v", code, out)
	}
	return "http://" + slug + ".handoff.test"
}

func TestOAuthAndMCP(t *testing.T) {
	f := setup(t)
	acme := f.signup("acme")
	f.signup("globex")

	code, out := f.do("POST", acme+"/mcp", "{}", map[string]string{"Content-Type": "application/json"})
	if code != 401 || !strings.Contains(out["_www"].(string), acme+"/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("mcp without token: %d %v", code, out)
	}
	_, meta := f.do("GET", acme+"/.well-known/oauth-authorization-server", nil, nil)
	if meta["token_endpoint"] != acme+"/oauth/token" {
		t.Fatalf("metadata: %v", meta)
	}
	code, reg := f.do("POST", acme+"/oauth/register", map[string]any{"client_name": "Claude", "redirect_uris": []string{"https://claude.ai/api/mcp/auth_callback"}}, nil)
	if code != 201 {
		t.Fatalf("register: %d %v", code, reg)
	}
	clientID := reg["client_id"].(string)
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {clientID}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "response_type": {"code"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "state": {"xyz"}}
	_, out = f.do("GET", acme+"/oauth/authorize?"+q.Encode(), nil, nil)
	reqID := strings.TrimPrefix(out["_location"].(string), "/authorize?request=")

	// A session from another workspace can't approve: cookies are per host, and the request is per org.
	code, _ = f.do("POST", "http://globex.handoff.test/api/oauth/consent", map[string]any{"request": reqID, "approve": true}, nil)
	if code != 404 {
		t.Fatalf("cross-org consent: %d", code)
	}
	code, out = f.do("POST", acme+"/api/oauth/consent", map[string]any{"request": reqID, "approve": true, "name": "Claude (Joan)"}, nil)
	if code != 200 {
		t.Fatalf("consent: %d %v", code, out)
	}
	ru, _ := url.Parse(out["redirect"].(string))
	if ru.Query().Get("state") != "xyz" {
		t.Fatalf("state lost: %v", ru)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {ru.Query().Get("code")},
		"redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "code_verifier": {verifier}}
	ct := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	code, tok := f.do("POST", acme+"/oauth/token", form.Encode(), ct)
	if code != 200 {
		t.Fatalf("token: %d %v", code, tok)
	}
	if code, _ := f.do("POST", acme+"/oauth/token", form.Encode(), ct); code != 400 {
		t.Fatal("code reused")
	}
	access := tok["access_token"].(string)

	// The token is useless on another workspace.
	if code, _ := f.do("POST", "http://globex.handoff.test/mcp", "{}", map[string]string{"Authorization": "Bearer " + access}); code != 401 {
		t.Fatalf("cross-org token: %d", code)
	}

	// Real MCP client session through the streamable HTTP endpoint.
	httpc := &http.Client{Transport: authTransport{tok: access, next: f.client.Transport}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: acme + "/mcp", HTTPClient: httpc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tl, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Tools) != 30 {
		t.Fatalf("tools: %d", len(tl.Tools))
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_tasks",
		Arguments: map[string]any{"tasks": []map[string]any{{"title": "From Claude", "external_id": "c1"}}}})
	if err != nil || res.IsError {
		t.Fatalf("create: %v %+v", err, res)
	}
	sc, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(sc), `"name":"Claude (Joan)"`) {
		t.Fatalf("attribution: %s", sc)
	}
	res, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "claim_task", Arguments: map[string]any{"id": "nope"}})
	if !res.IsError {
		t.Fatal("expected tool error")
	}

	// The person sees it in the web API.
	code, out = f.do("POST", acme+"/api/tools/search_tasks", map[string]any{}, nil)
	if code != 200 || len(out["tasks"].([]any)) != 1 {
		t.Fatalf("web search: %d %v", code, out)
	}
	// CSRF guard.
	req, _ := http.NewRequest("POST", acme+"/api/tools/search_tasks", strings.NewReader("{}"))
	if resp, _ := f.client.Do(req); resp.StatusCode != 403 {
		t.Fatalf("missing X-Handoff accepted: %d", resp.StatusCode)
	}
	// Refresh rotates.
	rf := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {tok["refresh_token"].(string)}}
	if code, _ := f.do("POST", acme+"/oauth/token", rf.Encode(), ct); code != 200 {
		t.Fatalf("refresh: %d", code)
	}
	if code, _ := f.do("POST", acme+"/oauth/token", rf.Encode(), ct); code != 400 {
		t.Fatal("refresh token reused")
	}
}

func TestAPIKeyAndBeads(t *testing.T) {
	f := setup(t)
	acme := f.signup("acme")
	_, agent := f.do("POST", acme+"/api/tools/create_agent", map[string]any{"name": "CI bot"}, nil)
	_, key := f.do("POST", acme+"/api/agents/"+agent["id"].(string)+"/keys", nil, nil)
	h := map[string]string{"Authorization": "Bearer " + key["key"].(string)}
	jsonl := `{"id":"bd-1","title":"Write spec","status":"closed","priority":1}
{"id":"bd-2","title":"Build it","status":"open","priority":2,"dependencies":[{"issue_id":"bd-2","depends_on_id":"bd-1","type":"blocks"}],"comments":[{"author":"marta","text":"start with auth"}]}
`
	code, out := f.do("POST", acme+"/api/beads/import", jsonl, h)
	if code != 200 || out["created"].(float64) != 2 || out["closed"].(float64) != 1 || out["linked"].(float64) != 1 {
		t.Fatalf("import: %d %v", code, out)
	}
	code, out = f.do("POST", acme+"/api/beads/import", jsonl, h)
	if code != 200 || out["existed"].(float64) != 2 {
		t.Fatalf("re-import: %d %v", code, out)
	}
	_, ready := f.do("POST", acme+"/api/tools/list_ready", map[string]any{}, h)
	if ts := ready["tasks"].([]any); len(ts) != 1 || ts[0].(map[string]any)["title"] != "Build it" {
		t.Fatalf("ready: %v", ready)
	}
	req, _ := http.NewRequest("GET", acme+"/api/beads/export", nil)
	req.Header.Set("Authorization", h["Authorization"])
	resp, _ := f.client.Do(req)
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"depends_on_id":"bd-1"`) || !strings.Contains(string(b), `"status":"closed"`) {
		t.Fatalf("export: %s", b)
	}
	// Agents can't call web-only tools with their key.
	if code, _ := f.do("POST", acme+"/api/tools/invite", map[string]any{"email": "x@y.z"}, h); code != 403 {
		t.Fatalf("agent invited: %d", code)
	}
	// Unknown workspace host.
	if code, _ := f.do("GET", "http://nope.handoff.test/", nil, nil); code != 404 {
		t.Fatalf("unknown host: %d", code)
	}
}

type authTransport struct {
	tok  string
	next http.RoundTripper
}

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.tok)
	return a.next.RoundTrip(r)
}

func TestLLMsDocs(t *testing.T) {
	f := setup(t)
	acme := f.signup("acme")
	get := func(u string) string {
		t.Helper()
		resp, err := f.client.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/markdown") {
			t.Fatalf("%s: %d %s", u, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		return string(b)
	}
	idx := get(acme + "/llms.txt")
	if !strings.Contains(idx, "http://acme.handoff.test/mcp") || !strings.Contains(idx, "/llms-full.txt") {
		t.Fatalf("index: %s", idx)
	}
	full := get(acme + "/llms-full.txt")
	for _, want := range []string{"### claim_task", "`lease_minutes`", "  - `title` (string, required)", "### inbox", "Call what_changed", "claude mcp add --transport http handoff http://acme.handoff.test/mcp"} {
		if !strings.Contains(full, want) {
			t.Fatalf("full docs missing %q", want)
		}
	}
	if strings.Contains(full, "### invite") {
		t.Fatal("web-only tool listed")
	}
	if apex := get("http://handoff.test/llms.txt"); !strings.Contains(apex, "http://<workspace>.handoff.test/mcp") {
		t.Fatalf("apex: %s", apex)
	}
}
