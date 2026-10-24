package store

import "strings"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Sample is one measurement in the metric's canonical unit.
type Sample struct {
	ID       int64    `json:"id"`
	UserID   int64    `json:"-"`
	Metric   string   `json:"metric"`
	StartAt  string   `json:"start_at"`
	EndAt    string   `json:"end_at"`
	Date     string   `json:"date"`
	Value    float64  `json:"value"`
	Value2   *float64 `json:"value2,omitempty"`
	ValueMin *float64 `json:"value_min,omitempty"`
	ValueMax *float64 `json:"value_max,omitempty"`
	Unit     string   `json:"unit"`
	Source   string   `json:"source"`
	Device   string   `json:"device"`
	Note     string   `json:"note"`
}

// DailyPoint is a day's rolled-up number for a metric.
type DailyPoint struct {
	Date   string   `json:"date"`
	Value  float64  `json:"value"`
	Value2 *float64 `json:"value2,omitempty"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	N      int      `json:"n"`
}

// Workout is one session.
type Workout struct {
	ID            int64    `json:"id"`
	UserID        int64    `json:"-"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	StartAt       string   `json:"start_at"`
	EndAt         string   `json:"end_at"`
	Date          string   `json:"date"`
	DurationS     int      `json:"duration_s"`
	DistanceM     float64  `json:"distance_m"`
	EnergyKcal    float64  `json:"energy_kcal"`
	AvgHR         float64  `json:"avg_hr"`
	MaxHR         float64  `json:"max_hr"`
	ElevationM    float64  `json:"elevation_m"`
	Steps         int      `json:"steps"`
	Temperature   *float64 `json:"temperature"`
	Humidity      *float64 `json:"humidity"`
	Source        string   `json:"source"`
	Device        string   `json:"device"`
	ExternalID    string   `json:"external_id"`
	Notes         string   `json:"notes"`
	HabitID       *int64   `json:"habit_id"`
	HabitMarkedAt string   `json:"habit_marked_at"`
	HabitError    string   `json:"habit_error"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// SleepSession is one night; Date is the morning.
type SleepSession struct {
	ID      int64  `json:"id"`
	UserID  int64  `json:"-"`
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
	Date    string `json:"date"`
	InBedS  int    `json:"in_bed_s"`
	AsleepS int    `json:"asleep_s"`
	CoreS   int    `json:"core_s"`
	DeepS   int    `json:"deep_s"`
	RemS    int    `json:"rem_s"`
	AwakeS  int    `json:"awake_s"`
	Source  string `json:"source"`
	Device  string `json:"device"`
	Note    string `json:"note"`
}

// Goal is a target per metric.
type Goal struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"-"`
	Metric    string  `json:"metric"`
	Target    float64 `json:"target"`
	Direction string  `json:"direction"` // at_least | at_most
	Period    string  `json:"period"`    // day | week
	Active    bool    `json:"active"`
	CreatedAt string  `json:"created_at"`
}

// Medication is something taken on a schedule.
type Medication struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Name      string `json:"name"`
	Dose      string `json:"dose"`
	Form      string `json:"form"`
	Times     string `json:"times"`
	Days      string `json:"days"`
	Notes     string `json:"notes"`
	Active    bool   `json:"active"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Remind    bool   `json:"remind"`
	Stock     int    `json:"stock"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	// computed for a day
	Slots []MedSlot `json:"slots,omitempty"`
	// computed: adherence over the last 30 days (0–100), -1 unknown
	Adherence int `json:"adherence"`
}

// MedSlot is one scheduled dose on a day with what happened.
type MedSlot struct {
	Time    string `json:"time"`
	Status  string `json:"status"` // "" (due) | taken | skipped
	TakenAt string `json:"taken_at"`
	LogID   int64  `json:"log_id"`
	Note    string `json:"note"`
}

// MedLog is one dose taken or skipped.
type MedLog struct {
	ID           int64  `json:"id"`
	MedicationID int64  `json:"medication_id"`
	Name         string `json:"name"`
	Dose         string `json:"dose"`
	Date         string `json:"date"`
	Slot         string `json:"slot"`
	TakenAt      string `json:"taken_at"`
	Status       string `json:"status"`
	Note         string `json:"note"`
}

// Symptom is one logged symptom.
type Symptom struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"-"`
	Name        string `json:"name"`
	Severity    int    `json:"severity"`
	At          string `json:"at"`
	Date        string `json:"date"`
	DurationMin int    `json:"duration_min"`
	Note        string `json:"note"`
	Tags        string `json:"tags"`
	CreatedAt   string `json:"created_at"`
}

// HabitRule ticks a Pocket Habits habit when something happens here.
type HabitRule struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Trigger   string `json:"trigger"` // workout | goal
	Match     string `json:"match"`   // workout kind or "any"; or a metric key
	HabitID   int64  `json:"habit_id"`
	HabitName string `json:"habit_name"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

// Import is one background import job.
type Import struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	Kind       string `json:"kind"`
	File       string `json:"file"`
	Status     string `json:"status"`
	Progress   string `json:"progress"`
	Samples    int    `json:"samples"`
	Workouts   int    `json:"workouts"`
	Sleep      int    `json:"sleep"`
	Skipped    int    `json:"skipped"`
	Error      string `json:"error"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections. Connection modes: "core" (an app link through Pocket
// Core), "direct" (URL + token) or "off"; "" = not chosen yet.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	Units      string  `json:"units"` // metric | imperial
	HeightCm   float64 `json:"height_cm"`
	BirthDate  string  `json:"birth_date"`
	Sex        string  `json:"sex"` // "" | female | male | other
	DateFormat string  `json:"date_format"`
	TimeFormat string  `json:"time_format"`
	WeekStart  int     `json:"week_start"` // 1 = Monday, 7 = Sunday
	// Dashboard tiles on Home, in order (metric keys).
	HomeMetrics []string `json:"home_metrics"`

	NotifyURL      string `json:"notify_url"`
	NotifyFormat   string `json:"notify_format"` // apprise | ntfy | json
	NotifyGoals    bool   `json:"notify_goals"`
	NotifyWeekly   bool   `json:"notify_weekly"`
	NotifyMeds     bool   `json:"notify_meds"`
	NotifyInactive bool   `json:"notify_inactive"` // no data from the phone for a day
	NotifyHour     int    `json:"notify_hour"`
	WeeklyDay      int    `json:"weekly_day"` // ISO weekday the weekly report goes out

	HabitsMode      string `json:"habits_mode"`
	HabitsURL       string `json:"habits_url"`
	HabitsToken     string `json:"habits_token"`
	HabitsPublicURL string `json:"habits_public_url"`

	MovementMode      string `json:"movement_mode"`
	MovementURL       string `json:"movement_url"`
	MovementToken     string `json:"movement_token"`
	MovementPublicURL string `json:"movement_public_url"`

	// AI for the weekly recap and questions: Pocket AI through Core, or an
	// OpenAI-compatible API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`

	WeatherEnabled bool    `json:"weather_enabled"`
	WeatherLat     float64 `json:"weather_lat"`
	WeatherLon     float64 `json:"weather_lon"`
	WeatherPlace   string  `json:"weather_place"`
	WeatherLast    string  `json:"weather_last"`

	// Which source wins when two devices report the same day of a summed
	// metric (Apple Health export: iPhone and Watch both count steps).
	PreferDevice string `json:"prefer_device"`
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }

// WorkoutKinds people can pick, with the Apple names they map from.
var WorkoutKinds = []struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}{
	{"running", "Running", "run"}, {"walking", "Walking", "footprints"}, {"hiking", "Hiking", "mountain"}, {"cycling", "Cycling", "bike"},
	{"swimming", "Swimming", "swim"}, {"strength", "Strength training", "dumbbell"}, {"hiit", "HIIT", "zap"}, {"yoga", "Yoga", "yoga"},
	{"pilates", "Pilates", "yoga"}, {"core", "Core training", "dumbbell"}, {"stretching", "Stretching / mobility", "yoga"}, {"rowing", "Rowing", "activity"},
	{"elliptical", "Elliptical", "activity"}, {"stairs", "Stair climbing", "stairs"}, {"football", "Football", "ball"}, {"basketball", "Basketball", "ball"},
	{"tennis", "Tennis / padel", "ball"}, {"table_tennis", "Table tennis", "ball"}, {"volleyball", "Volleyball", "ball"}, {"climbing", "Climbing", "mountain"},
	{"skiing", "Skiing / snowboarding", "mountain"}, {"skating", "Skating", "activity"}, {"dance", "Dance", "music"}, {"martial_arts", "Martial arts", "activity"},
	{"boxing", "Boxing / kickboxing", "activity"}, {"golf", "Golf", "ball"}, {"cooldown", "Cooldown", "yoga"}, {"functional", "Functional training", "dumbbell"},
	{"cross_training", "Cross training", "dumbbell"}, {"mind_body", "Mind & body", "brain"}, {"wheelchair", "Wheelchair", "activity"}, {"other", "Other", "activity"},
}

// WorkoutKindName is the display name of a kind.
func WorkoutKindName(key string) string {
	for _, k := range WorkoutKinds {
		if k.Key == key {
			return k.Name
		}
	}
	return strings.ToUpper(key[:1]) + strings.ReplaceAll(key[1:], "_", " ")
}

// WorkoutKindFromApple maps an HKWorkoutActivityType… or a Health Auto
// Export workout name to a kind.
func WorkoutKindFromApple(name string) string {
	n := strings.ToLower(strings.TrimPrefix(name, "HKWorkoutActivityType"))
	n = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(n)
	switch {
	case strings.Contains(n, "running") || n == "run":
		return "running"
	case strings.Contains(n, "walking") || n == "walk":
		return "walking"
	case strings.Contains(n, "hiking"):
		return "hiking"
	case strings.Contains(n, "cycling") || strings.Contains(n, "bike") || strings.Contains(n, "biking"):
		return "cycling"
	case strings.Contains(n, "swim"):
		return "swimming"
	case strings.Contains(n, "traditionalstrength") || strings.Contains(n, "strength") || strings.Contains(n, "weight"):
		return "strength"
	case strings.Contains(n, "highintensity") || strings.Contains(n, "hiit"):
		return "hiit"
	case strings.Contains(n, "yoga"):
		return "yoga"
	case strings.Contains(n, "pilates"):
		return "pilates"
	case strings.Contains(n, "coretraining"):
		return "core"
	case strings.Contains(n, "flexibility") || strings.Contains(n, "stretch"):
		return "stretching"
	case strings.Contains(n, "rowing"):
		return "rowing"
	case strings.Contains(n, "elliptical"):
		return "elliptical"
	case strings.Contains(n, "stair"):
		return "stairs"
	case strings.Contains(n, "soccer") || strings.Contains(n, "football"):
		return "football"
	case strings.Contains(n, "basketball"):
		return "basketball"
	case strings.Contains(n, "tabletennis"):
		return "table_tennis"
	case strings.Contains(n, "tennis") || strings.Contains(n, "padel") || strings.Contains(n, "pickleball") || strings.Contains(n, "racquet") || strings.Contains(n, "squash") || strings.Contains(n, "badminton"):
		return "tennis"
	case strings.Contains(n, "volleyball"):
		return "volleyball"
	case strings.Contains(n, "climbing"):
		return "climbing"
	case strings.Contains(n, "ski") || strings.Contains(n, "snowboard") || strings.Contains(n, "snowsports"):
		return "skiing"
	case strings.Contains(n, "skating"):
		return "skating"
	case strings.Contains(n, "dance") || strings.Contains(n, "cardiodance"):
		return "dance"
	case strings.Contains(n, "martialarts") || strings.Contains(n, "taichi"):
		return "martial_arts"
	case strings.Contains(n, "boxing") || strings.Contains(n, "kickboxing"):
		return "boxing"
	case strings.Contains(n, "golf"):
		return "golf"
	case strings.Contains(n, "cooldown"):
		return "cooldown"
	case strings.Contains(n, "functional"):
		return "functional"
	case strings.Contains(n, "crosstraining") || strings.Contains(n, "mixedcardio"):
		return "cross_training"
	case strings.Contains(n, "mindandbody") || strings.Contains(n, "mindbody"):
		return "mind_body"
	case strings.Contains(n, "wheelchair"):
		return "wheelchair"
	}
	for _, k := range WorkoutKinds {
		if k.Key == n {
			return n
		}
	}
	return "other"
}
