package server

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/joalavedra/handoff/internal/auth"
)

// appCode issues a pairing code for the person's mobile app. People only.
func (s *Server) appCode(w http.ResponseWriter, r *http.Request) {
	p := s.humanPrincipal(w, r)
	if p == nil {
		return
	}
	if !s.limiter.allow("appcode:" + p.Member.ID) {
		writeErr(w, 429, "too many attempts; wait a minute")
		return
	}
	code, exp, err := s.Auth.CreateAppCode(r.Context(), p.Org.ID, p.Member.ID)
	if err != nil {
		slog.Error("app code", "err", err)
		writeErr(w, 500, "couldn't make a code")
		return
	}
	writeJSON(w, 200, map[string]any{"code": code, "expires_at": exp.Unix()})
}

// appRedeem is the phone's sign-in: the code becomes a normal session. Bad,
// expired and spent codes all get the same 400, and it's throttled per client
// and per workspace because the code is short enough to guess.
func (s *Server) appRedeem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "invalid or expired code")
		return
	}
	org := orgFrom(r)
	if !s.limiter.allow("appredeem:"+clientIP(r)) || !s.limiter.allow("appredeem-org:"+org.ID) {
		writeErr(w, 429, "too many attempts; wait a minute")
		return
	}
	memberID, tok, err := s.Auth.RedeemAppCode(r.Context(), org.ID, in.Code)
	if errors.Is(err, auth.ErrInvalid) {
		writeErr(w, 400, "invalid or expired code")
		return
	}
	if err != nil {
		slog.Error("app redeem", "err", err)
		writeErr(w, 500, "internal error")
		return
	}
	p, err := s.Svc.Principal(r.Context(), org, memberID, "web")
	if err != nil || p.Member.IsAgent() {
		writeErr(w, 400, "invalid or expired code")
		return
	}
	// The app sends the token as `Cookie: handoff_session=<token>`; the cookie covers a browser flow too.
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, Secure: s.Cfg.Secure(),
		SameSite: http.SameSiteLaxMode, MaxAge: int(auth.SessionTTL.Seconds())})
	writeJSON(w, 200, map[string]any{"token": tok,
		"member": map[string]any{"id": p.Member.ID, "name": p.Member.Name, "email": p.Member.Email}})
}
