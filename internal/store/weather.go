package store

import (
	"context"
	"database/sql"
)

// WeatherDay is one day's weather at the home location.
type WeatherDay struct {
	Date      string   `json:"date"`
	Code      int      `json:"code"`
	Condition string   `json:"condition"`
	TMax      *float64 `json:"t_max"`
	TMin      *float64 `json:"t_min"`
	PrecipMM  *float64 `json:"precip_mm"`
	SunshineH *float64 `json:"sunshine_h"`
	DaylightH *float64 `json:"daylight_h"`
	Lat       float64  `json:"lat"`
	Lon       float64  `json:"lon"`
	Place     string   `json:"place"`
	FetchedAt string   `json:"fetched_at"`
}

// Conditions in the order the insights show them.
var Conditions = []string{"clear", "partly", "cloudy", "fog", "rain", "snow", "storm"}

// ConditionLabel is the word shown for a condition.
func ConditionLabel(c string) string {
	switch c {
	case "clear":
		return "Clear"
	case "partly":
		return "Partly cloudy"
	case "cloudy":
		return "Overcast"
	case "fog":
		return "Fog"
	case "rain":
		return "Rain"
	case "snow":
		return "Snow"
	case "storm":
		return "Thunderstorm"
	}
	return "Unknown"
}

// ConditionFromCode groups a WMO weather code into a condition.
func ConditionFromCode(code int) string {
	switch {
	case code == 0:
		return "clear"
	case code == 1 || code == 2:
		return "partly"
	case code == 3:
		return "cloudy"
	case code == 45 || code == 48:
		return "fog"
	case code >= 51 && code <= 67, code >= 80 && code <= 82:
		return "rain"
	case code >= 71 && code <= 77, code == 85 || code == 86:
		return "snow"
	case code >= 95:
		return "storm"
	}
	return ""
}

const weatherCols = `date, code, condition, t_max, t_min, precip_mm, sunshine_h, daylight_h, lat, lon, place, fetched_at`

func scanWeather(row interface{ Scan(dest ...any) error }) (WeatherDay, error) {
	var w WeatherDay
	var tmax, tmin, pr, sun, day sql.NullFloat64
	if err := row.Scan(&w.Date, &w.Code, &w.Condition, &tmax, &tmin, &pr, &sun, &day, &w.Lat, &w.Lon, &w.Place, &w.FetchedAt); err != nil {
		return w, err
	}
	w.TMax, w.TMin, w.PrecipMM, w.SunshineH, w.DaylightH = nf(tmax), nf(tmin), nf(pr), nf(sun), nf(day)
	return w, nil
}

// Weather returns the user's stored days in a range, by date.
func (s *Store) Weather(ctx context.Context, uid int64, from, to string) (map[string]WeatherDay, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+weatherCols+` FROM weather_days WHERE user_id = ? AND date >= ? AND date <= ? ORDER BY date`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]WeatherDay{}
	for rows.Next() {
		w, err := scanWeather(rows)
		if err != nil {
			return nil, err
		}
		out[w.Date] = w
	}
	return out, rows.Err()
}

// UpsertWeather writes a fetched day.
func (s *Store) UpsertWeather(ctx context.Context, uid int64, w WeatherDay) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO weather_days (user_id, `+weatherCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, date) DO UPDATE SET code = excluded.code, condition = excluded.condition,
			t_max = excluded.t_max, t_min = excluded.t_min, precip_mm = excluded.precip_mm, sunshine_h = excluded.sunshine_h, daylight_h = excluded.daylight_h,
			lat = excluded.lat, lon = excluded.lon, place = excluded.place, fetched_at = excluded.fetched_at`,
		uid, w.Date, w.Code, w.Condition, pf(w.TMax), pf(w.TMin), pf(w.PrecipMM), pf(w.SunshineH), pf(w.DaylightH), w.Lat, w.Lon, w.Place, w.FetchedAt)
	return err
}

// MissingWeatherDays lists the days in a range with no fetched weather.
func (s *Store) MissingWeatherDays(ctx context.Context, uid int64, from, to string) ([]string, error) {
	have, err := s.Weather(ctx, uid, from, to)
	if err != nil {
		return nil, err
	}
	start, err := ParseDate(from)
	if err != nil {
		return nil, err
	}
	end, err := ParseDate(to)
	if err != nil {
		return nil, err
	}
	var out []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		if w, ok := have[ds]; !ok || w.FetchedAt == "" {
			out = append(out, ds)
		}
	}
	return out, nil
}

// SetSetting writes one personal setting key as a JSON string value.
func (s *Store) SetSetting(ctx context.Context, uid int64, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?) ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, uid, key, `"`+value+`"`)
	return err
}
