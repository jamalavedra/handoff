// Package auth stores Handoff's credentials: browser sessions, email login
// links, agent API keys and OAuth 2.1 clients, codes and tokens. Only SHA-256
// hashes of secrets are stored. Every lookup is scoped to the workspace the
// request arrived at, so a credential from one workspace is meaningless on another.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/db"
)

var ErrInvalid = errors.New("invalid or expired credential")

// ErrBusy: too many unexpired sign-in links for one address, or OAuth requests for one workspace.
var ErrBusy = errors.New("too many recent requests")

const (
	loginLinksPerEmail  = 5
	oauthRequestsPerOrg = 1000
)

const (
	SessionTTL      = 30 * 24 * time.Hour
	LoginTokenTTL   = 15 * time.Minute
	OAuthRequestTTL = 15 * time.Minute
	CodeTTL         = 2 * time.Minute
	AccessTTL       = time.Hour
	RefreshTTL      = 90 * 24 * time.Hour
	AppCodeTTL      = 10 * time.Minute

	APIKeyPrefix = "hnd_"
)

type Store struct{ DB *db.DB }

func NewToken(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

func Hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// ---- browser sessions ----

func (s *Store) CreateSession(ctx context.Context, orgID, memberID string) (string, error) {
	tok := NewToken("")
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO sessions (token_hash, org_id, member_id, expires_at) VALUES ($1, $2, $3, $4)`,
			Hash(tok), orgID, memberID, time.Now().Add(SessionTTL))
		return err
	})
	return tok, err
}

func (s *Store) SessionMember(ctx context.Context, orgID, tok string) (string, error) {
	var id string
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT member_id::text FROM sessions WHERE token_hash = $1 AND expires_at > now()`, Hash(tok)).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalid
	}
	return id, err
}

func (s *Store) DeleteSession(ctx context.Context, orgID, tok string) error {
	return s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, Hash(tok))
		return err
	})
}

// ---- email login links ----

// CreateLoginToken caps emailed links per address; Google sign-ins (emailed false) are already verified, so they skip the cap.
func (s *Store) CreateLoginToken(ctx context.Context, orgID, email, name, next string, emailed bool) (string, error) {
	tok := NewToken("")
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM login_tokens WHERE expires_at < now()`); err != nil {
			return err
		}
		if emailed {
			// The lock makes count-then-insert atomic per address across requests and processes.
			var n int
			if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('login:' || $1, 0)), (SELECT count(*) FROM login_tokens WHERE email = $1 AND used_at IS NULL)`,
				strings.ToLower(email)).Scan(nil, &n); err != nil {
				return err
			}
			if n >= loginLinksPerEmail {
				return ErrBusy
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO login_tokens (token_hash, org_id, email, name, next_path, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			Hash(tok), orgID, strings.ToLower(email), name, next, time.Now().Add(LoginTokenTTL))
		return err
	})
	return tok, err
}

type LoginToken struct{ Email, Name, Next string }

func (s *Store) ConsumeLoginToken(ctx context.Context, orgID, tok string) (*LoginToken, error) {
	lt := &LoginToken{}
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE login_tokens SET used_at = now()
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING email, name, next_path`, Hash(tok)).
			Scan(&lt.Email, &lt.Name, &lt.Next)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalid
	}
	return lt, err
}

// ---- app pairing codes ----
// A signed-in person shows their phone a short code; the app redeems it for a
// session. 8 Crockford base32 chars (~40 bits), hashed like every secret.

const appCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func newAppCode() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	var c [8]byte
	v := uint64(b[0])<<32 | uint64(b[1])<<24 | uint64(b[2])<<16 | uint64(b[3])<<8 | uint64(b[4])
	for i := 7; i >= 0; i-- {
		c[i] = appCodeAlphabet[v&31]
		v >>= 5
	}
	return string(c[:4]) + "-" + string(c[4:])
}

// normalizeAppCode accepts the code with or without its dash, any case.
func normalizeAppCode(s string) (string, bool) {
	s = strings.ToUpper(strings.ReplaceAll(s, "-", ""))
	if len(s) != 8 {
		return "", false
	}
	for i := range 8 {
		if !strings.ContainsRune(appCodeAlphabet, rune(s[i])) {
			return "", false
		}
	}
	return s, true
}

// CreateAppCode issues a single-use code for a member; a new code retires their unused ones.
func (s *Store) CreateAppCode(ctx context.Context, orgID, memberID string) (code string, expires time.Time, err error) {
	raw := newAppCode()
	expires = time.Now().Add(AppCodeTTL)
	err = s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM app_codes WHERE expires_at < now()`); err != nil {
			return err
		}
		// The lock makes retire-then-insert atomic per member across requests and processes.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('appcode:' || $1, 0))`, memberID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE app_codes SET used_at = now() WHERE member_id = $1 AND used_at IS NULL`, memberID); err != nil {
			return err
		}
		norm, _ := normalizeAppCode(raw)
		_, err = tx.Exec(ctx, `INSERT INTO app_codes (org_id, member_id, code_hash, expires_at) VALUES ($1, $2, $3, $4)`,
			orgID, memberID, Hash(norm), expires)
		return err
	})
	return raw, expires, err
}

// RedeemAppCode spends the code once and mints its session in the same transaction,
// so a failure leaves the code usable. Bad, expired and spent codes all read the same.
func (s *Store) RedeemAppCode(ctx context.Context, orgID, code string) (memberID, tok string, err error) {
	norm, ok := normalizeAppCode(code)
	if !ok {
		return "", "", ErrInvalid
	}
	tok = NewToken("")
	err = s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `UPDATE app_codes SET used_at = now()
			WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING member_id::text`, Hash(norm)).Scan(&memberID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO sessions (token_hash, org_id, member_id, expires_at) VALUES ($1, $2, $3, $4)`,
			Hash(tok), orgID, memberID, time.Now().Add(SessionTTL))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrInvalid
	}
	return memberID, tok, err
}

// ---- API keys ----

func (s *Store) APIKeyMember(ctx context.Context, orgID, key string) (string, error) {
	var id string
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE api_keys SET last_used_at = now() WHERE token_hash = $1 AND revoked_at IS NULL
			RETURNING member_id::text`, Hash(key)).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalid
	}
	return id, err
}

// ---- OAuth 2.1 ----

type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
	AuthMethod   string
	secretHash   []byte
}

// RegisterClient implements RFC 7591 dynamic registration. Public clients
// (auth method "none") get no secret and must use PKCE, as all clients must.
func (s *Store) RegisterClient(ctx context.Context, orgID, name string, redirects []string, method string) (*Client, string, error) {
	c := &Client{ID: NewToken("hc_"), Name: name, RedirectURIs: redirects, AuthMethod: method}
	var secret string
	if method != "none" {
		secret = NewToken("hcs_")
		c.secretHash = Hash(secret)
	}
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO oauth_clients (org_id, client_id, secret_hash, name, redirect_uris, auth_method)
			VALUES ($1, $2, $3, $4, $5, $6)`, orgID, c.ID, c.secretHash, name, redirects, method)
		return err
	})
	return c, secret, err
}

func (s *Store) Client(ctx context.Context, orgID, id string) (*Client, error) {
	c := &Client{}
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT client_id, name, redirect_uris, auth_method, secret_hash FROM oauth_clients WHERE client_id = $1`, id).
			Scan(&c.ID, &c.Name, &c.RedirectURIs, &c.AuthMethod, &c.secretHash)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalid
	}
	return c, err
}

// CheckSecret verifies a confidential client's secret; public clients pass with an empty secret.
func (c *Client) CheckSecret(secret string) bool {
	if c.AuthMethod == "none" {
		return true
	}
	return subtle.ConstantTimeCompare(Hash(secret), c.secretHash) == 1
}

type Request struct {
	ID, ClientID, RedirectURI, State, CodeChallenge, Scope, Resource string
}

func (s *Store) SaveRequest(ctx context.Context, orgID string, r *Request) error {
	return s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_requests WHERE expires_at < now()`); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('oauth:' || $1, 0)), (SELECT count(*) FROM oauth_requests)`, orgID).Scan(nil, &n); err != nil {
			return err
		}
		if n >= oauthRequestsPerOrg {
			return ErrBusy
		}
		return tx.QueryRow(ctx, `INSERT INTO oauth_requests (org_id, client_id, redirect_uri, state, code_challenge, scope, resource, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, orgID, r.ClientID, r.RedirectURI, r.State, r.CodeChallenge, r.Scope, r.Resource,
			time.Now().Add(OAuthRequestTTL)).Scan(&r.ID)
	})
}

func (s *Store) GetRequest(ctx context.Context, orgID, id string) (*Request, error) {
	r := &Request{ID: id}
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT client_id, redirect_uri, state, code_challenge, scope, resource FROM oauth_requests
			WHERE id::text = $1 AND expires_at > now()`, id).Scan(&r.ClientID, &r.RedirectURI, &r.State, &r.CodeChallenge, &r.Scope, &r.Resource)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalid
	}
	return r, err
}

func (s *Store) DeleteRequest(ctx context.Context, orgID, id string) error {
	return s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM oauth_requests WHERE id::text = $1`, id)
		return err
	})
}

func (s *Store) IssueCode(ctx context.Context, orgID string, r *Request, memberID string) (string, error) {
	code := NewToken("")
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO oauth_codes (code_hash, org_id, client_id, member_id, redirect_uri, code_challenge, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, Hash(code), orgID, r.ClientID, memberID, r.RedirectURI, r.CodeChallenge, time.Now().Add(CodeTTL))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM oauth_requests WHERE id::text = $1`, r.ID)
		return err
	})
	return code, err
}

// VerifyPKCE checks an S256 code_verifier against its challenge.
func VerifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	h := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(h[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

type Tokens struct {
	Access, Refresh string
	ExpiresIn       int
	MemberID        string
}

// ExchangeCode redeems a single-use code. Replaying a used code revokes the tokens it minted.
func (s *Store) ExchangeCode(ctx context.Context, orgID, clientID, code, redirectURI, verifier string) (*Tokens, error) {
	var t *Tokens
	var replayed struct{ member, client string }
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		var member, redirect, challenge, cid string
		var used *time.Time
		var expires time.Time
		err := tx.QueryRow(ctx, `SELECT client_id, member_id::text, redirect_uri, code_challenge, used_at, expires_at FROM oauth_codes
			WHERE code_hash = $1 FOR UPDATE`, Hash(code)).Scan(&cid, &member, &redirect, &challenge, &used, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		if cid != clientID {
			return ErrInvalid
		}
		if used != nil {
			replayed.member, replayed.client = member, cid
			return ErrInvalid
		}
		if redirect != redirectURI || time.Now().After(expires) || !VerifyPKCE(verifier, challenge) {
			return ErrInvalid
		}
		if _, err := tx.Exec(ctx, `UPDATE oauth_codes SET used_at = now() WHERE code_hash = $1`, Hash(code)); err != nil {
			return err
		}
		t, err = mint(ctx, tx, orgID, clientID, member)
		return err
	})
	// The error rolls the exchange back, so the revocation commits on its own.
	if replayed.member != "" {
		if rerr := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE oauth_tokens SET revoked_at = now() WHERE member_id = $1 AND client_id = $2 AND revoked_at IS NULL`,
				replayed.member, replayed.client)
			return err
		}); rerr != nil {
			return nil, rerr
		}
	}
	return t, err
}

func mint(ctx context.Context, tx pgx.Tx, orgID, clientID, memberID string) (*Tokens, error) {
	t := &Tokens{Access: NewToken("hat_"), Refresh: NewToken("hrt_"), ExpiresIn: int(AccessTTL.Seconds()), MemberID: memberID}
	for _, x := range []struct {
		kind, tok string
		ttl       time.Duration
	}{{"access", t.Access, AccessTTL}, {"refresh", t.Refresh, RefreshTTL}} {
		if _, err := tx.Exec(ctx, `INSERT INTO oauth_tokens (token_hash, org_id, kind, client_id, member_id, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			Hash(x.tok), orgID, x.kind, clientID, memberID, time.Now().Add(x.ttl)); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// Refresh rotates a refresh token: the old one stops working.
func (s *Store) Refresh(ctx context.Context, orgID, clientID, refresh string) (*Tokens, error) {
	var t *Tokens
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		var member string
		err := tx.QueryRow(ctx, `UPDATE oauth_tokens SET revoked_at = now() WHERE token_hash = $1 AND kind = 'refresh' AND client_id = $2
			AND revoked_at IS NULL AND expires_at > now() RETURNING member_id::text`, Hash(refresh), clientID).Scan(&member)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		t, err = mint(ctx, tx, orgID, clientID, member)
		return err
	})
	return t, err
}

func (s *Store) AccessTokenMember(ctx context.Context, orgID, tok string) (string, time.Time, error) {
	var id string
	var exp time.Time
	err := s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT member_id::text, expires_at FROM oauth_tokens WHERE token_hash = $1 AND kind = 'access'
			AND revoked_at IS NULL AND expires_at > now()`, Hash(tok)).Scan(&id, &exp)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", exp, ErrInvalid
	}
	return id, exp, err
}

func (s *Store) Revoke(ctx context.Context, orgID, tok string) error {
	return s.DB.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE oauth_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, Hash(tok))
		return err
	})
}
