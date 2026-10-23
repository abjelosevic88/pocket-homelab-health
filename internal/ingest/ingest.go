// Package ingest turns what phones and exports send into samples, workouts
// and nights: the Health Auto Export app's JSON (the live path from an
// iPhone), Apple Health's export.zip, Pocket Health's own JSON, and CSV.
package ingest

import (
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// Batch is what a parser produces.
type Batch struct {
	Samples  []store.Sample
	Workouts []store.Workout
	Sleep    []store.SleepSession
	Skipped  int      // things we could not place
	Unknown  []string // metric names kept generically
	Notes    []string
}

// Loc is the zone used for times that come without an offset.
var Loc = time.Local

// ParseTime reads the time formats the sources use and returns RFC3339 with
// the offset it came with (Apple: "2024-01-01 08:00:00 +0100"; ISO 8601).
func ParseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 -07:00",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02T15:04",
		"2006-01-02",
		"02/01/2006 15:04",
		"02.01.2006 15:04",
		"02/01/2006",
		"02.01.2006",
		"1/2/2006 15:04",
		"1/2/2006",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, Loc); err == nil {
			return t, true
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 1e9 {
		if n > 1e12 {
			n /= 1000
		}
		return time.Unix(n, 0).In(Loc), true
	}
	return time.Time{}, false
}

// RFC formats for storage.
func RFC(t time.Time) string { return t.Format(time.RFC3339) }

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(x), ",", "."), 64)
		return f, err == nil
	}
	return 0, false
}

// convert normalizes a value and remembers unknown units in the note.
func convert(m metrics.Metric, unit string, v float64) (float64, string) {
	nv, ok := metrics.Normalize(m, unit, v)
	if !ok {
		return v, "unit " + unit
	}
	return nv, ""
}

// genericMetric is a metric not in the catalog: kept under a clean key.
func genericMetric(name, unit string) metrics.Metric {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.NewReplacer(" ", "_", "-", "_", "/", "_").Replace(key)
	m := metrics.Get(key)
	m.Unit = unit
	m.Agg = metrics.GuessAgg(unit)
	return m
}

func timeSeconds(s int) time.Duration { return time.Duration(s) * time.Second }
