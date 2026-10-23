package ingest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// Pocket Health's own body, for the Pocket Homelab app, Shortcuts, scripts
// and other Pocket apps:
//
//	{"samples": [{"metric": "weight", "at": "2025-10-23T07:30:00+02:00", "value": 80.4, "unit": "kg"}],
//	 "workouts": [{"kind": "running", "start": "…", "end": "…", "distance_m": 5100, "energy_kcal": 320}],
//	 "sleep": [{"start": "…", "end": "…", "asleep_min": 430, "deep_min": 60}]}
type Generic struct {
	Samples []struct {
		Metric string   `json:"metric"`
		At     string   `json:"at"`
		Start  string   `json:"start"`
		End    string   `json:"end"`
		Value  *float64 `json:"value"`
		Value2 *float64 `json:"value2"`
		Unit   string   `json:"unit"`
		Device string   `json:"device"`
		Note   string   `json:"note"`
	} `json:"samples"`
	Workouts []struct {
		Kind       string   `json:"kind"`
		Name       string   `json:"name"`
		Start      string   `json:"start"`
		End        string   `json:"end"`
		DurationS  int      `json:"duration_s"`
		DistanceM  float64  `json:"distance_m"`
		EnergyKcal float64  `json:"energy_kcal"`
		AvgHR      float64  `json:"avg_hr"`
		MaxHR      float64  `json:"max_hr"`
		ElevationM float64  `json:"elevation_m"`
		Steps      int      `json:"steps"`
		Notes      string   `json:"notes"`
		Device     string   `json:"device"`
		ExternalID string   `json:"external_id"`
		Temp       *float64 `json:"temperature"`
	} `json:"workouts"`
	Sleep []struct {
		Start     string  `json:"start"`
		End       string  `json:"end"`
		AsleepMin float64 `json:"asleep_min"`
		InBedMin  float64 `json:"in_bed_min"`
		DeepMin   float64 `json:"deep_min"`
		RemMin    float64 `json:"rem_min"`
		CoreMin   float64 `json:"core_min"`
		AwakeMin  float64 `json:"awake_min"`
		Device    string  `json:"device"`
		Note      string  `json:"note"`
	} `json:"sleep"`
}

// Pocket parses the generic body; source names who sent it ("api", "manual", an app slug).
func Pocket(raw []byte, source string) (*Batch, error) {
	var g Generic
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	b := &Batch{}
	for _, s := range g.Samples {
		at := s.At
		if at == "" {
			at = s.Start
		}
		t, ok := ParseTime(at)
		if !ok || s.Metric == "" || s.Value == nil {
			b.Skipped++
			continue
		}
		key := strings.ToLower(strings.TrimSpace(s.Metric))
		met := metrics.Get(key)
		if !metrics.Known(key) {
			if hm, ok := metrics.FromHAE(key); ok {
				met = hm
			} else if am, ok := metrics.FromApple(s.Metric); ok {
				met = am
			} else {
				met = genericMetric(key, s.Unit)
				b.Unknown = append(b.Unknown, key)
			}
		}
		v, note := convert(met, s.Unit, *s.Value)
		if s.Note != "" {
			note = s.Note
		}
		sm := store.Sample{Metric: met.Key, StartAt: RFC(t), Value: v, Unit: met.Unit, Source: source, Device: s.Device, Note: note}
		if s.End != "" {
			if e, ok := ParseTime(s.End); ok {
				sm.EndAt = RFC(e)
			}
		}
		if s.Value2 != nil {
			v2, _ := convert(met, s.Unit, *s.Value2)
			sm.Value2 = &v2
		}
		b.Samples = append(b.Samples, sm)
	}
	for _, w := range g.Workouts {
		start, ok := ParseTime(w.Start)
		if !ok {
			b.Skipped++
			continue
		}
		wo := store.Workout{Kind: w.Kind, Name: w.Name, StartAt: RFC(start), DurationS: w.DurationS, DistanceM: w.DistanceM, EnergyKcal: w.EnergyKcal, AvgHR: w.AvgHR, MaxHR: w.MaxHR, ElevationM: w.ElevationM, Steps: w.Steps, Notes: w.Notes, Source: source, Device: w.Device, ExternalID: w.ExternalID, Temperature: w.Temp}
		if wo.Kind == "" {
			wo.Kind = store.WorkoutKindFromApple(w.Name)
		} else if store.WorkoutKindName(wo.Kind) == "" {
			wo.Kind = "other"
		}
		if e, ok := ParseTime(w.End); ok {
			wo.EndAt = RFC(e)
		} else if w.DurationS > 0 {
			wo.EndAt = RFC(start.Add(time.Duration(w.DurationS) * time.Second))
		}
		b.Workouts = append(b.Workouts, wo)
	}
	for _, x := range g.Sleep {
		start, ok1 := ParseTime(x.Start)
		end, ok2 := ParseTime(x.End)
		if !ok1 || !ok2 {
			b.Skipped++
			continue
		}
		b.Sleep = append(b.Sleep, store.SleepSession{StartAt: RFC(start), EndAt: RFC(end), AsleepS: int(x.AsleepMin * 60), InBedS: int(x.InBedMin * 60), DeepS: int(x.DeepMin * 60), RemS: int(x.RemMin * 60), CoreS: int(x.CoreMin * 60), AwakeS: int(x.AwakeMin * 60), Source: source, Device: x.Device, Note: x.Note})
	}
	return b, nil
}

// CSV reads "date,value[,unit]" or "date,metric,value[,unit]" rows for one
// metric (metric = "" means the file names it per row). Smart scales,
// spreadsheets and other apps export this.
func CSV(r io.Reader, metric, source string) (*Batch, error) {
	rd := csv.NewReader(r)
	rd.FieldsPerRecord = -1
	rd.LazyQuotes = true
	rd.TrimLeadingSpace = true
	rows, err := rd.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	b := &Batch{}
	if len(rows) == 0 {
		return b, nil
	}
	// header?
	head := rows[0]
	cols := map[string]int{}
	hasHeader := false
	for i, h := range head {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "date" || h == "time" || h == "datetime" || h == "timestamp" || h == "value" || h == "metric" || h == "weight" || h == "unit" {
			hasHeader = true
		}
		cols[h] = i
	}
	if !hasHeader {
		cols = map[string]int{}
	} else {
		rows = rows[1:]
	}
	idx := func(names ...string) int {
		for _, n := range names {
			if i, ok := cols[n]; ok {
				return i
			}
		}
		return -1
	}
	iDate, iVal, iMet, iUnit, iNote := idx("date", "datetime", "time", "timestamp", "day"), idx("value", "qty", "weight", "amount"), idx("metric", "type", "name"), idx("unit", "units"), idx("note", "notes", "comment")
	if iDate < 0 {
		iDate = 0
	}
	if iVal < 0 {
		if iMet >= 0 || metric == "" {
			iVal = 2
		} else {
			iVal = 1
		}
	}
	if iMet < 0 && metric == "" {
		iMet = 1
	}
	for _, row := range rows {
		if len(row) <= iDate || len(row) <= iVal {
			b.Skipped++
			continue
		}
		t, ok := ParseTime(row[iDate])
		if !ok {
			b.Skipped++
			continue
		}
		v, ok := num(row[iVal])
		if !ok {
			b.Skipped++
			continue
		}
		key := metric
		if iMet >= 0 && iMet < len(row) && strings.TrimSpace(row[iMet]) != "" {
			key = strings.ToLower(strings.TrimSpace(row[iMet]))
		}
		if key == "" {
			b.Skipped++
			continue
		}
		met := metrics.Get(key)
		if !metrics.Known(key) {
			if hm, ok := metrics.FromHAE(key); ok {
				met = hm
			} else {
				met = genericMetric(key, "")
				b.Unknown = append(b.Unknown, key)
			}
		}
		unit := ""
		if iUnit >= 0 && iUnit < len(row) {
			unit = row[iUnit]
		}
		v, note := convert(met, unit, v)
		if iNote >= 0 && iNote < len(row) {
			note = row[iNote]
		}
		b.Samples = append(b.Samples, store.Sample{Metric: met.Key, StartAt: RFC(t), Value: v, Unit: met.Unit, Source: source, Note: note})
	}
	return b, nil
}
