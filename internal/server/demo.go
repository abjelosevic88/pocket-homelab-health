package server

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// seedDemo fills an empty box owner's account with 90 days of plausible
// data (DEMO_DATA=true), so a first look at the app is not an empty page.
func (s *Server) seedDemo(ctx context.Context, owner int64) error {
	if stats, err := s.store.MetricStats(ctx, owner); err != nil || len(stats) > 0 {
		return err
	}
	rng := rand.New(rand.NewSource(7))
	loc := time.Local
	today := time.Now().In(loc)
	var samples []store.Sample
	var workouts []store.Workout
	var nights []store.SleepSession
	weight := 82.4
	at := func(d time.Time, h, m int) string {
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc).Format(time.RFC3339)
	}
	add := func(metric string, when string, v float64, dev string) {
		samples = append(samples, store.Sample{Metric: metric, StartAt: when, Value: math.Round(v*10) / 10, Source: "hae", Device: dev})
	}
	for i := 89; i >= 0; i-- {
		d := today.AddDate(0, 0, -i)
		wd := d.Weekday()
		weekend := wd == time.Saturday || wd == time.Sunday
		// steps: weekday commute pattern, weekends vary
		base := 7800.0
		if weekend {
			base = 5500 + rng.Float64()*6000
		}
		steps := base + rng.NormFloat64()*1800
		if i%11 == 0 {
			steps += 6000 // a long walk
		}
		steps = math.Max(1200, steps)
		add("steps", at(d, 0, 0), math.Round(steps), "Apple Watch")
		add("distance", at(d, 0, 0), steps*0.00074, "Apple Watch")
		add("flights", at(d, 0, 0), math.Round(4+rng.Float64()*10), "Apple Watch")
		add("active_energy", at(d, 0, 0), 300+steps*0.03+rng.NormFloat64()*60, "Apple Watch")
		add("resting_energy", at(d, 0, 0), 1720+rng.NormFloat64()*30, "Apple Watch")
		add("exercise_min", at(d, 0, 0), math.Max(0, 18+rng.NormFloat64()*14), "Apple Watch")
		add("stand_hours", at(d, 0, 0), math.Round(9+rng.Float64()*5), "Apple Watch")
		// sleep: weekend later
		bedH, bedM := 23, 10+rng.Intn(50)
		if weekend {
			bedH = 23 + rng.Intn(2)
		}
		bed := time.Date(d.Year(), d.Month(), d.Day(), bedH, bedM, 0, 0, loc).AddDate(0, 0, -1)
		dur := 6.6 + rng.NormFloat64()*0.8
		if weekend {
			dur += 0.7
		}
		wake := bed.Add(time.Duration(dur*3600) * time.Second)
		asleep := int(dur*3600) - 20*60
		deep := int(float64(asleep) * (0.16 + rng.Float64()*0.06))
		rem := int(float64(asleep) * (0.20 + rng.Float64()*0.06))
		nights = append(nights, store.SleepSession{StartAt: bed.Format(time.RFC3339), EndAt: wake.Format(time.RFC3339), InBedS: int(dur * 3600), AsleepS: asleep, DeepS: deep, RemS: rem, CoreS: asleep - deep - rem, AwakeS: 20 * 60, Source: "hae", Device: "Apple Watch"})
		// heart: resting HR drifts down over the 90 days, worse after short sleep
		rhr := 60 - float64(89-i)*0.03 + (7-dur)*1.5 + rng.NormFloat64()*1.5
		add("resting_hr", at(d, 6, 30), math.Round(rhr), "Apple Watch")
		add("hrv", at(d, 5, 0), math.Max(18, 42+(dur-7)*4+rng.NormFloat64()*6), "Apple Watch")
		add("walking_hr", at(d, 12, 0), 98+rng.NormFloat64()*6, "Apple Watch")
		samples = append(samples, store.Sample{Metric: "heart_rate", StartAt: at(d, 0, 0), Value: math.Round(72 + rng.NormFloat64()*3), ValueMin: ptr(math.Round(48 + rng.Float64()*6)), ValueMax: ptr(math.Round(120 + rng.Float64()*45)), Source: "hae", Device: "Apple Watch"})
		add("spo2", at(d, 4, 0), 96+rng.Float64()*2.5, "Apple Watch")
		add("resp_rate", at(d, 4, 0), 14.5+rng.NormFloat64()*0.8, "Apple Watch")
		if i%7 == 3 {
			add("vo2max", at(d, 18, 0), 41.2+float64(89-i)*0.012+rng.NormFloat64()*0.3, "Apple Watch")
		}
		// weight most mornings, trending down slowly
		if rng.Float64() < 0.8 {
			weight += -0.025 + rng.NormFloat64()*0.25
			add("weight", at(d, 7, 5), weight, "Withings Body+")
			if rng.Float64() < 0.5 {
				add("body_fat", at(d, 7, 5), 21.5-float64(89-i)*0.01+rng.NormFloat64()*0.5, "Withings Body+")
			}
		}
		// water, caffeine
		add("water", at(d, 0, 0), math.Max(600, 1700+rng.NormFloat64()*500), "iPhone")
		add("caffeine", at(d, 0, 0), math.Max(0, 160+rng.NormFloat64()*70), "iPhone")
		if rng.Float64() < 0.4 {
			add("mindful_min", at(d, 21, 30), 10, "iPhone")
		}
		if rng.Float64() < 0.3 {
			samples = append(samples, store.Sample{Metric: "blood_pressure", StartAt: at(d, 8, 0), Value: math.Round(118 + rng.NormFloat64()*6), Value2: ptr(math.Round(76 + rng.NormFloat64()*4)), Unit: "mmHg", Source: "manual"})
		}
		add("time_in_daylight", at(d, 0, 0), math.Max(5, 45+rng.NormFloat64()*30), "Apple Watch")
		// workouts: Mon/Wed/Fri strength, Tue/Sat run, Sun hike sometimes
		switch wd {
		case time.Monday, time.Wednesday, time.Friday:
			if rng.Float64() < 0.85 {
				st := time.Date(d.Year(), d.Month(), d.Day(), 18, 15, 0, 0, loc)
				dur := 45 + rng.Intn(20)
				workouts = append(workouts, store.Workout{Kind: "strength", Name: "Traditional Strength Training", StartAt: st.Format(time.RFC3339), EndAt: st.Add(time.Duration(dur) * time.Minute).Format(time.RFC3339), DurationS: dur * 60, EnergyKcal: float64(dur) * 6.5, AvgHR: 118 + rng.Float64()*10, MaxHR: 150 + rng.Float64()*15, Source: "hae", Device: "Apple Watch"})
			}
		case time.Tuesday, time.Saturday:
			if rng.Float64() < 0.8 {
				st := time.Date(d.Year(), d.Month(), d.Day(), 7, 10, 0, 0, loc)
				km := 5 + rng.Float64()*4
				pace := 5.6 - float64(89-i)*0.004 + rng.NormFloat64()*0.2
				dur := km * pace
				workouts = append(workouts, store.Workout{Kind: "running", Name: "Running", StartAt: st.Format(time.RFC3339), EndAt: st.Add(time.Duration(dur*60) * time.Second).Format(time.RFC3339), DurationS: int(dur * 60), DistanceM: km * 1000, EnergyKcal: km * 68, AvgHR: 152 + rng.Float64()*8, MaxHR: 172 + rng.Float64()*10, ElevationM: 20 + rng.Float64()*60, Source: "hae", Device: "Apple Watch", Temperature: ptr(12 + rng.Float64()*12)})
			}
		case time.Sunday:
			if rng.Float64() < 0.35 {
				st := time.Date(d.Year(), d.Month(), d.Day(), 10, 0, 0, 0, loc)
				dur := 120 + rng.Intn(90)
				workouts = append(workouts, store.Workout{Kind: "hiking", Name: "Hiking", StartAt: st.Format(time.RFC3339), EndAt: st.Add(time.Duration(dur) * time.Minute).Format(time.RFC3339), DurationS: dur * 60, DistanceM: float64(dur) * 60, EnergyKcal: float64(dur) * 7, AvgHR: 125 + rng.Float64()*10, MaxHR: 160, ElevationM: 300 + rng.Float64()*400, Source: "hae", Device: "Apple Watch"})
			}
		}
	}
	if _, _, err := s.store.UpsertSamples(ctx, owner, samples); err != nil {
		return err
	}
	if _, _, err := s.store.UpsertWorkouts(ctx, owner, workouts); err != nil {
		return err
	}
	if _, _, err := s.store.UpsertSleep(ctx, owner, nights); err != nil {
		return err
	}
	st, _ := s.store.GetSettings(ctx, owner)
	st.HeightCm, st.Sex, st.BirthDate = 181, "male", "1988-05-14"
	st.WeatherEnabled, st.WeatherLat, st.WeatherLon, st.WeatherPlace = true, 43.85, 18.38, "Sarajevo"
	_ = s.store.SaveUserSettings(ctx, owner, st)
	if err := s.store.RollupAll(ctx, owner, ""); err != nil {
		return err
	}
	// medications, symptoms, a habit rule
	meds := []store.Medication{
		{Name: "Vitamin D3", Dose: "2000 IU", Form: "capsule", Times: "08:00", Days: "1234567", Active: true, Remind: true, Stock: 54, Color: "#f59e0b"},
		{Name: "Magnesium", Dose: "400 mg", Form: "tablet", Times: "21:00", Days: "1234567", Active: true, Remind: true, Stock: -1, Color: "#6366f1"},
		{Name: "Allergy tablet", Dose: "10 mg", Form: "tablet", Times: "08:00", Days: "12345", Active: true, Remind: false, Stock: 12, Color: "#22c55e", Notes: "Cetirizine, pollen season"},
	}
	for i := range meds {
		if err := s.store.CreateMedication(ctx, owner, &meds[i]); err != nil {
			return err
		}
	}
	for i := 29; i >= 1; i-- {
		d := today.AddDate(0, 0, -i).Format("2006-01-02")
		for _, m := range meds[:2] {
			if rng.Float64() < 0.9 {
				_ = s.store.LogDose(ctx, owner, &store.MedLog{MedicationID: m.ID, Date: d, Slot: m.Times, Status: "taken", TakenAt: d + "T" + m.Times + ":00+00:00"})
			}
		}
	}
	syms := []store.Symptom{
		{Name: "Headache", Severity: 3, At: today.AddDate(0, 0, -12).Format("2006-01-02") + "T15:30:00+02:00", DurationMin: 120, Note: "After a short night", Tags: "evening"},
		{Name: "Muscle soreness", Severity: 2, At: today.AddDate(0, 0, -5).Format("2006-01-02") + "T09:00:00+02:00", Tags: "legs"},
		{Name: "Allergy", Severity: 2, At: today.AddDate(0, 0, -2).Format("2006-01-02") + "T08:00:00+02:00", Note: "Sneezing, itchy eyes"},
	}
	for i := range syms {
		if err := s.store.CreateSymptom(ctx, owner, &syms[i]); err != nil {
			return err
		}
	}
	_ = s.store.CreateRule(ctx, owner, &store.HabitRule{Trigger: "workout", Match: "any", HabitID: 1, HabitName: "Exercise", Enabled: true})
	// goal hits for the history (quiet: no pushes)
	t := store.Touched{}
	for i := 89; i >= 0; i-- {
		d := today.AddDate(0, 0, -i).Format("2006-01-02")
		for _, k := range []string{"steps", "exercise_min", "sleep_asleep", "water", "stand_hours"} {
			t.Merge(store.Touched{k: {d: true}})
		}
	}
	s.checkGoalsQuiet(ctx, owner, t)
	s.log.Info("demo data seeded", "user", owner, "samples", len(samples), "workouts", len(workouts), "nights", len(nights))
	return nil
}

func ptr(v float64) *float64 { return &v }
