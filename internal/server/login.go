package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/joalavedra/handoff/internal/auth"
	"github.com/joalavedra/handoff/internal/core"
)

// safeNext keeps a post-sign-in redirect on this site. Browsers drop tabs and newlines from URLs,
// so "/\t/evil.com" would become "//evil.com"; control characters are refused.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, `\`) ||
		strings.ContainsFunc(next, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "/"
	}
	return next
}

// sendLoginLink emails a one-time link; in dev mode the link is also returned.
func (s *Server) sendLoginLink(r *http.Request, org *core.Org, email, name, next string) (string, error) {
	tok, err := s.Auth.CreateLoginToken(r.Context(), org.ID, email, name, safeNext(next))
	if err != nil {
		return "", err
	}
	link := s.Cfg.OrgURL(org.Slug) + "/auth/verify?token=" + url.QueryEscape(tok)
	if org.CustomDomain != "" {
		link = s.Cfg.URLForHost(org.CustomDomain) + "/auth/verify?token=" + url.QueryEscape(tok)
	}
	body := fmt.Sprintf("Sign in to %s on Handoff:\n\n%s\n\nThe link works once and expires in 15 minutes. If you didn't ask for it, ignore this email.\n", org.Name, link)
	if err := s.Mail.Send(email, "Sign in to "+org.Name+" on Handoff", body); err != nil {
		return "", err
	}
	if s.Cfg.Dev {
		return link, nil
	}
	return "", nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Next  string `json:"next"`
	}
	if err := readJSON(r, &in); err != nil || !strings.Contains(in.Email, "@") {
		writeErr(w, 400, "enter your work email")
		return
	}
	if !s.limiter.allow("login:" + clientIP(r)) {
		writeErr(w, 429, "too many attempts; wait a minute")
		return
	}
	org := orgFrom(r)
	resp := map[string]any{"sent": true}
	// Only members get a link; the response is the same either way so emails can't be probed.
	// Nobody is added or activated until the link is opened, which proves the address.
	if err := s.Svc.CanSignIn(r.Context(), org, in.Email); err == nil {
		link, err := s.sendLoginLink(r, org, in.Email, "", in.Next)
		if err != nil {
			slog.Error("send login link", "err", err)
			writeErr(w, 500, "couldn't send the email")
			return
		}
		if link != "" {
			resp["dev_link"] = link
		}
	} else if !core.IsCode(err, core.ErrForbidden) {
		writeCoreErr(w, err)
		return
	}
	writeJSON(w, 200, resp)
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	org := orgFrom(r)
	lt, err := s.Auth.ConsumeLoginToken(r.Context(), org.ID, r.URL.Query().Get("token"))
	if err != nil {
		http.Redirect(w, r, "/login?error=expired", http.StatusSeeOther)
		return
	}
	m, err := s.Svc.MemberForLogin(r.Context(), org, lt.Email, lt.Name)
	if err != nil {
		http.Redirect(w, r, "/login?error=denied", http.StatusSeeOther)
		return
	}
	tok, err := s.Auth.CreateSession(r.Context(), org.ID, m.ID)
	if err != nil {
		slog.Error("create session", "err", err)
		http.Error(w, "internal error", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, Secure: s.Cfg.Secure(),
		SameSite: http.SameSiteLaxMode, MaxAge: int(auth.SessionTTL.Seconds())})
	http.Redirect(w, r, safeNext(lt.Next), http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.Auth.DeleteSession(r.Context(), orgFrom(r).ID, c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Cfg.Secure()})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// signup creates a workspace from the apex site and emails its first admin a login link.
func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.AllowSignup {
		writeErr(w, 403, "signup is closed on this server")
		return
	}
	if !s.limiter.allow("signup:" + clientIP(r)) {
		writeErr(w, 429, "too many attempts; wait a minute")
		return
	}
	var in struct {
		Slug, Name, Email, AdminName string
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	org, admin, err := s.Svc.CreateOrg(r.Context(), core.NewOrg{Slug: in.Slug, Name: in.Name, AdminEmail: in.Email, AdminName: in.AdminName})
	if err != nil {
		writeCoreErr(w, err)
		return
	}
	link, err := s.sendLoginLink(r, org, admin.Email, admin.Name, "/")
	if err != nil {
		slog.Error("send signup link", "err", err)
		writeErr(w, 500, "workspace created, but the email failed; sign in from "+s.Cfg.OrgURL(org.Slug))
		return
	}
	resp := map[string]any{"url": s.Cfg.OrgURL(org.Slug), "sent": true}
	if link != "" {
		resp["dev_link"] = link
	}
	writeJSON(w, 200, resp)
}

// ---- Google sign-in ----
// Google needs fixed redirect URIs, so every workspace starts the flow but the
// callback lands on the apex, which hands the user back with a one-time login token.

func (s *Server) sign(v string) string {
	m := hmac.New(sha256.New, s.Cfg.Secret)
	m.Write([]byte(v))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Server) googleRedirect() string {
	return s.Cfg.BaseURL.String() + "/auth/google/callback"
}

func (s *Server) googleStart(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.GoogleClientID == "" {
		http.NotFound(w, r)
		return
	}
	org := orgFrom(r)
	payload := strings.Join([]string{org.ID, safeNext(r.URL.Query().Get("next")), strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)}, "|")
	state := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + s.sign(payload)
	q := url.Values{"client_id": {s.Cfg.GoogleClientID}, "redirect_uri": {s.googleRedirect()}, "response_type": {"code"},
		"scope": {"openid email profile"}, "state": {state}, "prompt": {"select_account"}}
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+q.Encode(), http.StatusFound)
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string, err error) {
		slog.Warn("google login", "msg", msg, "err", err)
		http.Error(w, "Google sign-in failed: "+msg, 400)
	}
	b64, sig, ok := strings.Cut(r.URL.Query().Get("state"), ".")
	raw, err := base64.RawURLEncoding.DecodeString(b64)
	if !ok || err != nil || !hmac.Equal([]byte(sig), []byte(s.sign(string(raw)))) {
		fail("bad state", err)
		return
	}
	parts := strings.Split(string(raw), "|")
	exp, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if len(parts) != 3 || time.Now().Unix() > exp {
		fail("expired", nil)
		return
	}
	org, err := s.Svc.OrgByID(r.Context(), parts[0])
	if err != nil {
		fail("unknown workspace", err)
		return
	}
	email, name, err := s.googleUser(r, r.URL.Query().Get("code"))
	if err != nil {
		fail("couldn't read your Google account", err)
		return
	}
	if _, err := s.Svc.MemberForLogin(r.Context(), org, email, name); err != nil {
		http.Redirect(w, r, s.Cfg.OrgURL(org.Slug)+"/login?error=denied", http.StatusSeeOther)
		return
	}
	tok, err := s.Auth.CreateLoginToken(r.Context(), org.ID, email, name, parts[1])
	if err != nil {
		fail("internal error", err)
		return
	}
	host := s.Cfg.OrgURL(org.Slug)
	if org.CustomDomain != "" {
		host = s.Cfg.URLForHost(org.CustomDomain)
	}
	http.Redirect(w, r, host+"/auth/verify?token="+url.QueryEscape(tok), http.StatusSeeOther)
}

func (s *Server) googleUser(r *http.Request, code string) (email, name string, err error) {
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{"code": {code}, "client_id": {s.Cfg.GoogleClientID},
		"client_secret": {s.Cfg.GoogleClientSecret}, "redirect_uri": {s.googleRedirect()}, "grant_type": {"authorization_code"}})
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		return "", "", errors.New("token exchange failed")
	}
	req, _ := http.NewRequestWithContext(r.Context(), "GET", "https://openidconnect.googleapis.com/v1/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	ur, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer ur.Body.Close()
	var u struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(ur.Body).Decode(&u); err != nil || u.Email == "" || !u.Verified {
		return "", "", errors.New("no verified email")
	}
	return u.Email, u.Name, nil
}
