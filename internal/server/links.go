package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Health as the
// target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "health:read", Name: "See your health", Description: "Read your daily numbers (steps, sleep, heart rate, weight…), workouts, nights and goals — for your journal, calendar and dashboards. Never medications or symptoms."},
	{Scope: "health:write", Name: "Record measurements", Description: "Add measurements and workouts (a kitchen app logging water and calories, a location app logging distance). It may change nothing you entered yourself."},
}

// capabilities is what Pocket Search, Pocket Day and Pocket Hub may read here
// (docs/capabilities.md in pocket-homelab-core); Register adds pocket:search,
// pocket:day and pocket:widget. All three only read, and never medications,
// symptoms or cycle tracking. Nothing is kept from a quick line: Health has
// no one-line log to put it through.
var capabilities = core.Capabilities{
	Search: &core.Searchable{What: "Your workouts by kind or name, your planned workouts, your goals, and the latest value of a measurement by its name", Kinds: []string{"workout", "plan", "goal", "measurement"}, Bangs: []string{"health"}},
	Day:    &core.DayFeed{Label: "Health", One: "workout", Many: "workouts", Unit: "min", What: "Your workouts, the night's sleep and the goals you met that day, with your steps and other main numbers"},
	Widget: &core.Widget{Title: "Health today", What: "Your steps against your goal, last night's sleep, today's workouts and goals"},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "health.json", Kind: core.KindPocket, Title: "Every measurement, workout and night, your goals, medicines with their doses, symptoms and habit rules"},
		{Path: "/api/v1/export.csv", File: "health-daily.csv", Kind: core.KindCSV, Title: "Your daily numbers, one row per day"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json", Title: "Adds the measurements, workouts, nights, medicines and symptoms from another box; what you already have is left alone"},
		{Path: "/api/v1/import/apple", Kind: "apple-health-zip", Form: "file", MaxBytes: 4 << 30, Title: "Reads an Apple Health export (export.zip) in the background; measurements already here are not added twice"},
	},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(metrics|dashboard|goals|workouts|sleep|overlay|summary)$`), "health:read"},
	{"GET", regexp.MustCompile(`^/api/v1/metrics/[a-z0-9_]+$`), "health:read"},
	{"GET", regexp.MustCompile(`^/api/v1/day/\d{4}-\d{2}-\d{2}$`), "health:read"},
	{"GET", regexp.MustCompile(`^/api/v1/summary/\d{4}-\d{2}-\d{2}$`), "health:read"},
	{"GET", regexp.MustCompile(`^/api/v1/workouts/\d+(/track)?$`), "health:read"},
	{"POST", regexp.MustCompile(`^/api/v1/ingest$`), "health:write"},
	{"POST", regexp.MustCompile(`^/api/v1/samples$`), "health:write"},
	{"POST", regexp.MustCompile(`^/api/v1/workouts$`), "health:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) || capabilities.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}
