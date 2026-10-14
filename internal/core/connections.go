package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Connections: remote MCP servers the workspace's own agent can use while it works (triage, chat
// replies, the assistant, taking tasks on). An admin adds a server by URL; Handoff lists its tools,
// and the ones switched on join the agent's toolset. This first version is for looking things up:
// tools a server marks destructive can't be switched on, and every call is recorded as a step.

type ConnectionToolOut struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ReadOnly    bool   `json:"read_only"`
	Destructive bool   `json:"destructive,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type ConnectionOut struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	URL        string              `json:"url"`
	Auth       string              `json:"auth"`
	HeaderName string              `json:"header_name,omitempty"`
	HasSecret  bool                `json:"has_secret"`
	Tools      []ConnectionToolOut `json:"tools"`
	LastError  string              `json:"last_error,omitempty"`
	UpdatedAt  string              `json:"updated_at"`
}

type ConnectionsOut struct {
	Connections []ConnectionOut `json:"connections"`
}

type AddConnectionIn struct {
	Name       string `json:"name" jsonschema:"what to call it, e.g. Web search"`
	URL        string `json:"url" jsonschema:"the server's streamable HTTP endpoint, e.g. https://search.parallel.ai/mcp"`
	Auth       string `json:"auth,omitempty" jsonschema:"none (default), bearer (Authorization: Bearer <secret>) or header (<header_name>: <secret>)"`
	HeaderName string `json:"header_name,omitempty" jsonschema:"with auth=header, e.g. x-api-key"`
	Secret     string `json:"secret,omitempty" jsonschema:"API key or token; stored encrypted and never returned"`
}

type ConnectionIDIn struct {
	ID string `json:"id"`
}

type SetConnectionToolsIn struct {
	ID      string   `json:"id"`
	Enabled []string `json:"enabled" jsonschema:"names of the tools the agent may use"`
}

// connTool is a server's tool as cached: what the agent needs to offer and call it.
type connTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	ReadOnly    bool            `json:"read_only"`
	Destructive bool            `json:"destructive,omitempty"`
}

var headerName = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

func (s *Service) ListConnections(ctx context.Context, p *Principal, _ struct{}) (ConnectionsOut, error) {
	out := ConnectionsOut{Connections: []ConnectionOut{}}
	err := s.run(ctx, p, func(c *call) error {
		if err := p.requireAdmin(); err != nil {
			return err
		}
		var err error
		out.Connections, err = c.connections()
		return err
	})
	return out, err
}

func (c *call) connections() ([]ConnectionOut, error) {
	rows, err := c.tx.Query(c.ctx, `SELECT id::text, name, url, auth, header_name, secret IS NOT NULL, tools, enabled, last_error, updated_at
		FROM mcp_connections ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConnectionOut{}
	for rows.Next() {
		var o ConnectionOut
		var tools []byte
		var enabled []string
		var updated time.Time
		if err := rows.Scan(&o.ID, &o.Name, &o.URL, &o.Auth, &o.HeaderName, &o.HasSecret, &tools, &enabled, &o.LastError, &updated); err != nil {
			return nil, err
		}
		var list []connTool
		_ = json.Unmarshal(tools, &list)
		o.Tools = []ConnectionToolOut{}
		for _, t := range list {
			o.Tools = append(o.Tools, ConnectionToolOut{Name: t.Name, Description: clip(t.Description, 300), ReadOnly: t.ReadOnly, Destructive: t.Destructive, Enabled: slices.Contains(enabled, t.Name)})
		}
		o.UpdatedAt = ts(&updated)
		out = append(out, o)
	}
	return out, rows.Err()
}

// AddConnection checks that the server answers, lists its tools and switches on the read-only ones.
func (s *Service) AddConnection(ctx context.Context, p *Principal, in AddConnectionIn) (ConnectionOut, error) {
	if err := p.requireAdmin(); err != nil {
		return ConnectionOut{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 60 {
		return ConnectionOut{}, errf(ErrInvalid, "name must be 1-60 characters")
	}
	u, err := triageBaseURL(in.URL)
	if err != nil {
		return ConnectionOut{}, errf(ErrInvalid, "url must be an http(s) URL without credentials in it")
	}
	auth := strings.TrimSpace(in.Auth)
	if auth == "" {
		auth = "none"
	}
	hdr := strings.TrimSpace(in.HeaderName)
	switch auth {
	case "none":
	case "bearer":
		hdr = ""
	case "header":
		if !headerName.MatchString(hdr) || strings.EqualFold(hdr, "host") || strings.EqualFold(hdr, "cookie") {
			return ConnectionOut{}, errf(ErrInvalid, "header_name must be a header like x-api-key")
		}
	default:
		return ConnectionOut{}, errf(ErrInvalid, "auth must be none, bearer or header")
	}
	secret := strings.TrimSpace(in.Secret)
	if auth != "none" && secret == "" {
		return ConnectionOut{}, errf(ErrInvalid, "this server needs an API key or token")
	}
	srv := connServer{url: u, auth: auth, header: hdr, secret: secret}
	tools, err := s.listServerTools(ctx, srv)
	if err != nil {
		return ConnectionOut{}, errf(ErrInvalid, "couldn't connect to %s: %s", u, clip(err.Error(), 200))
	}
	return s.saveConnection(ctx, p, name, u, auth, hdr, secret, tools)
}

// RefreshConnection lists the server's tools again (they change as servers update).
func (s *Service) RefreshConnection(ctx context.Context, p *Principal, in ConnectionIDIn) (ConnectionOut, error) {
	if err := p.requireAdmin(); err != nil {
		return ConnectionOut{}, err
	}
	srv, err := s.connServer(ctx, p.Org.ID, in.ID)
	if err != nil {
		return ConnectionOut{}, err
	}
	tools, lerr := s.listServerTools(ctx, srv)
	var out ConnectionOut
	err = s.run(ctx, p, func(c *call) error {
		if lerr != nil {
			_, err := c.tx.Exec(c.ctx, `UPDATE mcp_connections SET last_error = $2, updated_at = now() WHERE id::text = $1`, in.ID, clip(lerr.Error(), 300))
			if err != nil {
				return err
			}
		} else {
			b, _ := json.Marshal(tools)
			// Tools that disappeared, or are now marked destructive, are switched off.
			var enabled []string
			if err := c.tx.QueryRow(c.ctx, `SELECT enabled FROM mcp_connections WHERE id::text = $1`, in.ID).Scan(&enabled); err != nil {
				return err
			}
			keep := []string{}
			for _, t := range tools {
				if slices.Contains(enabled, t.Name) && !t.Destructive {
					keep = append(keep, t.Name)
				}
			}
			if _, err := c.tx.Exec(c.ctx, `UPDATE mcp_connections SET tools = $2, enabled = $3, last_error = '', updated_at = now() WHERE id::text = $1`, in.ID, b, keep); err != nil {
				return err
			}
		}
		all, err := c.connections()
		for _, o := range all {
			if o.ID == in.ID {
				out = o
			}
		}
		return err
	})
	return out, err
}

func (s *Service) SetConnectionTools(ctx context.Context, p *Principal, in SetConnectionToolsIn) (ConnectionOut, error) {
	var out ConnectionOut
	err := s.run(ctx, p, func(c *call) error {
		if err := p.requireAdmin(); err != nil {
			return err
		}
		var tools []byte
		err := c.tx.QueryRow(c.ctx, `SELECT tools FROM mcp_connections WHERE id::text = $1 FOR UPDATE`, in.ID).Scan(&tools)
		if errors.Is(err, pgx.ErrNoRows) {
			return errf(ErrNotFound, "no connection %q", in.ID)
		}
		if err != nil {
			return err
		}
		var ts []connTool
		_ = json.Unmarshal(tools, &ts)
		on := []string{}
		for _, name := range in.Enabled {
			i := slices.IndexFunc(ts, func(t connTool) bool { return t.Name == name })
			if i < 0 {
				return errf(ErrInvalid, "the server has no tool %q", name)
			}
			if ts[i].Destructive {
				return errf(ErrInvalid, "%s is marked as making destructive changes; it can't be switched on yet", name)
			}
			on = append(on, name)
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE mcp_connections SET enabled = $2, updated_at = now() WHERE id::text = $1`, in.ID, on); err != nil {
			return err
		}
		all, err := c.connections()
		for _, o := range all {
			if o.ID == in.ID {
				out = o
			}
		}
		return err
	})
	return out, err
}

func (s *Service) RemoveConnection(ctx context.Context, p *Principal, in ConnectionIDIn) (ConnectionsOut, error) {
	out := ConnectionsOut{Connections: []ConnectionOut{}}
	err := s.run(ctx, p, func(c *call) error {
		if err := p.requireAdmin(); err != nil {
			return err
		}
		var name string
		err := c.tx.QueryRow(c.ctx, `DELETE FROM mcp_connections WHERE id::text = $1 RETURNING name`, in.ID).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return errf(ErrNotFound, "no connection %q", in.ID)
		}
		if err != nil {
			return err
		}
		if err := c.event("", "connection_removed", map[string]any{"name": name}); err != nil {
			return err
		}
		out.Connections, err = c.connections()
		return err
	})
	return out, err
}

// ---- talking to servers ----

type connServer struct {
	id, name, url, auth, header, secret string
}

func (s *Service) connServer(ctx context.Context, org, id string) (connServer, error) {
	srv := connServer{id: id}
	var sealed []byte
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT name, url, auth, header_name, secret FROM mcp_connections WHERE id::text = $1`, id).
			Scan(&srv.name, &srv.url, &srv.auth, &srv.header, &sealed)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return srv, errf(ErrNotFound, "no connection %q", id)
	}
	if err != nil {
		return srv, err
	}
	if len(sealed) > 0 && srv.auth == "oauth" {
		if srv.secret, err = s.freshAccess(ctx, org, id, sealed); err != nil {
			return srv, errf(ErrInvalid, "%s: %s", srv.name, clip(err.Error(), 200))
		}
	} else if len(sealed) > 0 {
		b, err := s.unseal(sealed)
		if err != nil {
			return srv, errf(ErrInvalid, "the saved key for %s can't be read; remove and add the connection again", srv.name)
		}
		srv.secret = string(b)
	}
	return srv, nil
}

// authTransport adds a connection's credential to every request, through the guarded outbound client
// (no redirects, no private addresses on multi-workspace servers).
type authTransport struct {
	base         http.RoundTripper
	header, val  string
	bearer, none bool
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	switch {
	case t.bearer:
		r.Header.Set("Authorization", "Bearer "+t.val)
	case !t.none:
		r.Header.Set(t.header, t.val)
	}
	return t.base.RoundTrip(r)
}

func (s *Service) openServer(ctx context.Context, srv connServer) (*mcp.ClientSession, error) {
	base := s.outbound()
	rt := base.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	hc := &http.Client{
		Timeout:       base.Timeout,
		CheckRedirect: base.CheckRedirect,
		Transport:     authTransport{base: rt, header: srv.header, val: srv.secret, bearer: srv.auth == "bearer" || srv.auth == "oauth", none: srv.auth == "none"},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "handoff", Version: "1"}, nil)
	return client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.url, HTTPClient: hc, MaxRetries: -1}, nil)
}

func (s *Service) listServerTools(ctx context.Context, srv connServer) ([]connTool, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cs, err := s.openServer(ctx, srv)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	var out []connTool
	var cursor string
	for page := 0; page < 10; page++ {
		res, err := cs.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, t := range res.Tools {
			if t == nil || t.Name == "" {
				continue
			}
			ct := connTool{Name: t.Name, Description: clip(t.Description, 1000)}
			if t.InputSchema != nil {
				ct.Schema, _ = json.Marshal(t.InputSchema)
			}
			if a := t.Annotations; a != nil {
				ct.ReadOnly = a.ReadOnlyHint
				ct.Destructive = a.DestructiveHint != nil && *a.DestructiveHint
			}
			out = append(out, ct)
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}

// ---- in the agent's hands ----

// extTool is a switched-on tool from a connection, as the model sees it.
type extTool struct {
	fn         string // the function name offered to the model
	conn, tool string
	desc       string
	schema     json.RawMessage
	server     connServer
}

var fnUnsafe = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// externalTools lists the switched-on tools of every connection (at most 24 in all).
func (s *Service) externalTools(ctx context.Context, org string) ([]extTool, error) {
	type row struct {
		id, name string
		tools    []byte
		enabled  []string
	}
	var rows []row
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		rs, err := tx.Query(ctx, `SELECT id::text, name, tools, enabled FROM mcp_connections WHERE cardinality(enabled) > 0 ORDER BY created_at`)
		if err != nil {
			return err
		}
		rows, err = pgx.CollectRows(rs, func(r pgx.CollectableRow) (row, error) {
			var x row
			return x, r.Scan(&x.id, &x.name, &x.tools, &x.enabled)
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	var out []extTool
	for _, r := range rows {
		srv, err := s.connServer(ctx, org, r.id)
		if err != nil {
			continue
		}
		var ts []connTool
		_ = json.Unmarshal(r.tools, &ts)
		prefix := strings.ToLower(strings.Trim(fnUnsafe.ReplaceAllString(r.name, "_"), "_"))
		if len(prefix) > 16 {
			prefix = prefix[:16]
		}
		for _, t := range ts {
			if !slices.Contains(r.enabled, t.Name) || t.Destructive || len(out) >= 24 {
				continue
			}
			fn := "ext_" + prefix + "__" + strings.Trim(fnUnsafe.ReplaceAllString(t.Name, "_"), "_")
			if len(fn) > 64 {
				fn = fn[:64]
			}
			schema := t.Schema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			out = append(out, extTool{fn: fn, conn: r.name, tool: t.Name, desc: clip(t.Description, 800), schema: schema, server: srv})
		}
	}
	return out, nil
}

func extToolDefs(ts []extTool) []map[string]any {
	var defs []map[string]any
	for _, t := range ts {
		desc := fmt.Sprintf("[%s] %s", t.conn, t.desc)
		defs = append(defs, map[string]any{"type": "function", "function": map[string]any{"name": t.fn, "description": desc, "parameters": t.schema}})
	}
	return defs
}

// extSessions keeps one session per connection for the length of a turn.
type extSessions map[string]*mcp.ClientSession

func (e extSessions) close() {
	for _, cs := range e {
		_ = cs.Close()
	}
}

// callExternal runs a connection's tool and returns its text for the model, clipped. What comes back
// is content from the outside world: the prompts tell the model to treat it as information only.
func (s *Service) callExternal(ctx context.Context, sessions extSessions, t extTool, args json.RawMessage) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cs := sessions[t.server.id]
	if cs == nil {
		var err error
		if cs, err = s.openServer(ctx, t.server); err != nil {
			return "", fmt.Errorf("couldn't reach %s: %s", t.conn, clip(err.Error(), 200))
		}
		sessions[t.server.id] = cs
	}
	var a map[string]any
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &a); err != nil {
			return "", errf(ErrInvalid, "arguments must be a JSON object")
		}
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: t.tool, Arguments: a})
	if err != nil {
		return "", fmt.Errorf("%s: %s", t.conn, clip(err.Error(), 200))
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
			b.WriteString("\n")
		}
	}
	text := strings.TrimSpace(b.String())
	if text == "" && res.StructuredContent != nil {
		sb, _ := json.Marshal(res.StructuredContent)
		text = string(sb)
	}
	if res.IsError {
		return "", fmt.Errorf("%s: %s", t.conn, clip(text, 300))
	}
	return clipBytes(text, 12000), nil
}
