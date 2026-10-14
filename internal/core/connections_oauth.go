package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/config"
)

// Signing in with the service: most hosted MCP servers use OAuth 2.1 with dynamic client
// registration and PKCE. Handoff finds the server's authorization server, registers itself, sends the
// admin there to approve (the service's own permissions apply), and keeps the tokens sealed,
// refreshing them as they expire. The connection is the workspace's, made with that admin's access.

type StartConnectionOAuthIn struct {
	Name   string `json:"name"`
	URL    string `json:"url" jsonschema:"the MCP server's endpoint"`
	Origin string `json:"origin" jsonschema:"the address the admin is using Handoff at, for the callback"`
}

type StartConnectionOAuthOut struct {
	AuthorizeURL string `json:"authorize_url"`
}

// oauthSecret is what's sealed for an OAuth connection.
type oauthSecret struct {
	Access        string    `json:"access"`
	Refresh       string    `json:"refresh,omitempty"`
	Expiry        time.Time `json:"expiry,omitzero"`
	ClientID      string    `json:"client_id"`
	TokenEndpoint string    `json:"token_endpoint"`
	Resource      string    `json:"resource"`
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// callbackBase checks the address the admin is on: it must be this workspace's host (any port), so a
// sign-in can only return to Handoff itself.
func (s *Service) callbackBase(org *Org, origin string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errf(ErrInvalid, "origin must be the address you use Handoff at")
	}
	want, _ := url.Parse(s.Cfg.OrgURL(org.Slug))
	ok := want != nil && strings.EqualFold(u.Hostname(), want.Hostname())
	if org.CustomDomain != "" && strings.EqualFold(u.Hostname(), org.CustomDomain) {
		ok = true
	}
	if !ok {
		return "", errf(ErrInvalid, "origin %s isn't this workspace's address", u.Host)
	}
	if u.Scheme == "http" && want.Scheme == "https" && !s.Cfg.Dev {
		return "", errf(ErrInvalid, "sign-in needs https")
	}
	return u.Scheme + "://" + u.Host, nil
}

func (s *Service) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.outbound().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", u, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

type authServerMeta struct {
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// discoverAuth finds the authorization server for an MCP endpoint (RFC 9728, then RFC 8414).
func (s *Service) discoverAuth(ctx context.Context, endpoint string) (authServerMeta, []string, error) {
	var meta authServerMeta
	u, _ := url.Parse(endpoint)
	root := u.Scheme + "://" + u.Host
	var prm struct {
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	err := s.getJSON(ctx, root+"/.well-known/oauth-protected-resource"+strings.TrimRight(u.Path, "/"), &prm)
	if err != nil || len(prm.AuthorizationServers) == 0 {
		if err = s.getJSON(ctx, root+"/.well-known/oauth-protected-resource", &prm); err != nil || len(prm.AuthorizationServers) == 0 {
			return meta, nil, errf(ErrInvalid, "%s doesn't say how to sign in (no OAuth metadata); try an API key instead", u.Host)
		}
	}
	as := strings.TrimRight(prm.AuthorizationServers[0], "/")
	if _, err := triageBaseURL(as); err != nil {
		return meta, nil, errf(ErrInvalid, "the sign-in server address isn't valid")
	}
	if err := s.getJSON(ctx, as+"/.well-known/oauth-authorization-server", &meta); err != nil || meta.TokenEndpoint == "" {
		if err := s.getJSON(ctx, as+"/.well-known/openid-configuration", &meta); err != nil || meta.TokenEndpoint == "" {
			return meta, nil, errf(ErrInvalid, "couldn't read the sign-in server's settings at %s", as)
		}
	}
	for _, e := range []string{meta.AuthorizationEndpoint, meta.TokenEndpoint} {
		if _, err := url.Parse(e); err != nil || !strings.HasPrefix(e, "http") {
			return meta, nil, errf(ErrInvalid, "the sign-in server's endpoints aren't valid")
		}
	}
	scopes := prm.ScopesSupported
	if len(scopes) == 0 {
		scopes = meta.ScopesSupported
	}
	return meta, scopes, nil
}

// StartConnectionOAuth begins signing in to an MCP server: it returns where to send the admin.
func (s *Service) StartConnectionOAuth(ctx context.Context, p *Principal, in StartConnectionOAuthIn) (StartConnectionOAuthOut, error) {
	var out StartConnectionOAuthOut
	if err := p.requireAdmin(); err != nil {
		return out, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 60 {
		return out, errf(ErrInvalid, "name must be 1-60 characters")
	}
	endpoint, err := triageBaseURL(in.URL)
	if err != nil {
		return out, errf(ErrInvalid, "url must be an http(s) URL without credentials in it")
	}
	base, err := s.callbackBase(p.Org, in.Origin)
	if err != nil {
		return out, err
	}
	redirect := base + "/oauth/mcp/callback"
	verifier, state := randomToken(32), randomToken(24)
	if h := strings.TrimPrefix(base, "http://"); h != base && strings.HasSuffix(strings.Split(h, ":")[0], ".localhost") {
		// Local development: services accept http only for plain localhost, so the sign-in comes back
		// there and LoopbackCallback forwards it to the workspace named at the start of the state.
		port := ""
		if i := strings.LastIndex(h, ":"); i >= 0 {
			port = h[i:]
		}
		redirect = "http://localhost" + port + "/oauth/mcp/callback"
		state = p.Org.Slug + "~" + state
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	meta, scopes, err := s.discoverAuth(ctx, endpoint)
	if err != nil {
		return out, err
	}
	if meta.RegistrationEndpoint == "" {
		return out, errf(ErrInvalid, "the service doesn't let apps register themselves; use an API key if it offers one")
	}
	// Register Handoff with the service for this workspace (public client: PKCE, no secret).
	reg, _ := json.Marshal(map[string]any{
		"client_name": "Handoff · " + p.Org.Name, "redirect_uris": []string{redirect},
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none", "scope": strings.Join(scopes, " "),
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, meta.RegistrationEndpoint, strings.NewReader(string(reg)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.outbound().Do(req)
	if err != nil {
		return out, errf(ErrInvalid, "couldn't register with the service: %s", clip(err.Error(), 150))
	}
	defer resp.Body.Close()
	var client struct {
		ClientID string `json:"client_id"`
	}
	if resp.StatusCode/100 != 2 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&client) != nil || client.ClientID == "" {
		return out, errf(ErrInvalid, "the service refused to register Handoff (%d)", resp.StatusCode)
	}
	challenge := sha256.Sum256([]byte(verifier))
	err = s.run(ctx, p, func(c *call) error {
		_, err := c.tx.Exec(c.ctx, `INSERT INTO mcp_oauth_pending (org_id, state, name, url, redirect_uri, client_id, verifier, token_endpoint, created_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now() + interval '15 minutes')`,
			p.Org.ID, state, name, endpoint, redirect, client.ClientID, verifier, meta.TokenEndpoint, p.Member.ID)
		return err
	})
	if err != nil {
		return out, err
	}
	q := url.Values{
		"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {redirect},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
		"state": {state}, "resource": {endpoint},
	}
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	sep := "?"
	if strings.Contains(meta.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	out.AuthorizeURL = meta.AuthorizationEndpoint + sep + q.Encode()
	return out, nil
}

// tokenRequest posts a form to the token endpoint and reads the tokens.
func (s *Service) tokenRequest(ctx context.Context, endpoint string, form url.Values) (oauthSecret, error) {
	var sec oauthSecret
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.outbound().Do(req)
	if err != nil {
		return sec, err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok); err != nil || tok.AccessToken == "" {
		if tok.Error != "" {
			return sec, fmt.Errorf("the service said %s", tok.Error)
		}
		return sec, fmt.Errorf("the service didn't return a token (%d)", resp.StatusCode)
	}
	sec.Access, sec.Refresh = tok.AccessToken, tok.RefreshToken
	if tok.ExpiresIn > 0 {
		sec.Expiry = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return sec, nil
}

// FinishConnectionOAuth completes the sign-in the same admin started and adds the connection.
func (s *Service) FinishConnectionOAuth(ctx context.Context, p *Principal, state, code string) (ConnectionOut, error) {
	var out ConnectionOut
	if err := p.requireAdmin(); err != nil {
		return out, err
	}
	var name, endpoint, redirect, clientID, verifier, tokenEndpoint string
	err := s.run(ctx, p, func(c *call) error {
		return c.tx.QueryRow(c.ctx, `DELETE FROM mcp_oauth_pending WHERE state = $1 AND created_by = $2 AND expires_at > now()
			RETURNING name, url, redirect_uri, client_id, verifier, token_endpoint`, state, p.Member.ID).
			Scan(&name, &endpoint, &redirect, &clientID, &verifier, &tokenEndpoint)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, errf(ErrInvalid, "this sign-in expired or was already used; start again")
	}
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sec, err := s.tokenRequest(ctx, tokenEndpoint, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {clientID}, "code_verifier": {verifier}, "resource": {endpoint},
	})
	if err != nil {
		return out, errf(ErrInvalid, "sign-in failed: %s", clip(err.Error(), 150))
	}
	sec.ClientID, sec.TokenEndpoint, sec.Resource = clientID, tokenEndpoint, endpoint
	tools, err := s.listServerTools(ctx, connServer{url: endpoint, auth: "oauth", secret: sec.Access})
	if err != nil {
		return out, errf(ErrInvalid, "signed in, but couldn't list the tools: %s", clip(err.Error(), 150))
	}
	return s.saveConnection(ctx, p, name, endpoint, "oauth", "", sec, tools)
}

// saveConnection stores a new connection (sealing its secret) with its read-only tools switched on.
func (s *Service) saveConnection(ctx context.Context, p *Principal, name, endpoint, auth, hdr string, secret any, tools []connTool) (ConnectionOut, error) {
	var out ConnectionOut
	var sealed []byte
	switch v := secret.(type) {
	case string:
		if v != "" {
			var err error
			if sealed, err = s.seal([]byte(v)); err != nil {
				return out, err
			}
		}
	case oauthSecret:
		b, _ := json.Marshal(v)
		var err error
		if sealed, err = s.seal(b); err != nil {
			return out, err
		}
	}
	var on []string
	for _, t := range tools {
		if t.ReadOnly {
			on = append(on, t.Name)
		}
	}
	toolsJSON, _ := json.Marshal(tools)
	err := s.run(ctx, p, func(c *call) error {
		var id string
		err := c.tx.QueryRow(c.ctx, `INSERT INTO mcp_connections (org_id, name, url, auth, header_name, secret, tools, enabled, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id::text`,
			p.Org.ID, name, endpoint, auth, hdr, sealed, toolsJSON, nonNil(on), p.Member.ID).Scan(&id)
		if err != nil {
			if strings.Contains(err.Error(), "mcp_connections_org_id_name_key") {
				return errf(ErrConflict, "there's already a connection called %s", name)
			}
			return err
		}
		if err := c.event("", "connection_added", map[string]any{"name": name, "url": endpoint, "tools": len(tools)}); err != nil {
			return err
		}
		all, err := c.connections()
		for _, o := range all {
			if o.ID == id {
				out = o
			}
		}
		return err
	})
	return out, err
}

// freshAccess returns a usable access token for an OAuth connection, refreshing (and saving) it when
// it's about to expire.
func (s *Service) freshAccess(ctx context.Context, org, id string, sealed []byte) (string, error) {
	b, err := s.unseal(sealed)
	if err != nil {
		return "", err
	}
	var sec oauthSecret
	if err := json.Unmarshal(b, &sec); err != nil {
		return "", err
	}
	if sec.Expiry.IsZero() || time.Until(sec.Expiry) > time.Minute || sec.Refresh == "" {
		return sec.Access, nil
	}
	next, err := s.tokenRequest(ctx, sec.TokenEndpoint, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {sec.Refresh}, "client_id": {sec.ClientID}, "resource": {sec.Resource},
	})
	if err != nil {
		return "", fmt.Errorf("the sign-in expired and couldn't be renewed (%s); connect it again", clip(err.Error(), 100))
	}
	if next.Refresh == "" {
		next.Refresh = sec.Refresh // some services keep the refresh token
	}
	next.ClientID, next.TokenEndpoint, next.Resource = sec.ClientID, sec.TokenEndpoint, sec.Resource
	nb, _ := json.Marshal(next)
	resealed, err := s.seal(nb)
	if err != nil {
		return "", err
	}
	_ = s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE mcp_connections SET secret = $2, updated_at = now() WHERE id::text = $1`, id, resealed)
		return err
	})
	return next.Access, nil
}

// LoopbackCallback is where a local-development sign-in lands (http://localhost:<port>): it returns
// the workspace address to forward the browser to, with the service's answer intact.
func (s *Service) LoopbackCallback(requestHost string, query url.Values) (string, bool) {
	if !strings.HasSuffix(s.Cfg.BaseHost(), "localhost") {
		return "", false
	}
	slug, _, ok := strings.Cut(query.Get("state"), "~")
	if !ok || !config.ValidSlug(slug) {
		return "", false
	}
	target, _ := url.Parse(s.Cfg.OrgURL(slug))
	host := target.Hostname()
	if _, port, ok := strings.Cut(requestHost, ":"); ok {
		host += ":" + port
	}
	return "http://" + host + "/oauth/mcp/callback?" + query.Encode(), true
}
