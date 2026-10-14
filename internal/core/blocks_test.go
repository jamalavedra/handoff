package core_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
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
	if len(out.Blocks) != 2 || out.Blocks[0].Type != "images" || out.Blocks[1].Type != "map" {
		t.Fatalf("blocks: %+v", out.Blocks)
	}
	var imgs struct {
		Images []struct{ Src, Page, Caption string }
	}
	_ = json.Unmarshal(out.Blocks[0].Data, &imgs)
	if len(imgs.Images) != 1 || imgs.Images[0].Src != "https://cdn.example/dolly.jpg" || imgs.Images[0].Caption != "Dolly Parton" {
		t.Fatalf("images: %+v", imgs)
	}
	var places struct {
		Places []struct {
			Name     string
			Lat, Lng float64
			Note     string
		}
	}
	_ = json.Unmarshal(out.Blocks[1].Data, &places)
	if len(places.Places) != 1 || places.Places[0].Lat != 37.7726 || places.Places[0].Note != "great accessories" {
		t.Fatalf("places: %+v", places)
	}
	// The unknown place on its own makes a failed step, not a block.
	if s := out.Steps[len(out.Steps)-1]; s.Tool != "show_map" || s.Error == "" {
		t.Fatalf("last step: %+v", s)
	}
}
