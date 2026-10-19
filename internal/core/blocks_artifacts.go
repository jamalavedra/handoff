package core

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// More block kinds: a route between two places, a reminder, and people to connect with. Like images
// and maps, each shows what the server checked: the route comes from a router, the reminder's date
// has to parse, and a profile has to be a page a lookup returned this turn.

// ---- route: from one place to another, with a router's path ----

type routeIn struct {
	From string `json:"from" jsonschema:"where it starts: an address, or name and city, e.g. 'Ferry Building, San Francisco'"`
	To   string `json:"to" jsonschema:"where it ends, the same way"`
	Mode string `json:"mode,omitempty" jsonschema:"walk, bike or drive (default walk)"`
	Note string `json:"note,omitempty" jsonschema:"a few words: why this route"`
}

type routeEnd struct {
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Address string  `json:"address,omitempty"`
}

type routeOut struct {
	From     routeEnd     `json:"from"`
	To       routeEnd     `json:"to"`
	Mode     string       `json:"mode"`
	Distance float64      `json:"distance_m"`
	Duration float64      `json:"duration_s"`
	Path     [][2]float64 `json:"path"` // [lat, lng] points
	Note     string       `json:"note,omitempty"`
}

// routeProfiles maps a mode to its OSRM profile on the router (routing.openstreetmap.de serves one
// per path prefix: /routed-foot, /routed-bike, /routed-car).
var routeProfiles = map[string]string{"walk": "routed-foot", "bike": "routed-bike", "drive": "routed-car"}

func routeMode(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "", "walk", "walking", "foot":
		return "walk"
	case "bike", "cycling", "bicycle":
		return "bike"
	case "drive", "car", "driving":
		return "drive"
	}
	return ""
}

var routePlugin = blockPlugin{
	tool:   "show_route",
	kind:   "route",
	desc:   "Show a route on a map under your message, from one place to another, walking, by bike or by car, with its distance and time. Both ends are looked up by address or name and city with outside map services, so give public places (shops, venues, stations, neighbourhoods), never a private home address from a task or chat. Use it when how to get somewhere matters (a shop to visit, a venue, a meeting point).",
	schema: schemaOf[routeIn](),
	resolve: func(ctx context.Context, a *assistantRun, raw json.RawMessage) (any, string, error) {
		var in routeIn
		if err := json.Unmarshal(raw, &in); err != nil || strings.TrimSpace(in.From) == "" || strings.TrimSpace(in.To) == "" {
			return nil, "", errors.New("give where it starts and where it ends")
		}
		mode := routeMode(in.Mode)
		if mode == "" {
			return nil, "", errors.New("mode is walk, bike or drive")
		}
		from, err := a.s.geocode(ctx, strings.TrimSpace(in.From))
		if err != nil || from == nil {
			return nil, "Looked up the route", fmt.Errorf("couldn't find %q; give an address or name and city", clip(in.From, 60))
		}
		to, err := a.s.geocode(ctx, strings.TrimSpace(in.To))
		if err != nil || to == nil {
			return nil, "Looked up the route", fmt.Errorf("couldn't find %q; give an address or name and city", clip(in.To, 60))
		}
		r, err := a.s.route(ctx, routeProfiles[mode], from, to)
		if err != nil {
			return nil, "Looked up the route", err
		}
		r.Mode, r.Note = mode, clip(in.Note, 120)
		r.From = routeEnd{Name: clip(in.From, 80), Lat: from.lat, Lng: from.lng, Address: clip(from.address, 160)}
		r.To = routeEnd{Name: clip(in.To, 80), Lat: to.lat, Lng: to.lng, Address: clip(to.address, 160)}
		return r, fmt.Sprintf("Showed a %s route (%s)", mode, humanDistance(r.Distance)), nil
	},
}

// The router is the FOSSGIS OSRM servers by default (HANDOFF_ROUTER_URL for another with the same
// /routed-foot|bike|car layout).
func routerURL() string {
	if u := os.Getenv("HANDOFF_ROUTER_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://routing.openstreetmap.de"
}

const maxRoutePoints = 400

// route asks the router for the way between two points, its path thinned to maxRoutePoints.
func (s *Service) route(ctx context.Context, profile string, from, to *geo) (*routeOut, error) {
	u := fmt.Sprintf("%s/%s/route/v1/driving/%f,%f;%f,%f?overview=simplified&geometries=geojson", routerURL(), profile, from.lng, from.lat, to.lng, to.lat)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Handoff/1 (+https://github.com/joalavedra/handoff)")
	req.Header.Set("Accept", "application/json")
	resp, err := s.outbound().Do(req)
	if err != nil {
		return nil, errors.New("the router couldn't be reached")
	}
	defer resp.Body.Close()
	var out struct {
		Code   string `json:"code"`
		Routes []struct {
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
			Geometry struct {
				Coordinates [][]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("the router answered %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || out.Code != "Ok" || len(out.Routes) == 0 {
		return nil, errors.New("the router found no way between them")
	}
	rt := out.Routes[0]
	r := &routeOut{Distance: math.Round(rt.Distance), Duration: math.Round(rt.Duration)}
	coords := rt.Geometry.Coordinates
	every := max(1, (len(coords)+maxRoutePoints-1)/maxRoutePoints)
	for i, c := range coords {
		if len(c) < 2 || (i%every != 0 && i != len(coords)-1) {
			continue
		}
		r.Path = append(r.Path, [2]float64{round5(c[1]), round5(c[0])})
	}
	if len(r.Path) < 2 {
		r.Path = [][2]float64{{from.lat, from.lng}, {to.lat, to.lng}}
	}
	return r, nil
}

func round5(f float64) float64 { return math.Round(f*1e5) / 1e5 }

func humanDistance(m float64) string {
	if m < 1000 {
		return fmt.Sprintf("%.0f m", m)
	}
	return fmt.Sprintf("%.1f km", m/1000)
}

// ---- reminder: a date to remember, for a calendar or as the task's due date ----

type reminderIn struct {
	Title string `json:"title" jsonschema:"what to remember, a few words: 'Order the costume'"`
	At    string `json:"at" jsonschema:"when: YYYY-MM-DD, or YYYY-MM-DDTHH:MM for a time of day (the person's local time)"`
	Note  string `json:"note,omitempty" jsonschema:"one line: why, or what to do then"`
}

type reminderOut struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	At     string `json:"at"` // YYYY-MM-DD or YYYY-MM-DDTHH:MM, the person's local time
	AllDay bool   `json:"all_day"`
	Note   string `json:"note,omitempty"`
	Task   string `json:"task,omitempty"` // the task it's about, so the person can make it the due date
}

var reminderPlugin = blockPlugin{
	tool:   "show_reminder",
	kind:   "reminder",
	desc:   "Show a reminder card under your message: a date (and time) to remember, like the last day to order something in time or a booking. The person can add it to their calendar or make it the task's due date in one tap. Use it instead of burying an important date in text; don't change the due date yourself unless asked.",
	schema: schemaOf[reminderIn](),
	resolve: func(_ context.Context, a *assistantRun, raw json.RawMessage) (any, string, error) {
		var in reminderIn
		if err := json.Unmarshal(raw, &in); err != nil || strings.TrimSpace(in.Title) == "" {
			return nil, "", errors.New("give a title and a date")
		}
		out, err := parseReminder(in, time.Now().UTC())
		if err != nil {
			return nil, "Didn't add a reminder", err
		}
		out.Task = cmp.Or(a.task, a.open)
		out.ID = randomID("r", 12) // its calendar event's UID: reminders on the same day stay apart
		return out, "Added a reminder for " + out.At, nil
	},
}

// parseReminder checks the date: a real day, from yesterday (time zones) to five years out.
func parseReminder(in reminderIn, now time.Time) (reminderOut, error) {
	const day, minute = "2006-01-02", "2006-01-02T15:04"
	at := strings.TrimSpace(in.At)
	out := reminderOut{Title: clip(in.Title, 80), Note: clip(in.Note, 160)}
	var t time.Time
	err := errors.New("bad format")
	switch {
	case len(at) == len(day):
		t, err = time.Parse(day, at)
		out.AllDay = true
	case len(at) >= len(minute):
		at = at[:len(minute)] // seconds and zones are dropped: it's the person's local time
		t, err = time.Parse(minute, at)
	}
	if err != nil {
		return out, errors.New("at is YYYY-MM-DD or YYYY-MM-DDTHH:MM")
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if t.Before(today.AddDate(0, 0, -1)) {
		return out, errors.New("that date has passed")
	}
	if t.After(today.AddDate(5, 0, 0)) {
		return out, errors.New("that date is more than five years away")
	}
	out.At = at
	return out, nil
}

// ---- people: profiles to connect with on social networks ----

type peopleIn struct {
	People []struct {
		Name     string `json:"name"`
		URL      string `json:"url" jsonschema:"their profile page as a lookup returned it this turn (LinkedIn, GitHub, X, Instagram, a personal site…)"`
		Headline string `json:"headline,omitempty" jsonschema:"their role or what they do, a few words"`
		Note     string `json:"note,omitempty" jsonschema:"why connect, a few words"`
	} `json:"people" jsonschema:"1 to 6 people"`
}

type personOut struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Network  string `json:"network"` // linkedin, github, x, instagram, facebook, youtube, tiktok, mastodon or web
	Handle   string `json:"handle,omitempty"`
	Headline string `json:"headline,omitempty"`
	Note     string `json:"note,omitempty"`
	Avatar   string `json:"avatar,omitempty"` // the page's own picture, through the image proxy
}

var peoplePlugin = blockPlugin{
	tool:   "show_people",
	kind:   "people",
	desc:   "Show people to connect with under your message: a card each, with a button to connect or follow on their network (LinkedIn, GitHub, X, Instagram…). Only profile pages a lookup returned in this turn, so search first. Use it when the task is about reaching out to, hiring or meeting someone.",
	schema: schemaOf[peopleIn](),
	resolve: func(ctx context.Context, a *assistantRun, raw json.RawMessage) (any, string, error) {
		var in peopleIn
		if err := json.Unmarshal(raw, &in); err != nil || len(in.People) == 0 {
			return nil, "", errors.New("give 1 to 6 people")
		}
		if len(in.People) > 6 {
			in.People = in.People[:6]
		}
		out := make([]personOut, len(in.People))
		var wg sync.WaitGroup
		for i, p := range in.People {
			u, err := url.Parse(strings.TrimSpace(p.URL))
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.TrimSpace(p.Name) == "" {
				continue
			}
			// Only profiles a lookup returned: the model can't make people up, or fetch a page of its choosing.
			if a.turn == nil || !a.turn.found[normPage(u.String())] {
				continue
			}
			network, handle := profileOf(u)
			out[i] = personOut{Name: clip(p.Name, 60), URL: u.String(), Network: network, Handle: handle, Headline: clip(p.Headline, 80), Note: clip(p.Note, 120)}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if src := a.s.pageImage(ctx, u); src != "" {
					out[i].Avatar = a.s.imageProxy(src)
				}
			}()
		}
		wg.Wait()
		var found []personOut
		for _, o := range out {
			if o.URL != "" && !slices.ContainsFunc(found, func(f personOut) bool { return f.URL == o.URL }) {
				found = append(found, o)
			}
		}
		if len(found) == 0 {
			return nil, "Didn't show people", errors.New("show_people only takes profile pages that a lookup returned in this turn")
		}
		return map[string]any{"people": found}, fmt.Sprintf("Showed %d %s", len(found), plural(len(found), "person", "people")), nil
	},
}

// profileOf names the network a profile is on, and the handle in its address when there is one.
func profileOf(u *url.URL) (network, handle string) {
	host := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), "m.")
	var parts []string
	for _, p := range strings.Split(u.Path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	// first is the first part of the path that isn't one of skip ("in" in linkedin.com/in/jane).
	first := func(skip ...string) string {
		for _, p := range parts {
			if !slices.Contains(skip, strings.ToLower(p)) {
				return p
			}
		}
		return ""
	}
	at := func(h string) string {
		if h == "" {
			return ""
		}
		return "@" + strings.TrimPrefix(h, "@")
	}
	switch {
	case host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com"):
		return "linkedin", first("in", "pub", "company", "school")
	case host == "github.com":
		return "github", first()
	case host == "x.com" || host == "twitter.com":
		return "x", at(first())
	case host == "instagram.com":
		return "instagram", at(first())
	case host == "facebook.com":
		return "facebook", first()
	case host == "youtube.com":
		return "youtube", at(first("c", "user", "channel"))
	case host == "tiktok.com":
		return "tiktok", at(first())
	case len(parts) == 1 && strings.HasPrefix(parts[0], "@") && len(parts[0]) > 1:
		return "mastodon", parts[0] + "@" + host
	}
	return "web", host
}
