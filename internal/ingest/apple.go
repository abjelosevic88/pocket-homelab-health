package ingest

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// Apple Health → Export All Health Data → export.zip (apple_health_export/
// export.xml inside, often more than a gigabyte). Parsed as a token stream so
// memory stays flat. Fine-grained records (steps every few minutes) are
// folded into hourly buckets per device so the database stays small; the
// rollup later picks one device per day for summed metrics.

// Sink receives parsed things in chunks.
type Sink interface {
	Samples(ctx context.Context, s []store.Sample) error
	Workouts(ctx context.Context, w []store.Workout) error
	Sleep(ctx context.Context, s []store.SleepSession) error
	Progress(ctx context.Context, text string)
}

// AppleStats is what the import counted.
type AppleStats struct {
	Records  int
	Samples  int
	Workouts int
	Sleep    int
	Skipped  int
	Unknown  map[string]int
}

// AppleFile opens export.zip or export.xml.
func AppleFile(path string) (io.ReadCloser, error) {
	if strings.HasSuffix(strings.ToLower(path), ".zip") {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if strings.HasSuffix(f.Name, "export.xml") || strings.HasSuffix(f.Name, "Export.xml") {
				rc, err := f.Open()
				if err != nil {
					zr.Close()
					return nil, err
				}
				return &zipEntry{rc, zr}, nil
			}
		}
		zr.Close()
		return nil, fmt.Errorf("no export.xml in the zip")
	}
	return os.Open(path)
}

type zipEntry struct {
	io.ReadCloser
	zr *zip.ReadCloser
}

func (z *zipEntry) Close() error { z.ReadCloser.Close(); return z.zr.Close() }

type bucketKey struct {
	metric, hour, device string
}

type bucket struct {
	start, end time.Time
	values     []float64
	v2         []float64
	unit       string
}

// Apple streams export.xml into the sink.
func Apple(ctx context.Context, r io.Reader, sink Sink, since string) (*AppleStats, error) {
	st := &AppleStats{Unknown: map[string]int{}}
	dec := xml.NewDecoder(r)
	dec.Strict = false
	buckets := map[bucketKey]*bucket{}
	var pendingSleep []sleepRec
	var samples []store.Sample
	var workouts []store.Workout
	flushBuckets := func(all bool, before time.Time) error {
		for k, b := range buckets {
			if !all && !b.start.Before(before) {
				continue
			}
			m := metrics.Get(k.metric)
			var s store.Sample
			s.Metric, s.StartAt, s.EndAt, s.Unit, s.Source, s.Device = k.metric, RFC(b.start), RFC(b.end), b.unit, "apple", k.device
			mm := m
			if !metrics.Known(k.metric) {
				mm.Agg = metrics.GuessAgg(b.unit)
			}
			s.Value = metrics.Aggregate(mm, b.values)
			if len(b.v2) > 0 {
				v2 := metrics.Aggregate(mm, b.v2)
				s.Value2 = &v2
			}
			if mm.Agg != metrics.Sum && len(b.values) > 1 {
				lo, hi := b.values[0], b.values[0]
				for _, v := range b.values {
					if v < lo {
						lo = v
					}
					if v > hi {
						hi = v
					}
				}
				s.ValueMin, s.ValueMax = &lo, &hi
			}
			samples = append(samples, s)
			delete(buckets, k)
		}
		if len(samples) >= 2000 || (all && len(samples) > 0) {
			if err := sink.Samples(ctx, samples); err != nil {
				return err
			}
			st.Samples += len(samples)
			samples = samples[:0]
		}
		return nil
	}
	var lastStart time.Time
	lastFlush := time.Time{}
	bpPending := map[string]*store.Sample{} // systolic/diastolic pairs by start time
	for {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return st, fmt.Errorf("xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "Record":
			st.Records++
			a := attrs(se)
			typ := a["type"]
			start, ok := ParseTime(a["startDate"])
			if !ok {
				st.Skipped++
				continue
			}
			if since != "" && start.Format("2006-01-02") < since {
				continue
			}
			end, _ := ParseTime(a["endDate"])
			device := deviceName(a["sourceName"])
			if typ == "HKCategoryTypeIdentifierSleepAnalysis" {
				pendingSleep = append(pendingSleep, sleepRec{start: start, end: end, value: a["value"], device: device})
				if len(pendingSleep) > 20000 {
					sessions := foldSleep(pendingSleep[:15000])
					pendingSleep = pendingSleep[15000:]
					if err := sink.Sleep(ctx, sessions); err != nil {
						return st, err
					}
					st.Sleep += len(sessions)
				}
				continue
			}
			m, known := metrics.FromApple(typ)
			var v float64
			var vok bool
			if strings.HasPrefix(typ, "HKCategoryTypeIdentifier") {
				// category records (mindful sessions, handwashing…): the span or a count
				switch {
				case typ == "HKCategoryTypeIdentifierMindfulSession" && !end.IsZero():
					v, vok = end.Sub(start).Minutes(), true
				case typ == "HKCategoryTypeIdentifierHandwashingEvent" || typ == "HKCategoryTypeIdentifierToothbrushingEvent" || typ == "HKCategoryTypeIdentifierSexualActivity":
					v, vok = 1, true
				default:
					st.Skipped++
					continue
				}
				if !known {
					st.Unknown[typ]++
					continue
				}
			} else {
				v, vok = num(a["value"])
				if !vok {
					st.Skipped++
					continue
				}
				if !known {
					key := strings.TrimPrefix(typ, "HKQuantityTypeIdentifier")
					if key == typ || key == "" {
						st.Unknown[typ]++
						st.Skipped++
						continue
					}
					m = genericMetric(snake(key), a["unit"])
					st.Unknown[m.Key]++
				}
			}
			nv, _ := metrics.Normalize(m, a["unit"], v)
			if m.Key == "spo2" && nv <= 1 {
				nv *= 100
			}
			if m.Key == "body_fat" && nv <= 1 {
				nv *= 100
			}
			if m.Key == "blood_pressure" {
				key := RFC(start) + "|" + device
				p := bpPending[key]
				if p == nil {
					p = &store.Sample{Metric: "blood_pressure", StartAt: RFC(start), Unit: "mmHg", Source: "apple", Device: device}
					bpPending[key] = p
				}
				if strings.HasSuffix(typ, "Systolic") {
					p.Value = nv
				} else {
					d := nv
					p.Value2 = &d
				}
				if p.Value > 0 && p.Value2 != nil {
					samples = append(samples, *p)
					delete(bpPending, key)
				}
				continue
			}
			// bucket per hour and device
			hour := start.Format("2006-01-02T15")
			if m.Agg == metrics.Last || (!metrics.Known(m.Key) && metrics.GuessAgg(a["unit"]) != metrics.Sum) || m.Category == metrics.Body {
				hour = start.Format("2006-01-02T15:04:05") // keep every reading of slow metrics
			}
			k := bucketKey{m.Key, hour, device}
			b := buckets[k]
			if b == nil {
				b = &bucket{start: start, end: end, unit: m.Unit}
				buckets[k] = b
			}
			if start.Before(b.start) {
				b.start = start
			}
			if end.After(b.end) {
				b.end = end
			}
			b.values = append(b.values, nv)
			if start.After(lastStart) {
				lastStart = start
			}
			// the file is roughly chronological per type; flush buckets older than two days now and then
			if time.Since(lastFlush) > 2*time.Second || len(buckets) > 50000 {
				lastFlush = time.Now()
				if err := flushBuckets(false, lastStart.Add(-48*time.Hour)); err != nil {
					return st, err
				}
				sink.Progress(ctx, fmt.Sprintf("%d records read, up to %s", st.Records, lastStart.Format("2006-01-02")))
			}
		case "Workout":
			a := attrs(se)
			start, ok := ParseTime(a["startDate"])
			if !ok {
				st.Skipped++
				continue
			}
			if since != "" && start.Format("2006-01-02") < since {
				dec.Skip()
				continue
			}
			end, _ := ParseTime(a["endDate"])
			w := store.Workout{Kind: store.WorkoutKindFromApple(a["workoutActivityType"]), Name: prettyWorkout(a["workoutActivityType"]), StartAt: RFC(start), Source: "apple", Device: deviceName(a["sourceName"])}
			if !end.IsZero() {
				w.EndAt = RFC(end)
				w.DurationS = int(end.Sub(start).Seconds())
			}
			if d, ok := num(a["duration"]); ok && d > 0 {
				switch strings.ToLower(a["durationUnit"]) {
				case "s", "sec":
					w.DurationS = int(d)
				case "h", "hr":
					w.DurationS = int(d * 3600)
				default:
					w.DurationS = int(d * 60)
				}
			}
			if d, ok := num(a["totalDistance"]); ok {
				km, _ := metrics.Normalize(metrics.Get("distance"), a["totalDistanceUnit"], d)
				w.DistanceM = km * 1000
			}
			if e, ok := num(a["totalEnergyBurned"]); ok {
				w.EnergyKcal, _ = metrics.Normalize(metrics.Get("active_energy"), a["totalEnergyBurnedUnit"], e)
			}
			// newer exports: child WorkoutStatistics / MetadataEntry
			depth := 1
			for depth > 0 {
				t2, err := dec.Token()
				if err != nil {
					break
				}
				switch x := t2.(type) {
				case xml.StartElement:
					depth++
					b := attrs(x)
					switch x.Name.Local {
					case "WorkoutStatistics":
						typ := b["type"]
						switch {
						case strings.HasSuffix(typ, "DistanceWalkingRunning"), strings.HasSuffix(typ, "DistanceCycling"), strings.HasSuffix(typ, "DistanceSwimming"):
							if s, ok := num(b["sum"]); ok {
								km, _ := metrics.Normalize(metrics.Get("distance"), b["unit"], s)
								w.DistanceM = km * 1000
							}
						case strings.HasSuffix(typ, "ActiveEnergyBurned"):
							if s, ok := num(b["sum"]); ok {
								w.EnergyKcal, _ = metrics.Normalize(metrics.Get("active_energy"), b["unit"], s)
							}
						case strings.HasSuffix(typ, "HeartRate"):
							if v, ok := num(b["average"]); ok {
								w.AvgHR = v
							}
							if v, ok := num(b["maximum"]); ok {
								w.MaxHR = v
							}
						case strings.HasSuffix(typ, "StepCount"):
							if s, ok := num(b["sum"]); ok {
								w.Steps = int(s)
							}
						}
					case "MetadataEntry":
						switch b["key"] {
						case "HKElevationAscended":
							if v, ok := num(strings.Fields(b["value"])[0]); ok {
								unit := "cm"
								if f := strings.Fields(b["value"]); len(f) > 1 {
									unit = f[1]
								}
								m, _ := metrics.Normalize(metrics.Get("six_min_walk"), unit, v)
								w.ElevationM = m
							}
						case "HKWeatherTemperature":
							f := strings.Fields(b["value"])
							if len(f) > 0 {
								if v, ok := num(f[0]); ok {
									unit := "degF"
									if len(f) > 1 {
										unit = f[1]
									}
									c, _ := metrics.Normalize(metrics.Get("body_temp"), unit, v)
									w.Temperature = &c
								}
							}
						case "HKWeatherHumidity":
							f := strings.Fields(b["value"])
							if len(f) > 0 {
								if v, ok := num(f[0]); ok {
									if v > 1 {
										v = v / 100
									}
									h := v * 100
									w.Humidity = &h
								}
							}
						case "HKIndoorWorkout":
							if b["value"] == "1" {
								w.Notes = "Indoor"
							}
						}
					}
				case xml.EndElement:
					depth--
				}
			}
			workouts = append(workouts, w)
			if len(workouts) >= 200 {
				if err := sink.Workouts(ctx, workouts); err != nil {
					return st, err
				}
				st.Workouts += len(workouts)
				workouts = workouts[:0]
			}
		case "ActivitySummary", "ClinicalRecord", "Correlation", "Audiogram", "ExportDate", "Me":
			// ActivitySummary duplicates the ring data already in Records
			if se.Name.Local != "ExportDate" && se.Name.Local != "Me" {
				dec.Skip()
			}
		}
	}
	if err := flushBuckets(true, time.Time{}); err != nil {
		return st, err
	}
	if len(workouts) > 0 {
		if err := sink.Workouts(ctx, workouts); err != nil {
			return st, err
		}
		st.Workouts += len(workouts)
	}
	if len(pendingSleep) > 0 {
		sessions := foldSleep(pendingSleep)
		if err := sink.Sleep(ctx, sessions); err != nil {
			return st, err
		}
		st.Sleep += len(sessions)
	}
	return st, nil
}

func attrs(se xml.StartElement) map[string]string {
	m := make(map[string]string, len(se.Attr))
	for _, a := range se.Attr {
		m[a.Name.Local] = a.Value
	}
	return m
}

// deviceName shortens "Aleksandar's Apple Watch" to "Apple Watch".
func deviceName(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case strings.Contains(s, "Apple Watch") || strings.Contains(s, "Watch"):
		return "Apple Watch"
	case strings.Contains(s, "iPhone"):
		return "iPhone"
	}
	if i := strings.Index(s, "’s "); i > 0 {
		return s[i+4:]
	}
	if i := strings.Index(s, "'s "); i > 0 {
		return s[i+3:]
	}
	return s
}

func snake(camel string) string {
	var b strings.Builder
	for i, r := range camel {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func prettyWorkout(typ string) string {
	n := strings.TrimPrefix(typ, "HKWorkoutActivityType")
	var b strings.Builder
	for i, r := range n {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

type sleepRec struct {
	start, end time.Time
	value      string
	device     string
}

// foldSleep turns Apple's per-stage records into nights: records of the same
// device less than two hours apart belong to one night; the night's date is
// the wake-up morning. "InBed" records only count when no stage data exists.
func foldSleep(recs []sleepRec) []store.SleepSession {
	byDev := map[string][]sleepRec{}
	for _, r := range recs {
		if r.end.IsZero() || !r.end.After(r.start) {
			continue
		}
		byDev[r.device] = append(byDev[r.device], r)
	}
	var out []store.SleepSession
	for dev, rs := range byDev {
		sortSleep(rs)
		var cur *store.SleepSession
		var curEnd time.Time
		hasStages := false
		flush := func() {
			if cur == nil {
				return
			}
			if hasStages {
				cur.AsleepS = cur.CoreS + cur.DeepS + cur.RemS
				if cur.AsleepS == 0 {
					cur.AsleepS = cur.InBedS - cur.AwakeS
				}
			} else if cur.AsleepS == 0 {
				cur.AsleepS = cur.InBedS
			}
			if cur.InBedS == 0 {
				cur.InBedS = cur.AsleepS + cur.AwakeS
			}
			cur.EndAt = RFC(curEnd)
			cur.Date = curEnd.Format("2006-01-02")
			if cur.AsleepS >= 20*60 {
				out = append(out, *cur)
			}
			cur = nil
			hasStages = false
		}
		for _, r := range rs {
			if cur != nil && r.start.Sub(curEnd) > 2*time.Hour {
				flush()
			}
			if cur == nil {
				cur = &store.SleepSession{StartAt: RFC(r.start), Source: "apple", Device: dev}
				curEnd = r.end
			}
			secs := int(r.end.Sub(r.start).Seconds())
			switch {
			case strings.HasSuffix(r.value, "AsleepCore"):
				cur.CoreS += secs
				hasStages = true
			case strings.HasSuffix(r.value, "AsleepDeep"):
				cur.DeepS += secs
				hasStages = true
			case strings.HasSuffix(r.value, "AsleepREM"):
				cur.RemS += secs
				hasStages = true
			case strings.HasSuffix(r.value, "Awake"):
				cur.AwakeS += secs
			case strings.HasSuffix(r.value, "AsleepUnspecified") || strings.HasSuffix(r.value, "Asleep"):
				cur.AsleepS += secs
			case strings.HasSuffix(r.value, "InBed"):
				cur.InBedS += secs
			}
			if r.end.After(curEnd) {
				curEnd = r.end
			}
		}
		flush()
	}
	return out
}

func sortSleep(rs []sleepRec) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j].start.Before(rs[j-1].start); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

var _ = strconv.Itoa
