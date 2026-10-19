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
	"time"

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

// A route comes from the router between two geocoded ends, a reminder needs a real date, and people
// are only shown from profile pages a lookup returned this turn.
func TestAssistantArtifactBlocks(t *testing.T) {
	geo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("q") {
		case "Ferry Building, San Francisco":
			fmt.Fprint(w, `[{"lat":"37.7955","lon":"-122.3937","display_name":"Ferry Building"}]`)
		case "One Stop Party Shop, San Francisco":
			fmt.Fprint(w, `[{"lat":"37.7726","lon":"-122.4313","display_name":"One Stop Party Shop"}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	t.Cleanup(geo.Close)
	t.Setenv("HANDOFF_GEOCODER_URL", geo.URL)
	var routed string
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routed = r.URL.Path
		fmt.Fprint(w, `{"code":"Ok","routes":[{"distance":4321.4,"duration":3100.2,"geometry":{"coordinates":[[-122.3937,37.7955],[-122.41,37.785],[-122.4313,37.7726]]}}]}`)
	}))
	t.Cleanup(router.Close)
	t.Setenv("HANDOFF_ROUTER_URL", router.URL)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><meta property="og:image" content="https://cdn.example/jane.jpg"></head></html>`)
	}))
	t.Cleanup(site.Close)
	search := fakeSearchServer(t)
	soon := time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")

	model, _ := scriptedModel(t,
		calls(toolCall("c1", "ext_web_search__web_search", map[string]any{"query": "site " + strings.TrimPrefix(site.URL, "http://") + "/in/jane"})),
		calls(
			toolCall("c2", "show_route", map[string]any{"from": "Ferry Building, San Francisco", "to": "One Stop Party Shop, San Francisco", "mode": "cycling"}),
			toolCall("c3", "show_reminder", map[string]any{"title": "Order the costume", "at": soon, "note": "to try it on in time"}),
			toolCall("c4", "show_reminder", map[string]any{"title": "Too late", "at": "2001-01-01"}),
			toolCall("c5", "show_people", map[string]any{"people": []map[string]any{
				{"name": "Jane Doe", "url": site.URL + "/in/jane", "headline": "Costume designer"},
				{"name": "Made Up", "url": "https://www.linkedin.com/in/made-up"},
			}}),
		),
		say("Here's the plan."),
	)
	e, admin, _ := assistantEnv(t, model, "artifacts")
	if _, err := e.s.AddConnection(e.ctx, admin, core.AddConnectionIn{Name: "Web search", URL: search.URL, Auth: "bearer", Secret: "sk-mcp"}); err != nil {
		t.Fatal(err)
	}
	out, err := e.s.AssistantChat(e.ctx, admin, ask("plan my costume shopping"))
	if err != nil || out.Error != "" {
		t.Fatal(err, out.Error)
	}
	var kinds []string
	for _, b := range out.Blocks {
		kinds = append(kinds, b.Type)
	}
	if !slices.Equal(kinds, []string{"route", "reminder", "people"}) {
		t.Fatalf("blocks: %v", kinds)
	}
	var route struct {
		Mode     string
		Distance float64 `json:"distance_m"`
		Path     [][2]float64
		From, To struct{ Lat float64 }
	}
	_ = json.Unmarshal(out.Blocks[0].Data, &route)
	if route.Mode != "bike" || routed != "/routed-bike/route/v1/driving/-122.393700,37.795500;-122.431300,37.772600" ||
		route.Distance != 4321 || len(route.Path) != 3 || route.Path[0] != [2]float64{37.7955, -122.3937} || route.To.Lat != 37.7726 {
		t.Fatalf("route: %+v (asked %s)", route, routed)
	}
	var rem struct {
		Title, At string
		AllDay    bool `json:"all_day"`
	}
	_ = json.Unmarshal(out.Blocks[1].Data, &rem)
	if rem.Title != "Order the costume" || rem.At != soon || !rem.AllDay {
		t.Fatalf("reminder: %+v", rem)
	}
	if i := slices.IndexFunc(out.Steps, func(s core.AssistantStep) bool { return s.Tool == "show_reminder" && s.Error != "" }); i < 0 {
		t.Fatalf("a past reminder wasn't refused: %+v", out.Steps)
	}
	var people struct {
		People []struct{ Name, URL, Network, Avatar, Headline string }
	}
	_ = json.Unmarshal(out.Blocks[2].Data, &people)
	// Only the profile the lookup returned; the one the model named itself is dropped.
	if len(people.People) != 1 || people.People[0].Name != "Jane Doe" || people.People[0].Network != "web" ||
		!strings.HasPrefix(people.People[0].Avatar, "/api/image?") || people.People[0].Headline != "Costume designer" {
		t.Fatalf("people: %+v", people)
	}
}
