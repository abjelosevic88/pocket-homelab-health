package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// The day's weather comes from Open-Meteo (free, no key): the forecast API
// answers the last three months with past_days, the archive API anything
// older. One fetch a day for the home location.

const openMeteoForecast = "https://api.open-meteo.com/v1/forecast"
const openMeteoArchive = "https://archive-api.open-meteo.com/v1/archive"
const openMeteoGeocode = "https://geocoding-api.open-meteo.com/v1/search"
const dailyVars = "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,sunshine_duration,daylight_duration"

var weatherHTTP = &http.Client{Timeout: 20 * time.Second}

type openMeteoDaily struct {
	Time             []string   `json:"time"`
	WeatherCode      []*float64 `json:"weather_code"`
	TMax             []*float64 `json:"temperature_2m_max"`
	TMin             []*float64 `json:"temperature_2m_min"`
	Precip           []*float64 `json:"precipitation_sum"`
	SunshineDuration []*float64 `json:"sunshine_duration"`
	DaylightDuration []*float64 `json:"daylight_duration"`
}

func parseRFC(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

// fetchWeather asks Open-Meteo for the days from..to at a place.
func fetchWeather(ctx context.Context, lat, lon float64, from, to, place string) ([]store.WeatherDay, error) {
	today := store.Today()
	if to > today {
		to = today
	}
	if from > to {
		return nil, nil
	}
	cut := time.Now().AddDate(0, 0, -85).Format("2006-01-02")
	var out []store.WeatherDay
	if from < cut {
		archTo := to
		if archTo >= cut {
			archTo = time.Now().AddDate(0, 0, -86).Format("2006-01-02")
		}
		days, err := fetchOpenMeteo(ctx, openMeteoArchive, lat, lon, from, archTo, place)
		if err != nil {
			return nil, err
		}
		out = append(out, days...)
		from = cut
	}
	if from <= to {
		days, err := fetchOpenMeteo(ctx, openMeteoForecast, lat, lon, from, to, place)
		if err != nil {
			return nil, err
		}
		out = append(out, days...)
	}
	return out, nil
}

func fetchOpenMeteo(ctx context.Context, base string, lat, lon float64, from, to, place string) ([]store.WeatherDay, error) {
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%.4f", lat))
	q.Set("longitude", fmt.Sprintf("%.4f", lon))
	q.Set("daily", dailyVars)
	q.Set("timezone", "auto")
	q.Set("start_date", from)
	q.Set("end_date", to)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pocket-health/"+Version)
	res, err := weatherHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open-meteo: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var e struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Reason == "" {
			e.Reason = res.Status
		}
		return nil, fmt.Errorf("open-meteo: %s", e.Reason)
	}
	var body struct {
		Daily openMeteoDaily `json:"daily"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("open-meteo: %w", err)
	}
	now := store.Now()
	var out []store.WeatherDay
	at := func(a []*float64, i int) *float64 {
		if i < len(a) && a[i] != nil {
			v := math.Round(*a[i]*10) / 10
			return &v
		}
		return nil
	}
	for i, d := range body.Daily.Time {
		code := -1
		if c := at(body.Daily.WeatherCode, i); c != nil {
			code = int(*c)
		}
		if code < 0 && at(body.Daily.TMax, i) == nil {
			continue
		}
		w := store.WeatherDay{Date: d, Code: code, Condition: store.ConditionFromCode(code), TMax: at(body.Daily.TMax, i), TMin: at(body.Daily.TMin, i),
			PrecipMM: at(body.Daily.Precip, i), Lat: lat, Lon: lon, Place: place, FetchedAt: now}
		if s := at(body.Daily.SunshineDuration, i); s != nil {
			h := math.Round(*s/360) / 10
			w.SunshineH = &h
		}
		if s := at(body.Daily.DaylightDuration, i); s != nil {
			h := math.Round(*s/360) / 10
			w.DaylightH = &h
		}
		out = append(out, w)
	}
	return out, nil
}

// refreshWeather fetches the days in from..to at the home location.
func (s *Server) refreshWeather(ctx context.Context, uid int64, st store.Settings, from, to string) (int, error) {
	if !st.WeatherEnabled || (st.WeatherLat == 0 && st.WeatherLon == 0) {
		return 0, fmt.Errorf("weather is off or the home location is not set")
	}
	if to > store.Today() {
		to = store.Today()
	}
	days, err := fetchWeather(ctx, st.WeatherLat, st.WeatherLon, from, to, st.WeatherPlace)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, w := range days {
		if err := s.store.UpsertWeather(ctx, uid, w); err != nil {
			return n, err
		}
		n++
	}
	_ = s.store.SetSetting(ctx, uid, "weather_last", store.Now())
	return n, nil
}

// backfillWeather fetches every day since the first data that has no weather yet (two years at most).
func (s *Server) backfillWeather(ctx context.Context, uid int64, st store.Settings) (int, error) {
	from := s.store.FirstDate(ctx, uid)
	if from == "" {
		from = time.Now().AddDate(0, -3, 0).Format("2006-01-02")
	}
	if floor := time.Now().AddDate(-2, 0, 0).Format("2006-01-02"); from < floor {
		from = floor
	}
	missing, err := s.store.MissingWeatherDays(ctx, uid, from, store.Today())
	if err != nil || len(missing) == 0 {
		return 0, err
	}
	return s.refreshWeather(ctx, uid, st, missing[0], missing[len(missing)-1])
}

// RunWeather is called every 15 minutes per user: every three hours it
// refreshes today and yesterday, and fills in the missing days once.
func (s *Server) RunWeather(ctx context.Context, uid int64) {
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil || !st.WeatherEnabled || (st.WeatherLat == 0 && st.WeatherLon == 0) {
		return
	}
	if st.WeatherLast != "" {
		if last, err := time.Parse(time.RFC3339Nano, st.WeatherLast); err == nil && time.Since(last) < 3*time.Hour {
			return
		}
	}
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := s.refreshWeather(ctx, uid, st, yesterday, store.Today()); err != nil {
		s.log.Warn("weather refresh failed", "user", uid, "err", err)
		_ = s.store.SetSetting(ctx, uid, "weather_last", store.Now())
		return
	}
	if n, err := s.backfillWeather(ctx, uid, st); err != nil {
		s.log.Warn("weather backfill failed", "user", uid, "err", err)
	} else if n > 0 {
		s.log.Info("weather backfilled", "user", uid, "days", n)
	}
}

func (s *Server) handleWeather(w http.ResponseWriter, r *http.Request) {
	from, to := dateRange(r, 30)
	days, err := s.store.Weather(r.Context(), actorID(r.Context()), from, to)
	if err != nil {
		s.fail(w, err, "weather")
		return
	}
	list := make([]store.WeatherDay, 0, len(days))
	for _, d := range days {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Date < list[j].Date })
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRefreshWeather(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	n, err := s.refreshWeather(ctx, uid, st, yesterday, store.Today())
	if err == nil {
		var more int
		more, err = s.backfillWeather(ctx, uid, st)
		n += more
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": n})
}

// handleGeocode looks a place name up (Open-Meteo's geocoder).
func (s *Server) handleGeocode(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	v := url.Values{}
	v.Set("name", q)
	v.Set("count", "8")
	v.Set("language", "en")
	v.Set("format", "json")
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, openMeteoGeocode+"?"+v.Encode(), nil)
	req.Header.Set("User-Agent", "pocket-health/"+Version)
	res, err := weatherHTTP.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "geocoder: "+err.Error())
		return
	}
	defer res.Body.Close()
	var body struct {
		Results []struct {
			Name     string  `json:"name"`
			Country  string  `json:"country"`
			Admin1   string  `json:"admin1"`
			Lat      float64 `json:"latitude"`
			Lon      float64 `json:"longitude"`
			Timezone string  `json:"timezone"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadGateway, "geocoder: "+err.Error())
		return
	}
	type place struct {
		Name     string  `json:"name"`
		Label    string  `json:"label"`
		Lat      float64 `json:"lat"`
		Lon      float64 `json:"lon"`
		Timezone string  `json:"timezone"`
	}
	out := []place{}
	for _, x := range body.Results {
		label := x.Name
		if x.Admin1 != "" && x.Admin1 != x.Name {
			label += ", " + x.Admin1
		}
		if x.Country != "" {
			label += ", " + x.Country
		}
		out = append(out, place{Name: x.Name, Label: label, Lat: x.Lat, Lon: x.Lon, Timezone: x.Timezone})
	}
	writeJSON(w, http.StatusOK, out)
}
