package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// ---- events without Pocket Core ----
//
// Every event Health publishes on the Core bus can also go straight to a
// webhook of the person's own (POST, JSON, signed with their secret), and a new
// workout can be a push through their notifier (ntfy, Apprise, JSON). Both work
// with or without Pocket Core.

// webhookBody is what a webhook receives.
type webhookBody struct {
	Event   string         `json:"event"`
	Subject string         `json:"subject"`
	SentAt  string         `json:"sent_at"`
	Source  string         `json:"source"`
	URL     string         `json:"url,omitempty"`
	Data    map[string]any `json:"data"`
}

// recentEvent says whether an event about a workout is new enough to deliver
// outside Core: an Apple Health export of years does not become a flood.
func recentWorkout(payload map[string]any, within time.Duration) bool {
	start, _ := payload["start_at"].(string)
	t, err := time.Parse(time.RFC3339, start)
	return err == nil && time.Since(t) < within
}

// deliverLocal hands an event to the person's webhook and notifier, in the
// background.
func (s *Server) deliverLocal(uid int64, topic, subject string, payload map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	st, err := s.store.GetSettings(ctx, uid)
	cancel()
	if err != nil {
		return
	}
	isWorkout := topic == "health.workout_recorded"
	hook := st.WebhookURL != "" && contains(st.WebhookEvents, topic) && (!isWorkout || recentWorkout(payload, 7*24*time.Hour))
	push := isWorkout && st.NotifyWorkouts && st.NotifyURL != "" && recentWorkout(payload, 48*time.Hour)
	if !hook && !push {
		return
	}
	data := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		data[k] = v
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if hook {
			if isWorkout && st.WebhookTrack {
				if id, ok := data["workout_id"].(int64); ok {
					if t, err := s.store.WorkoutTrack(ctx, id); err == nil && t != nil {
						tv := buildTrack(t, "km", 0, s.zonesFor(ctx, uid))
						tv.Route = [][4]float64{}
						data["track"] = tv
					}
				}
			}
			var err error
			for try := 0; try < 3; try++ {
				if err = s.sendWebhook(ctx, st, topic, subject, data); err == nil {
					break
				}
				select {
				case <-ctx.Done():
				case <-time.After(time.Duration(5<<try) * time.Second):
				}
			}
			if err != nil {
				s.log.Warn("webhook failed", "user", uid, "event", topic, "err", err)
			}
		}
		if push {
			if err := notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, workoutMessage(data, s.cfg.BaseURL)); err != nil {
				s.log.Warn("workout push failed", "user", uid, "err", err)
			}
		}
	}()
}

// sendWebhook POSTs one event. With a secret the body is signed:
// X-Pocket-Signature: sha256=<hex HMAC-SHA256 of the body>.
func (s *Server) sendWebhook(ctx context.Context, st store.Settings, topic, subject string, data map[string]any) error {
	body := webhookBody{Event: topic, Subject: subject, SentAt: time.Now().UTC().Format(time.RFC3339), Source: "pocket-health", Data: data}
	if id, ok := data["workout_id"].(int64); ok && s.cfg.BaseURL != "" {
		body.URL = fmt.Sprintf("%s/workouts/%d", s.cfg.BaseURL, id)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, st.WebhookURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Pocket-Health/"+Version)
	req.Header.Set("X-Pocket-Event", topic)
	if st.WebhookSecret != "" {
		m := hmac.New(sha256.New, []byte(st.WebhookSecret))
		m.Write(b)
		req.Header.Set("X-Pocket-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	res, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("webhook HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// workoutMessage is the push for a new workout: "Tempo 5×1 km · 8.2 km · 42:10 · 151 bpm".
func workoutMessage(d map[string]any, base string) notify.Message {
	name, _ := d["name"].(string)
	if name == "" {
		k, _ := d["kind"].(string)
		name = store.WorkoutKindName(k)
	}
	parts := []string{}
	if v, _ := d["distance_m"].(float64); v >= 10 {
		parts = append(parts, fmt.Sprintf("%.2f km", v/1000))
	}
	if v, _ := d["duration_s"].(int); v > 0 {
		parts = append(parts, clockText(v))
	}
	if v, _ := d["avg_hr"].(float64); v > 0 {
		parts = append(parts, fmt.Sprintf("%.0f bpm", v))
	}
	if v, _ := d["energy_kcal"].(float64); v > 0 {
		parts = append(parts, fmt.Sprintf("%.0f kcal", v))
	}
	m := notify.Message{Title: "Workout: " + name, Body: strings.Join(parts, " · "), Priority: "success", Tags: []string{"runner"}}
	if m.Body == "" {
		m.Body = "Recorded."
	}
	if id, ok := d["workout_id"].(int64); ok && base != "" {
		m.Link = fmt.Sprintf("%s/workouts/%d", base, id)
	}
	return m
}

func clockText(s int) string {
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
