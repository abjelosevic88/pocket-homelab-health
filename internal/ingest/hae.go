package ingest

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// Health Auto Export (iOS) "REST API" automation body:
//
//	{"data": {"metrics": [{"name": "step_count", "units": "count", "data": [{"date": "2025-10-23 00:00:00 +0200", "qty": 8123, "source": "Apple Watch"}]}],
//	          "workouts": [{"name": "Running", "start": "…", "end": "…", "duration": 1800, "activeEnergyBurned": {"qty": 300, "units": "kcal"}, "distance": {"qty": 5.1, "units": "km"}, "heartRate": {"avg": …}}]}}
//
// Some metrics carry other fields than qty: heart_rate has Avg/Min/Max,
// blood_pressure has systolic/diastolic, sleep_analysis has the stages.

type haeMetric struct {
	Name  string           `json:"name"`
	Units string           `json:"units"`
	Data  []map[string]any `json:"data"`
}

type haeQty struct {
	Qty   float64 `json:"qty"`
	Units string  `json:"units"`
}

type haeWorkout struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	Start              string           `json:"start"`
	End                string           `json:"end"`
	Duration           float64          `json:"duration"`
	ActiveEnergyBurned *haeQty          `json:"activeEnergyBurned"`
	ActiveEnergy       *haeQty          `json:"activeEnergy"`
	TotalEnergy        *haeQty          `json:"totalEnergy"`
	Distance           *haeQty          `json:"distance"`
	Elevation          *haeQty          `json:"elevationUp"`
	StepCount          *haeQty          `json:"stepCount"`
	Intensity          *haeQty          `json:"intensity"`
	Temperature        *haeQty          `json:"temperature"`
	Humidity           *haeQty          `json:"humidity"`
	HeartRate          map[string]any   `json:"heartRate"`
	AvgHR              *haeQty          `json:"avgHeartRate"`
	MaxHR              *haeQty          `json:"maxHeartRate"`
	HeartRateData      []map[string]any `json:"heartRateData"`
	Location           string           `json:"location"`
}

type haeBody struct {
	Data struct {
		Metrics  []haeMetric  `json:"metrics"`
		Workouts []haeWorkout `json:"workouts"`
	} `json:"data"`
}

// HAE parses a Health Auto Export body.
func HAE(raw []byte) (*Batch, error) {
	var body haeBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("not a Health Auto Export body: %w", err)
	}
	b := &Batch{}
	for _, m := range body.Data.Metrics {
		name := strings.ToLower(strings.TrimSpace(m.Name))
		if name == "sleep_analysis" {
			haeSleep(b, m)
			continue
		}
		met, known := metrics.FromHAE(name)
		if !known {
			met = genericMetric(name, m.Units)
			b.Unknown = append(b.Unknown, name)
		}
		for _, d := range m.Data {
			t, ok := ParseTime(str(d["date"]))
			if !ok {
				b.Skipped++
				continue
			}
			device := str(d["source"])
			if i := strings.Index(device, "|"); i > 0 { // "Apple Watch|iPhone" when merged
				device = strings.TrimSpace(device[:i])
			}
			s := store.Sample{Metric: met.Key, StartAt: RFC(t), Unit: met.Unit, Source: "hae", Device: device}
			switch {
			case met.Key == "blood_pressure":
				sys, ok1 := num(d["systolic"])
				dia, ok2 := num(d["diastolic"])
				if !ok1 || !ok2 {
					b.Skipped++
					continue
				}
				s.Value, s.Value2 = sys, &dia
			case d["qty"] != nil:
				v, ok := num(d["qty"])
				if !ok {
					b.Skipped++
					continue
				}
				v, note := convert(met, m.Units, v)
				s.Value, s.Note = v, note
			default:
				// heart rate style: Avg / Min / Max
				avg, okA := num(firstOf(d, "Avg", "avg", "average"))
				if !okA {
					b.Skipped++
					continue
				}
				avg, note := convert(met, m.Units, avg)
				s.Value, s.Note = avg, note
				if lo, ok := num(firstOf(d, "Min", "min")); ok {
					lo, _ = convert(met, m.Units, lo)
					s.ValueMin = &lo
				}
				if hi, ok := num(firstOf(d, "Max", "max")); ok {
					hi, _ = convert(met, m.Units, hi)
					s.ValueMax = &hi
				}
			}
			if met.Key == "spo2" && s.Value <= 1 {
				s.Value *= 100
			}
			if met.Key == "body_fat" && s.Value <= 1 {
				s.Value *= 100
			}
			b.Samples = append(b.Samples, s)
		}
	}
	for _, w := range body.Data.Workouts {
		start, ok := ParseTime(w.Start)
		if !ok {
			b.Skipped++
			continue
		}
		end, _ := ParseTime(w.End)
		wo := store.Workout{Kind: store.WorkoutKindFromApple(w.Name), Name: w.Name, StartAt: RFC(start), Source: "hae", ExternalID: w.ID}
		if !end.IsZero() {
			wo.EndAt = RFC(end)
			wo.DurationS = int(end.Sub(start).Seconds())
		}
		if w.Duration > 0 {
			wo.DurationS = int(w.Duration)
		}
		if q := firstQty(w.ActiveEnergyBurned, w.ActiveEnergy, w.TotalEnergy); q != nil {
			wo.EnergyKcal, _ = metrics.Normalize(metrics.Get("active_energy"), q.Units, q.Qty)
		}
		if w.Distance != nil {
			km, _ := metrics.Normalize(metrics.Get("distance"), w.Distance.Units, w.Distance.Qty)
			wo.DistanceM = km * 1000
		}
		if w.Elevation != nil {
			wo.ElevationM, _ = metrics.Normalize(metrics.Get("six_min_walk"), w.Elevation.Units, w.Elevation.Qty)
		}
		if w.StepCount != nil {
			wo.Steps = int(w.StepCount.Qty)
		}
		if w.Temperature != nil {
			c, _ := metrics.Normalize(metrics.Get("body_temp"), w.Temperature.Units, w.Temperature.Qty)
			wo.Temperature = &c
		}
		if w.Humidity != nil {
			h := w.Humidity.Qty
			wo.Humidity = &h
		}
		if w.HeartRate != nil {
			if v, ok := num(firstOf(w.HeartRate, "avg", "Avg", "average")); ok {
				wo.AvgHR = v
			}
			if v, ok := num(firstOf(w.HeartRate, "max", "Max")); ok {
				wo.MaxHR = v
			}
		}
		if w.AvgHR != nil {
			wo.AvgHR = w.AvgHR.Qty
		}
		if w.MaxHR != nil {
			wo.MaxHR = w.MaxHR.Qty
		}
		if wo.AvgHR == 0 && len(w.HeartRateData) > 0 {
			sum, n, mx := 0.0, 0, 0.0
			for _, p := range w.HeartRateData {
				if v, ok := num(firstOf(p, "Avg", "qty", "avg")); ok {
					sum += v
					n++
					if v > mx {
						mx = v
					}
				}
			}
			if n > 0 {
				wo.AvgHR, wo.MaxHR = sum/float64(n), mx
			}
		}
		b.Workouts = append(b.Workouts, wo)
	}
	return b, nil
}

func firstQty(qs ...*haeQty) *haeQty {
	for _, q := range qs {
		if q != nil && q.Qty > 0 {
			return q
		}
	}
	return nil
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// haeSleep turns sleep_analysis rows (one per night) into sessions. Values
// are hours in HAE's export (units "hr") unless told otherwise.
func haeSleep(b *Batch, m haeMetric) {
	for _, d := range m.Data {
		start, ok1 := ParseTime(str(firstOf(d, "sleepStart", "inBedStart", "date")))
		end, ok2 := ParseTime(str(firstOf(d, "sleepEnd", "inBedEnd")))
		if !ok1 {
			b.Skipped++
			continue
		}
		secs := func(k string) int {
			v, ok := num(d[k])
			if !ok {
				return 0
			}
			switch strings.ToLower(m.Units) {
			case "min":
				return int(v * 60)
			case "s", "sec":
				return int(v)
			}
			return int(v * 3600)
		}
		ss := store.SleepSession{StartAt: RFC(start), Source: "hae", Device: str(d["source"]),
			AsleepS: secs("asleep"), InBedS: secs("inBed"), CoreS: secs("core"), DeepS: secs("deep"), RemS: secs("rem"), AwakeS: secs("awake")}
		if ss.AsleepS == 0 {
			ss.AsleepS = secs("totalSleep")
		}
		if !ok2 {
			if ss.InBedS > 0 {
				end = start.Add(timeSeconds(ss.InBedS))
			} else if ss.AsleepS > 0 {
				end = start.Add(timeSeconds(ss.AsleepS))
			} else {
				b.Skipped++
				continue
			}
		}
		ss.EndAt = RFC(end)
		if ss.AsleepS == 0 && ss.InBedS == 0 {
			b.Skipped++
			continue
		}
		b.Sleep = append(b.Sleep, ss)
	}
}
