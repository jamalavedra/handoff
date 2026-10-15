package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Blocks are rich pieces the agent attaches to its message: images, a map. Each kind is a plugin: a
// tool the model calls with what it wants to show, and a resolver that turns that into facts the
// server checked (an image found on the page it cites, coordinates from a geocoder), never what the
// model made up. The web app has a renderer per kind and skips kinds it doesn't know. Adding a kind
// is one blockPlugin here and one renderer in web/src/blocks.

type Block struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type blockPlugin struct {
	tool    string // what the model calls
	kind    string // Block.Type
	desc    string
	schema  any
	resolve func(ctx context.Context, s *Service, raw json.RawMessage) (data any, summary string, err error)
}

var blockPlugins = []blockPlugin{imagesPlugin, mapPlugin}

var blockToolDefs = func() []map[string]any {
	var defs []map[string]any
	for _, b := range blockPlugins {
		defs = append(defs, map[string]any{"type": "function", "function": map[string]any{"name": b.tool, "description": b.desc, "parameters": b.schema}})
	}
	return defs
}()

const maxBlocks = 4

// showBlock runs a block tool for the model: the block goes under its message.
func (a *assistantRun) showBlock(ctx context.Context, b blockPlugin, args string) string {
	raw := json.RawMessage(args)
	if strings.TrimSpace(args) == "" {
		raw = json.RawMessage("{}")
	}
	step := AssistantStep{Type: "tool", Tool: b.tool}
	if len(a.out.Blocks) >= maxBlocks {
		step.Summary, step.Error = "Didn't add another "+b.kind, "too many blocks"
		a.add(ctx, step)
		return `{"error":"this message already has enough images and maps"}`
	}
	if b.tool == imagesPlugin.tool {
		if raw = a.onlyFoundPages(raw); raw == nil {
			step.Summary, step.Error = "Didn't show pictures", "only pages your lookups returned this turn"
			a.add(ctx, step)
			return `{"error":"show_images only takes pages that a lookup returned in this turn"}`
		}
	}
	rctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	data, summary, err := b.resolve(rctx, a.s, raw)
	step.Summary = summary
	if err != nil {
		step.Error = clip(err.Error(), 200)
		if step.Summary == "" {
			step.Summary = "Couldn't show " + b.kind
		}
		a.add(ctx, step)
		e, _ := json.Marshal(map[string]string{"error": step.Error})
		return string(e)
	}
	d, _ := json.Marshal(data)
	a.out.Blocks = append(a.out.Blocks, Block{Type: b.kind, Data: d})
	a.add(ctx, step)
	return `{"ok":true,"shown":"under your message; don't repeat it as text, refer to it"}`
}

var pageURL = regexp.MustCompile(`https?://[^\s"'<>\\\]]+`)

// normPage trims what ends a sentence around a URL: punctuation, and a ")" with no "(" in the URL,
// so "https://en.wikipedia.org/wiki/Paris_(film)" keeps its own.
func normPage(u string) string {
	u = strings.TrimSpace(u)
	for {
		t := strings.TrimRight(u, "/.,;:!?")
		if strings.HasSuffix(t, ")") && strings.Count(t, "(") < strings.Count(t, ")") {
			t = t[:len(t)-1]
		}
		if t == u {
			return u
		}
		u = t
	}
}

// foundPages remembers the pages a lookup returned, but not ones it was asked about: a search
// that echoes its query would otherwise let the model vouch for a URL of its own.
func (a *assistantRun) foundPages(text, args string) {
	if a.turn.found == nil {
		a.turn.found = map[string]bool{}
	}
	asked := map[string]bool{}
	for _, u := range pageURL.FindAllString(args, 50) {
		asked[normPage(u)] = true
	}
	for _, u := range pageURL.FindAllString(text, 500) {
		if u = normPage(u); !asked[u] {
			a.turn.found[u] = true
		}
	}
}

// onlyFoundPages drops pages no lookup returned this turn, or returns nil if none is left. Text in
// a task or chat can't make the agent fetch a page of its choosing, with private data in its URL.
func (a *assistantRun) onlyFoundPages(raw json.RawMessage) json.RawMessage {
	var in imagesIn
	if json.Unmarshal(raw, &in) != nil {
		return raw
	}
	pages := in.Pages[:0]
	for _, p := range in.Pages {
		if a.turn.found[normPage(p.URL)] {
			pages = append(pages, p)
		}
	}
	if len(pages) == 0 {
		return nil
	}
	in.Pages = pages
	out, _ := json.Marshal(in)
	return out
}

// ---- images: pictures from the pages it cites ----

type imagesIn struct {
	Pages []struct {
		URL     string `json:"url" jsonschema:"a page you found (a search result), not an image URL"`
		Caption string `json:"caption,omitempty" jsonschema:"a few words on what it shows"`
	} `json:"pages" jsonschema:"1 to 6 pages whose main picture to show"`
}

type imageOut struct {
	Src     string `json:"src"`
	Page    string `json:"page"`
	Host    string `json:"host"`
	Caption string `json:"caption,omitempty"`
}

var imagesPlugin = blockPlugin{
	tool:   "show_images",
	kind:   "images",
	desc:   "Show pictures under your message: give pages you found, one per item, each a page about that one item (a product page, a place's page), and each page's own main picture is shown with a link to it. A list article has one picture for the whole list, so don't use it for several items. Use it when seeing helps choose.",
	schema: schemaOf[imagesIn](),
	resolve: func(ctx context.Context, s *Service, raw json.RawMessage) (any, string, error) {
		var in imagesIn
		if err := json.Unmarshal(raw, &in); err != nil || len(in.Pages) == 0 {
			return nil, "", errors.New("give 1 to 6 pages")
		}
		if len(in.Pages) > 6 {
			in.Pages = in.Pages[:6]
		}
		out := make([]imageOut, len(in.Pages))
		var wg sync.WaitGroup
		for i, p := range in.Pages {
			wg.Add(1)
			go func() {
				defer wg.Done()
				page, err := url.Parse(strings.TrimSpace(p.URL))
				if err != nil || (page.Scheme != "https" && page.Scheme != "http") || page.Host == "" {
					return
				}
				if src := s.pageImage(ctx, page); src != "" {
					out[i] = imageOut{Src: s.imageProxy(src), Page: page.String(), Host: strings.TrimPrefix(page.Hostname(), "www."), Caption: clip(strings.TrimSpace(p.Caption), 80)}
				}
			}()
		}
		wg.Wait()
		// One picture once: two captions on the same page's picture would mislabel one of them.
		var found []imageOut
		for _, o := range out {
			if o.Src != "" && !slices.ContainsFunc(found, func(f imageOut) bool { return f.Src == o.Src || f.Page == o.Page }) {
				found = append(found, o)
			}
		}
		if len(found) == 0 {
			return nil, "Looked for pictures", errors.New("none of those pages has a picture to show")
		}
		return map[string]any{"images": found}, fmt.Sprintf("Showed %d %s", len(found), plural(len(found), "picture", "pictures")), nil
	},
}

var (
	metaTag  = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	metaAttr = regexp.MustCompile(`(?is)(property|name|content)\s*=\s*("[^"]*"|'[^']*')`)
)

// pageImage finds the picture a page names for itself (og:image, twitter:image) in its first 512KB.
func (s *Service) pageImage(ctx context.Context, page *url.URL) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page.String(), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Handoff/1; +https://github.com/joalavedra/handoff)")
	resp, err := s.outbound().Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "html") {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	return imageFromHTML(string(b), page)
}

// imageFromHTML picks the page's picture from its meta tags: the first usable (absolute, https) of
// og:image:secure_url, og:image, og:image:url, twitter:image.
func imageFromHTML(html string, page *url.URL) string {
	rank := map[string]int{"og:image:secure_url": 0, "og:image": 1, "og:image:url": 2, "twitter:image": 3, "twitter:image:src": 3}
	var found [4][]string
	for _, tag := range metaTag.FindAllString(html, 200) {
		var key, content string
		for _, m := range metaAttr.FindAllStringSubmatch(tag, -1) {
			v := strings.Trim(m[2], `"'`)
			if strings.EqualFold(m[1], "content") {
				content = v
			} else {
				key = strings.ToLower(v)
			}
		}
		if content == "" {
			continue
		}
		if r, ok := rank[key]; ok {
			found[r] = append(found[r], content)
		}
	}
	for _, cands := range found {
		for _, c := range cands {
			u, err := page.Parse(strings.ReplaceAll(strings.TrimSpace(c), "&amp;", "&"))
			if err == nil && u.Scheme == "https" && u.Host != "" {
				return u.String()
			}
		}
	}
	return ""
}

// ---- map: places, located by a geocoder ----

type mapIn struct {
	Places []struct {
		Name  string `json:"name"`
		Where string `json:"where" jsonschema:"address or name and city to look it up, e.g. 'One Stop Party Shop, San Francisco'"`
		URL   string `json:"url,omitempty" jsonschema:"its website or listing, if you found one"`
		Note  string `json:"note,omitempty" jsonschema:"a few words: why it's on the list"`
	} `json:"places" jsonschema:"1 to 8 places to put on a map"`
}

type placeOut struct {
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Address string  `json:"address,omitempty"`
	URL     string  `json:"url,omitempty"`
	Note    string  `json:"note,omitempty"`
}

var mapPlugin = blockPlugin{
	tool:   "show_map",
	kind:   "map",
	desc:   "Show places on a map under your message (shops, restaurants, venues): each is looked up by its address or name and city, so give real places you found. Use it when where things are helps choose.",
	schema: schemaOf[mapIn](),
	resolve: func(ctx context.Context, s *Service, raw json.RawMessage) (any, string, error) {
		var in mapIn
		if err := json.Unmarshal(raw, &in); err != nil || len(in.Places) == 0 {
			return nil, "", errors.New("give 1 to 8 places")
		}
		if len(in.Places) > 8 {
			in.Places = in.Places[:8]
		}
		var found []placeOut
		for _, p := range in.Places {
			where := strings.TrimSpace(p.Where)
			if where == "" {
				where = strings.TrimSpace(p.Name)
			}
			g, err := s.geocode(ctx, where)
			if err != nil || g == nil {
				continue
			}
			o := placeOut{Name: clip(strings.TrimSpace(p.Name), 80), Lat: g.lat, Lng: g.lng, Address: clip(g.address, 160), Note: clip(strings.TrimSpace(p.Note), 120)}
			if u, err := url.Parse(strings.TrimSpace(p.URL)); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
				o.URL = u.String()
			}
			if o.Name == "" {
				o.Name = clip(where, 80)
			}
			found = append(found, o)
		}
		if len(found) == 0 {
			return nil, "Looked up places", errors.New("couldn't find any of those places; give addresses or name and city")
		}
		return map[string]any{"places": found}, fmt.Sprintf("Mapped %d %s", len(found), plural(len(found), "place", "places")), nil
	},
}

type geo struct {
	lat, lng float64
	address  string
}

// The geocoder is OpenStreetMap's Nominatim by default (HANDOFF_GEOCODER_URL to use another
// Nominatim-compatible one). Its policy asks for at most one request a second and a real User-Agent;
// answers are kept for the life of the process.
var geocoder = struct {
	sync.Mutex
	last  time.Time
	cache map[string]geoHit
}{cache: map[string]geoHit{}}

// A place found is kept; one not found is asked again after a while (the index changes).
type geoHit struct {
	g  *geo
	at time.Time
}

const geoMissFor = 10 * time.Minute

func geocoderURL() string {
	if u := os.Getenv("HANDOFF_GEOCODER_URL"); u != "" {
		return u
	}
	return "https://nominatim.openstreetmap.org/search"
}

func (s *Service) geocode(ctx context.Context, q string) (*geo, error) {
	key := strings.ToLower(q)
	geocoder.Lock()
	defer geocoder.Unlock()
	if h, ok := geocoder.cache[key]; ok && (h.g != nil || time.Since(h.at) < geoMissFor) {
		return h.g, nil
	}
	if wait := time.Second - time.Since(geocoder.last); wait > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	geocoder.last = time.Now()
	u := geocoderURL() + "?" + url.Values{"q": {q}, "format": {"jsonv2"}, "limit": {"1"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Handoff/1 (+https://github.com/joalavedra/handoff)")
	req.Header.Set("Accept", "application/json")
	resp, err := s.outbound().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoder answered %d", resp.StatusCode)
	}
	var rows []map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rows); err != nil {
		return nil, err
	}
	var g *geo
	if len(rows) > 0 {
		lat, err1 := strconv.ParseFloat(fmt.Sprint(rows[0]["lat"]), 64)
		lng, err2 := strconv.ParseFloat(fmt.Sprint(rows[0]["lon"]), 64)
		if err1 == nil && err2 == nil && lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180 {
			addr, _ := rows[0]["display_name"].(string)
			g = &geo{lat: lat, lng: lng, address: addr}
		}
	}
	geocoder.cache[key] = geoHit{g: g, at: time.Now()}
	return g, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// ---- the image proxy ----

// Pictures are served through this server, so people viewing a chat never contact the sites they
// come from. Only URLs a block put there are served: each carries a signature of its address.

func (s *Service) imageKey() []byte {
	k := sha256.Sum256(append([]byte("handoff block image\x00"), s.Cfg.Secret...))
	return k[:]
}

func (s *Service) imageSig(src string) string {
	m := hmac.New(sha256.New, s.imageKey())
	m.Write([]byte(src))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Service) imageProxy(src string) string {
	return "/api/image?" + url.Values{"u": {src}, "s": {s.imageSig(src)}}.Encode()
}

const maxProxiedImage = 8 << 20

// ProxyImage fetches a signed picture for a viewer: images only, at most 8MB.
func (s *Service) ProxyImage(ctx context.Context, src, sig string) (string, []byte, error) {
	if !hmac.Equal([]byte(sig), []byte(s.imageSig(src))) {
		return "", nil, errf(ErrNotFound, "no such image")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", nil, errf(ErrNotFound, "no such image")
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Handoff/1; +https://github.com/joalavedra/handoff)")
	resp, err := s.outbound().Do(req)
	if err != nil {
		return "", nil, errf(ErrNotFound, "the image couldn't be loaded")
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") || strings.Contains(ct, "svg") {
		return "", nil, errf(ErrNotFound, "the image couldn't be loaded")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxProxiedImage+1))
	if err != nil || len(b) > maxProxiedImage {
		return "", nil, errf(ErrNotFound, "the image is too big")
	}
	return ct, b, nil
}
