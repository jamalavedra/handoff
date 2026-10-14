package server

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/joalavedra/handoff/internal/auth"
	"github.com/joalavedra/handoff/internal/tools"
)

// OAuth 2.1 authorization server for MCP clients (Claude, ChatGPT, Cursor...).
// Discovery: RFC 9728 protected-resource metadata -> RFC 8414 server metadata ->
// RFC 7591 dynamic registration -> authorization code + PKCE (S256 only) ->
// rotating refresh tokens. Consent creates (or reuses) an agent member owned by
// the person approving, so every client acts under its own name and permissions.

func (s *Server) issuer(r *http.Request) string { return s.Cfg.URLForHost(r.Host) }

func (s *Server) protectedResource(w http.ResponseWriter, r *http.Request) {
	iss := s.issuer(r)
	writeJSON(w, 200, map[string]any{
		"resource":                 iss + "/mcp",
		"authorization_servers":    []string{iss},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Handoff — " + orgFrom(r).Name,
	})
}

func (s *Server) authServerMetadata(w http.ResponseWriter, r *http.Request) {
	iss := s.issuer(r)
	writeJSON(w, 200, map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/oauth/authorize",
		"token_endpoint":                        iss + "/oauth/token",
		"registration_endpoint":                 iss + "/oauth/register",
		"revocation_endpoint":                   iss + "/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		"scopes_supported":                      []string{"tasks"},
	})
}

func oauthErr(w http.ResponseWriter, code int, kind, desc string) {
	writeJSON(w, code, map[string]string{"error": kind, "error_description": desc})
}

func validRedirect(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Fragment != "" || p.Scheme == "" {
		return false
	}
	switch p.Scheme {
	case "https":
		return p.Host != ""
	case "http":
		h := p.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	case "javascript", "data", "file", "vbscript":
		return false
	}
	return true // custom schemes for native apps, e.g. cursor://
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if err := readJSON(r, &in); err != nil {
		oauthErr(w, 400, "invalid_client_metadata", "body must be JSON")
		return
	}
	if !s.limiter.allow("register:" + clientIP(r)) {
		oauthErr(w, 429, "invalid_request", "too many registrations")
		return
	}
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 10 {
		oauthErr(w, 400, "invalid_redirect_uri", "1-10 redirect_uris required")
		return
	}
	for _, u := range in.RedirectURIs {
		if !validRedirect(u) {
			oauthErr(w, 400, "invalid_redirect_uri", "unsupported redirect_uri "+u)
			return
		}
	}
	method := in.AuthMethod
	if method == "" {
		method = "none"
	}
	if !slices.Contains([]string{"none", "client_secret_post", "client_secret_basic"}, method) {
		oauthErr(w, 400, "invalid_client_metadata", "unsupported token_endpoint_auth_method")
		return
	}
	name := strings.TrimSpace(in.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	c, secret, err := s.Auth.RegisterClient(r.Context(), orgFrom(r).ID, name, in.RedirectURIs, method)
	if err != nil {
		oauthErr(w, 500, "server_error", "registration failed")
		return
	}
	resp := map[string]any{"client_id": c.ID, "client_name": c.Name, "redirect_uris": c.RedirectURIs,
		"token_endpoint_auth_method": method, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}}
	if secret != "" {
		resp["client_secret"] = secret
	}
	writeJSON(w, 201, resp)
}

// authorize validates the request, then hands off to the web app's consent screen.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	org := orgFrom(r)
	c, err := s.Auth.Client(r.Context(), org.ID, q.Get("client_id"))
	if err != nil {
		http.Error(w, "Unknown OAuth client. Remove the connector and add it again.", 400)
		return
	}
	redirect := q.Get("redirect_uri")
	if !slices.Contains(c.RedirectURIs, redirect) {
		http.Error(w, "redirect_uri is not registered for this client", 400)
		return
	}
	fail := func(kind, desc string) {
		u, _ := url.Parse(redirect)
		v := u.Query()
		v.Set("error", kind)
		v.Set("error_description", desc)
		if st := q.Get("state"); st != "" {
			v.Set("state", st)
		}
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only code is supported")
		return
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "PKCE with S256 is required")
		return
	}
	req := &auth.Request{ClientID: c.ID, RedirectURI: redirect, State: q.Get("state"), CodeChallenge: q.Get("code_challenge"),
		Scope: q.Get("scope"), Resource: q.Get("resource")}
	if err := s.Auth.SaveRequest(r.Context(), org.ID, req); err != nil {
		fail("server_error", "try again")
		return
	}
	http.Redirect(w, r, "/authorize?request="+url.QueryEscape(req.ID), http.StatusFound)
}

func (s *Server) oauthRequest(w http.ResponseWriter, r *http.Request) {
	p := s.humanPrincipal(w, r)
	if p == nil {
		return
	}
	req, err := s.Auth.GetRequest(r.Context(), p.Org.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "this sign-in request expired; start again from your agent")
		return
	}
	c, err := s.Auth.Client(r.Context(), p.Org.ID, req.ClientID)
	if err != nil {
		writeErr(w, 404, "unknown client")
		return
	}
	u, _ := url.Parse(req.RedirectURI)
	writeJSON(w, 200, map[string]any{"client_name": c.Name, "redirect_host": u.Host, "org": p.Org.Name})
}

func (s *Server) consent(w http.ResponseWriter, r *http.Request) {
	p := s.humanPrincipal(w, r)
	if p == nil {
		return
	}
	var in struct {
		Request string `json:"request"`
		Approve bool   `json:"approve"`
		Name    string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	req, err := s.Auth.GetRequest(r.Context(), p.Org.ID, in.Request)
	if err != nil {
		writeErr(w, 404, "this sign-in request expired; start again from your agent")
		return
	}
	u, _ := url.Parse(req.RedirectURI)
	v := u.Query()
	if req.State != "" {
		v.Set("state", req.State)
	}
	if !in.Approve {
		_ = s.Auth.DeleteRequest(r.Context(), p.Org.ID, req.ID)
		v.Set("error", "access_denied")
		u.RawQuery = v.Encode()
		writeJSON(w, 200, map[string]string{"redirect": u.String()})
		return
	}
	c, err := s.Auth.Client(r.Context(), p.Org.ID, req.ClientID)
	if err != nil {
		writeErr(w, 404, "unknown client")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = c.Name
	}
	agent, err := s.Svc.AgentForClient(r.Context(), p, c.ID, name)
	if err != nil {
		writeCoreErr(w, err)
		return
	}
	code, err := s.Auth.IssueCode(r.Context(), p.Org.ID, req, agent.ID)
	if err != nil {
		writeErr(w, 500, "internal error")
		return
	}
	v.Set("code", code)
	v.Set("iss", s.issuer(r))
	u.RawQuery = v.Encode()
	writeJSON(w, 200, map[string]string{"redirect": u.String()})
}

func (s *Server) clientCreds(r *http.Request) (string, string) {
	if id, secret, ok := r.BasicAuth(); ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
		return id, secret
	}
	return r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if err := r.ParseForm(); err != nil {
		oauthErr(w, 400, "invalid_request", "form body required")
		return
	}
	org := orgFrom(r)
	id, secret := s.clientCreds(r)
	c, err := s.Auth.Client(r.Context(), org.ID, id)
	if err != nil || !c.CheckSecret(secret) {
		oauthErr(w, 401, "invalid_client", "unknown client or bad secret")
		return
	}
	var t *auth.Tokens
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		t, err = s.Auth.ExchangeCode(r.Context(), org.ID, c.ID, r.PostForm.Get("code"), r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier"))
	case "refresh_token":
		t, err = s.Auth.Refresh(r.Context(), org.ID, c.ID, r.PostForm.Get("refresh_token"))
	default:
		oauthErr(w, 400, "unsupported_grant_type", "use authorization_code or refresh_token")
		return
	}
	if err != nil {
		oauthErr(w, 400, "invalid_grant", "code or refresh token is invalid, expired or already used")
		return
	}
	if _, err := s.Svc.Principal(r.Context(), org, t.MemberID, "mcp"); err != nil {
		oauthErr(w, 400, "invalid_grant", "this agent was revoked")
		return
	}
	writeJSON(w, 200, map[string]any{"access_token": t.Access, "token_type": "Bearer", "expires_in": t.ExpiresIn,
		"refresh_token": t.Refresh, "scope": "tasks"})
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err == nil {
		_ = s.Auth.Revoke(r.Context(), orgFrom(r).ID, r.PostForm.Get("token"))
	}
	w.WriteHeader(200)
}

// connectionCallback is where a service sends the admin back after approving an MCP connection.
func (s *Server) connectionCallback(w http.ResponseWriter, r *http.Request) {
	back := func(q string) { http.Redirect(w, r, "/settings/connections?"+q, http.StatusFound) }
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		msg := q.Get("error_description")
		if msg == "" {
			msg = e
		}
		back("error=" + url.QueryEscape("The service said: "+msg))
		return
	}
	p := s.humanPrincipal(w, r)
	if p == nil {
		return
	}
	c, err := s.Svc.FinishConnectionOAuth(r.Context(), p, q.Get("state"), q.Get("code"))
	if err != nil {
		back("error=" + url.QueryEscape(tools.Public(err).Error()))
		return
	}
	back("connected=" + url.QueryEscape(c.Name))
}
