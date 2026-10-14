package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
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
	desc:   "Show pictures under your message: give the pages you found (search results about a product, place, costume…) and each page's own main picture is shown with a link to it. Use it when seeing helps choose.",
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
					out[i] = imageOut{Src: src, Page: page.String(), Host: strings.TrimPrefix(page.Hostname(), "www."), Caption: clip(strings.TrimSpace(p.Caption), 80)}
				}
			}()
		}
		wg.Wait()
		var found []imageOut
		for _, o := range out {
			if o.Src != "" {
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

// imageFromHTML picks og:image (or twitter:image) from a page's meta tags, made absolute, https only.
func imageFromHTML(html string, page *url.URL) string {
	best := ""
tags:
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
		switch key {
		case "og:image", "og:image:secure_url":
			best = content
			break tags // the page's own choice
		case "og:image:url", "twitter:image", "twitter:image:src":
			if best == "" {
				best = content
			}
		}
	}
	if best == "" {
		return ""
	}
	u, err := page.Parse(strings.ReplaceAll(strings.TrimSpace(best), "&amp;", "&"))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return u.String()
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
	cache map[string]*geo
}{cache: map[string]*geo{}}

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
	if g, ok := geocoder.cache[key]; ok {
		return g, nil
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
		if err1 == nil && err2 == nil {
			addr, _ := rows[0]["display_name"].(string)
			g = &geo{lat: lat, lng: lng, address: addr}
		}
	}
	geocoder.cache[key] = g
	return g, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
