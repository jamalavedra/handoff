// Package server is Handoff's HTTP surface: the web app and its JSON API,
// email/Google login, OAuth 2.1 for MCP clients, and the MCP endpoint.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joalavedra/handoff/internal/auth"
	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
	"github.com/joalavedra/handoff/internal/mail"
	"github.com/joalavedra/handoff/internal/tools"
)

const sessionCookie = "handoff_session"

type Server struct {
	Cfg     *config.Config
	Svc     *core.Service
	Auth    *auth.Store
	Mail    *mail.Mailer
	Version string
	Static  fs.FS

	tools   []*tools.Tool
	byName  map[string]*tools.Tool
	limiter *limiter

	live     *hub
	liveOnce sync.Once
}

func New(cfg *config.Config, svc *core.Service, m *mail.Mailer, static fs.FS, version string) *Server {
	all := tools.All(svc)
	return &Server{Cfg: cfg, Svc: svc, Auth: &auth.Store{DB: svc.DB}, Mail: m, Static: static, Version: version,
		tools: all, byName: tools.ByName(all), limiter: newLimiter(10, time.Minute),
		live: &hub{subs: map[string]map[chan liveMsg]struct{}{}}}
}

type ctxKey int

const orgKey ctxKey = 0

func orgFrom(r *http.Request) *core.Org {
	o, _ := r.Context().Value(orgKey).(*core.Org)
	return o
}

func (s *Server) Handler() http.Handler {
	apex := http.NewServeMux()
	apex.HandleFunc("GET /healthz", s.health)
	apex.HandleFunc("POST /api/signup", s.signup)
	apex.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"apex": true, "base_host": s.Cfg.BaseHost(), "signup": s.Cfg.AllowSignup})
	})
	apex.HandleFunc("GET /auth/google/callback", s.googleCallback)
	apex.HandleFunc("GET /llms.txt", s.llms)
	apex.HandleFunc("GET /llms-full.txt", s.llmsFull)
	apex.Handle("/", s.spa())

	org := http.NewServeMux()
	org.HandleFunc("GET /healthz", s.health)
	org.HandleFunc("GET /.well-known/oauth-protected-resource", s.protectedResource)
	org.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.protectedResource)
	org.HandleFunc("GET /.well-known/oauth-authorization-server", s.authServerMetadata)
	org.HandleFunc("POST /oauth/register", s.register)
	org.HandleFunc("GET /oauth/authorize", s.authorize)
	org.HandleFunc("GET /oauth/mcp/callback", s.connectionCallback)
	org.HandleFunc("POST /oauth/token", s.token)
	org.HandleFunc("POST /oauth/revoke", s.revoke)
	org.Handle("/mcp", s.mcp())
	org.HandleFunc("POST /api/login", s.login)
	org.HandleFunc("POST /api/app/code", s.appCode)
	org.HandleFunc("POST /api/app/redeem", s.appRedeem)
	org.HandleFunc("GET /auth/verify", s.verifyPage)
	org.HandleFunc("POST /auth/verify", s.verify)
	org.HandleFunc("GET /auth/google", s.googleStart)
	org.HandleFunc("POST /api/logout", s.logout)
	org.HandleFunc("GET /api/me", s.me)
	org.HandleFunc("POST /api/tools/{name}", s.callTool)
	org.HandleFunc("GET /api/live", s.liveStream)
	org.HandleFunc("GET /api/image", s.blockImage)
	org.HandleFunc("GET /api/oauth/request/{id}", s.oauthRequest)
	org.HandleFunc("POST /api/oauth/consent", s.consent)
	org.HandleFunc("GET /api/agents/{id}/keys", s.listKeys)
	org.HandleFunc("POST /api/agents/{id}/keys", s.createKey)
	org.HandleFunc("DELETE /api/agents/{id}/keys/{key}", s.revokeKey)
	org.HandleFunc("GET /api/beads/export", s.beadsExport)
	org.HandleFunc("POST /api/beads/import", s.beadsImport)
	org.HandleFunc("GET /llms.txt", s.llms)
	org.HandleFunc("GET /llms-full.txt", s.llmsFull)
	org.Handle("/", s.spa())

	return s.recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		if r.URL.Path == "/internal/tls-ask" {
			s.tlsAsk(w, r)
			return
		}
		if r.URL.Path == "/oauth/mcp/callback" && strings.HasPrefix(r.Host, "localhost") {
			if to, ok := s.Svc.LoopbackCallback(r.Host, r.URL.Query()); ok {
				http.Redirect(w, r, to, http.StatusFound)
				return
			}
		}
		o, err := s.Svc.OrgForHost(r.Context(), r.Host)
		switch {
		case errors.Is(err, core.ErrNoOrg):
			http.Error(w, "No Handoff workspace at "+r.Host, http.StatusNotFound)
		case err != nil:
			slog.Error("org lookup", "host", r.Host, "err", err)
			http.Error(w, "internal error", 500)
		case o == nil:
			apex.ServeHTTP(w, r)
		default:
			org.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), orgKey, o)))
		}
	}))
}

func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("panic", "path", r.URL.Path, "err", v)
				http.Error(w, "internal error", 500)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.DB.Pool.Ping(r.Context()); err != nil {
		http.Error(w, "db down", 503)
		return
	}
	io.WriteString(w, "ok")
}

// tlsAsk lets Caddy's on-demand TLS issue certificates only for real workspaces.
func (s *Server) tlsAsk(w http.ResponseWriter, r *http.Request) {
	d := r.URL.Query().Get("domain")
	if d == s.Cfg.BaseHost() {
		return
	}
	if s.Cfg.SingleOrg != "" {
		http.Error(w, "unknown", 404)
		return
	}
	if o, err := s.Svc.OrgForHost(r.Context(), d); err != nil || o == nil {
		http.Error(w, "unknown", 404)
	}
}

// spa serves the built web app, falling back to index.html for client routes.
func (s *Server) spa() http.Handler {
	files := http.FileServerFS(s.Static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := s.Static.Open(p); err == nil {
				st, _ := f.Stat()
				f.Close()
				if st != nil && !st.IsDir() {
					if strings.HasPrefix(p, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					files.ServeHTTP(w, r)
					return
				}
			}
			if strings.HasPrefix(p, "api/") || strings.HasPrefix(p, "assets/") {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		if _, err := fs.Stat(s.Static, "index.html"); err != nil {
			http.Error(w, "The web app isn't built. Run `make web` and rebuild the server.", http.StatusServiceUnavailable)
			return
		}
		http.ServeFileFS(w, r, s.Static, "index.html")
	})
}

// ---- auth helpers ----

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	return ""
}

// agentPrincipal authenticates an API key or OAuth access token.
func (s *Server) agentPrincipal(r *http.Request, via string) (*core.Principal, error) {
	org := orgFrom(r)
	tok := bearer(r)
	if tok == "" {
		return nil, auth.ErrInvalid
	}
	var id string
	var err error
	if strings.HasPrefix(tok, auth.APIKeyPrefix) {
		id, err = s.Auth.APIKeyMember(r.Context(), org.ID, tok)
	} else {
		id, _, err = s.Auth.AccessTokenMember(r.Context(), org.ID, tok)
	}
	if err != nil {
		return nil, err
	}
	return s.Svc.Principal(r.Context(), org, id, via)
}

func (s *Server) sessionPrincipal(r *http.Request) (*core.Principal, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, auth.ErrInvalid
	}
	org := orgFrom(r)
	id, err := s.Auth.SessionMember(r.Context(), org.ID, c.Value)
	if err != nil {
		return nil, err
	}
	p, err := s.Svc.Principal(r.Context(), org, id, "web")
	if err != nil {
		return nil, err
	}
	if p.Member.IsAgent() {
		return nil, auth.ErrInvalid
	}
	return p, nil
}

// apiPrincipal accepts a session (web app, CSRF-checked) or a bearer credential (scripts).
func (s *Server) apiPrincipal(w http.ResponseWriter, r *http.Request) *core.Principal {
	if bearer(r) != "" {
		p, err := s.agentPrincipal(r, "api")
		if err != nil {
			writeErr(w, 401, "invalid or expired token")
			return nil
		}
		return p
	}
	if r.Method != http.MethodGet && r.Header.Get("X-Handoff") != "1" {
		writeErr(w, 403, "missing X-Handoff header")
		return nil
	}
	p, err := s.sessionPrincipal(r)
	if err != nil {
		writeErr(w, 401, "sign in first")
		return nil
	}
	return p
}

func (s *Server) humanPrincipal(w http.ResponseWriter, r *http.Request) *core.Principal {
	p := s.apiPrincipal(w, r)
	if p != nil && p.Member.IsAgent() {
		writeErr(w, 403, "only people can do this")
		return nil
	}
	return p
}

// ---- JSON API ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeCoreErr(w http.ResponseWriter, err error) {
	var ce *core.Error
	if !errors.As(err, &ce) {
		err = tools.Public(err)
		writeErr(w, 500, err.Error())
		return
	}
	code := map[string]int{core.ErrNotFound: 404, core.ErrForbidden: 403, core.ErrInvalid: 400, core.ErrConflict: 409}[ce.Code]
	if code == 0 {
		code = 400
	}
	writeErr(w, code, ce.Msg)
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	org := orgFrom(r)
	base := map[string]any{
		"org":      map[string]string{"name": org.Name, "slug": org.Slug},
		"mcp_url":  s.Cfg.URLForHost(r.Host) + "/mcp",
		"google":   s.Cfg.GoogleClientID != "",
		"base_url": s.Cfg.URLForHost(r.Host),
	}
	p, err := s.sessionPrincipal(r)
	if err != nil {
		base["member"] = nil
		writeJSON(w, 200, base)
		return
	}
	base["org"] = p.Org
	base["member"] = map[string]any{"id": p.Member.ID, "name": p.Member.Name, "email": p.Member.Email, "role": p.Member.Role}
	writeJSON(w, 200, base)
}

func (s *Server) callTool(w http.ResponseWriter, r *http.Request) {
	t, ok := s.byName[r.PathValue("name")]
	if !ok {
		writeErr(w, 404, "unknown tool")
		return
	}
	p := s.apiPrincipal(w, r)
	if p == nil {
		return
	}
	if p.Member.IsAgent() && !t.Agent {
		writeErr(w, 403, "agents can't call "+t.Name)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, 400, "bad body")
		return
	}
	out, err := t.Call(r.Context(), p, raw)
	if err != nil {
		writeCoreErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	p := s.humanPrincipal(w, r)
	if p == nil {
		return
	}
	keys, err := s.Svc.ListAPIKeys(r.Context(), p, r.PathValue("id"))
	if err != nil {
		writeCoreErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"keys": keys})
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	p := s.humanPrincipal(w, r)
	if p == nil {
		return
	}
	k, err := s.Svc.CreateAPIKey(r.Context(), p, r.PathValue("id"), auth.NewToken(auth.APIKeyPrefix))
	if err != nil {
		writeCoreErr(w, err)
		return
	}
	writeJSON(w, 200, k)
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	p := s.humanPrincipal(w, r)
	if p == nil {
		return
	}
	if err := s.Svc.RevokeAPIKey(r.Context(), p, r.PathValue("id"), r.PathValue("key")); err != nil {
		writeCoreErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- MCP ----

func (s *Server) resourceMetadataURL(r *http.Request) string {
	return s.Cfg.URLForHost(r.Host) + "/.well-known/oauth-protected-resource/mcp"
}

func (s *Server) mcp() http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		p, _ := r.Context().Value(principalKey{}).(*core.Principal)
		return tools.Server(s.tools, s.Version, p)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true,
		// Workspaces are routed by Host and every call needs a bearer token, so
		// DNS-rebinding protection (which rejects non-localhost hosts) doesn't apply.
		DisableLocalhostProtection: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.agentPrincipal(r, "mcp")
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+s.resourceMetadataURL(r)+`"`)
			writeErr(w, 401, "authentication required: add this URL to your MCP client to sign in, or send an agent API key as a Bearer token")
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

type principalKey struct{}

// ---- rate limiting for login emails ----

type limiter struct {
	mu     sync.Mutex
	n      int
	window time.Duration
	hits   map[string][]time.Time
	swept  time.Time
}

func newLimiter(n int, window time.Duration) *limiter {
	return &limiter{n: n, window: window, hits: map[string][]time.Time{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// Forget only expired keys, so a flood of new ones can't reset everyone's limit; sweep at most once a window so it stays cheap.
	// shortcut: memory grows with distinct client IPs per window, move limits to Postgres or Redis if that ever matters.
	if len(l.hits) > 10000 && now.Sub(l.swept) >= l.window {
		l.swept = now
		for k, ts := range l.hits {
			if now.Sub(ts[len(ts)-1]) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	h := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			h = append(h, t)
		}
	}
	if len(h) >= l.n {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)
	return true
}

func clientIP(r *http.Request) string {
	// The proxy in front (deploy/Caddyfile) appends the address it saw last; earlier entries are the client's to forge.
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		return strings.TrimSpace(f[strings.LastIndex(f, ",")+1:])
	}
	ip := r.RemoteAddr
	if i := strings.LastIndex(ip, ":"); i > 0 {
		ip = ip[:i]
	}
	return ip
}

// blockImage serves a picture an agent's block shows, fetched by the server so viewers never contact
// the site it comes from. Only signed addresses are served (see core.ProxyImage).
func (s *Server) blockImage(w http.ResponseWriter, r *http.Request) {
	if p := s.apiPrincipal(w, r); p == nil {
		return
	}
	ct, b, err := s.Svc.ProxyImage(r.Context(), r.URL.Query().Get("u"), r.URL.Query().Get("s"))
	if err != nil {
		writeCoreErr(w, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}
