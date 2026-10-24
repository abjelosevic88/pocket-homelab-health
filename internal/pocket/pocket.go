// Package pocket talks to sibling Pocket apps: Pocket Habits (a workout or a
// met goal ticks a habit) and Pocket Movement (the recorded route of a
// workout). Same bearer-token JSON API everywhere.
package pocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and token are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

// ErrNotFound is a 404 from the sibling app.
var ErrNotFound = errors.New("not found")

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, v any) error {
	if !c.Configured() {
		return errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errors.New("token rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		_ = json.Unmarshal(b, &e)
		if e.Error != "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// Health is the connection test for any Pocket app: /healthz plus a token check.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if !c.Configured() {
		return "", errors.New("not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var me map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, nil, &me); err != nil {
		return out.Version, err
	}
	return out.Version, nil
}

// ---------------- Pocket Habits ----------------

// Habit is a Pocket Habits habit.
type Habit struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Days  string `json:"days"`
}

// Habits lists the person's habits.
func (c *Client) Habits(ctx context.Context) ([]Habit, error) {
	var out []Habit
	if err := c.do(ctx, http.MethodGet, "/api/v1/habits", nil, nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Habit{}
	}
	return out, nil
}

// MarkHabit ticks a habit on a date (status 1 = done).
func (c *Client) MarkHabit(ctx context.Context, habitID int64, date string) error {
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/habits/%d/entries/%s", habitID, date), nil, map[string]any{"status": 1}, nil)
}

// ---------------- Pocket Movement (Dawarich-compatible points API) ----------------

// Point is one recorded position.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
	TS  int64   `json:"ts"`
}

// Points reads the positions recorded between from and to, oldest first.
// The token goes as a bearer (Pocket Core link tokens, Movement's own tokens)
// and as api_key (Dawarich itself).
func (c *Client) Points(ctx context.Context, from, to time.Time, max int) ([]Point, error) {
	if !c.Configured() {
		return nil, errors.New("not configured")
	}
	var out []Point
	for page := 1; page < 200; page++ {
		q := url.Values{"start_at": {from.UTC().Format(time.RFC3339)}, "end_at": {to.UTC().Format(time.RFC3339)}, "order": {"asc"}, "slim": {"true"}, "per_page": {"5000"}, "page": {fmt.Sprint(page)}, "api_key": {c.Token}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/points?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Forwarded-Proto", "https")
		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		var raw []struct {
			Latitude  json.Number `json:"latitude"`
			Longitude json.Number `json:"longitude"`
			Timestamp int64       `json:"timestamp"`
		}
		if res.StatusCode == http.StatusUnauthorized {
			res.Body.Close()
			return nil, errors.New("token rejected (401)")
		}
		if res.StatusCode >= 300 {
			res.Body.Close()
			return nil, fmt.Errorf("HTTP %d", res.StatusCode)
		}
		err = json.NewDecoder(res.Body).Decode(&raw)
		total := res.Header.Get("X-Total-Pages")
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, p := range raw {
			lat, _ := p.Latitude.Float64()
			lng, _ := p.Longitude.Float64()
			out = append(out, Point{Lat: lat, Lng: lng, TS: p.Timestamp})
			if max > 0 && len(out) >= max {
				return out, nil
			}
		}
		if n, _ := strconv.Atoi(total); page >= n || len(raw) == 0 {
			break
		}
	}
	return out, nil
}
