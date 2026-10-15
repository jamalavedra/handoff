package core_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/joalavedra/handoff/internal/core"
)

// The agent shows a page's picture and a map of places; the server finds the picture on the page and
// the coordinates with the geocoder, and refuses what it can't verify.
func TestAssistantBlocks(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/dolly" {
			fmt.Fprint(w, `<html><head><meta property="og:image" content="https://cdn.example/dolly.jpg"></head></html>`)
			return
		}
		fmt.Fprint(w, `<html><head><title>no picture</title></head></html>`)
	}))
	t.Cleanup(site.Close)
	geo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "One Stop Party Shop, San Francisco" {
			fmt.Fprint(w, `[{"lat":"37.7726","lon":"-122.4313","display_name":"One Stop Party Shop, Haight Street, San Francisco"}]`)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	t.Cleanup(geo.Close)
	t.Setenv("HANDOFF_GEOCODER_URL", geo.URL)

	model, _ := scriptedModel(t,
		calls(
			toolCall("c1", "show_images", map[string]any{"pages": []map[string]any{{"url": site.URL + "/dolly", "caption": "Dolly Parton"}, {"url": site.URL + "/plain"}}}),
			toolCall("c2", "show_map", map[string]any{"places": []map[string]any{{"name": "One Stop Party Shop", "where": "One Stop Party Shop, San Francisco", "note": "great accessories"}, {"name": "Nowhere", "where": "zzz unknown"}}}),
			toolCall("c3", "show_map", map[string]any{"places": []map[string]any{{"name": "Nowhere", "where": "zzz unknown"}}}),
		),
		say("Here are some options."),
	)
	e, admin, _ := assistantEnv(t, model, "blocks")
	out, err := e.s.AssistantChat(e.ctx, admin, ask("costume ideas and shops in SF?"))
	if err != nil || out.Error != "" {
		t.Fatal(err, out.Error)
	}
	// No lookup returned those pages this turn, so the agent can't fetch them (or leak data in their URLs).
	if len(out.Blocks) != 1 || out.Blocks[0].Type != "map" {
		t.Fatalf("blocks: %+v", out.Blocks)
	}
	if i := slices.IndexFunc(out.Steps, func(s core.AssistantStep) bool { return s.Tool == "show_images" }); i < 0 || out.Steps[i].Error == "" {
		t.Fatalf("show_images of pages no lookup returned wasn't refused: %+v", out.Steps)
	}
	var places struct {
		Places []struct {
			Name     string
			Lat, Lng float64
			Note     string
		}
	}
	_ = json.Unmarshal(out.Blocks[0].Data, &places)
	if len(places.Places) != 1 || places.Places[0].Lat != 37.7726 || places.Places[0].Note != "great accessories" {
		t.Fatalf("places: %+v", places)
	}
	// The unknown place on its own makes a failed step, not a block.
	if s := out.Steps[len(out.Steps)-1]; s.Tool != "show_map" || s.Error == "" {
		t.Fatalf("last step: %+v", s)
	}
}

// A page a lookup returned this turn is one the agent may show the picture of.
func TestAssistantBlocksFromLookup(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><meta property="og:image" content="https://cdn.example/dolly.jpg"></head></html>`)
	}))
	t.Cleanup(site.Close)
	search := fakeSearchServer(t)
	model, _ := scriptedModel(t,
		calls(toolCall("c1", "ext_web_search__web_search", map[string]any{"query": "site " + strings.TrimPrefix(site.URL, "http://") + "/dolly"})),
		calls(toolCall("c2", "show_images", map[string]any{"pages": []map[string]any{{"url": site.URL + "/dolly", "caption": "Dolly Parton"}, {"url": site.URL + "/other"}}})),
		say("Here she is."),
	)
	e, admin, _ := assistantEnv(t, model, "lookblocks")
	if _, err := e.s.AddConnection(e.ctx, admin, core.AddConnectionIn{Name: "Web search", URL: search.URL, Auth: "bearer", Secret: "sk-mcp"}); err != nil {
		t.Fatal(err)
	}
	out, err := e.s.AssistantChat(e.ctx, admin, ask("show me dolly"))
	if err != nil || out.Error != "" {
		t.Fatal(err, out.Error)
	}
	if len(out.Blocks) != 1 || out.Blocks[0].Type != "images" {
		t.Fatalf("blocks: %+v", out.Blocks)
	}
	var imgs struct {
		Images []struct{ Src, Page, Caption string }
	}
	_ = json.Unmarshal(out.Blocks[0].Data, &imgs)
	// Only the page the lookup returned; pictures are served through the server's signed image proxy.
	if len(imgs.Images) != 1 || imgs.Images[0].Page != site.URL+"/dolly" || !strings.HasPrefix(imgs.Images[0].Src, "/api/image?") ||
		!strings.Contains(imgs.Images[0].Src, url.QueryEscape("https://cdn.example/dolly.jpg")) || imgs.Images[0].Caption != "Dolly Parton" {
		t.Fatalf("images: %+v", imgs)
	}
}
