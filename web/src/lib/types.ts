export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'

export interface Settings {
  core_configured: boolean
  core_registered?: boolean
  title: string; units: 'metric' | 'imperial'; height_cm: number; birth_date: string; sex: string
  date_format: string; time_format: string; week_start: number; home_metrics: string[]
  notify_url: string; notify_format: string; notify_goals: boolean; notify_weekly: boolean; notify_meds: boolean; notify_inactive: boolean
  notify_hour: number; weekly_day: number; notify_configured: boolean
  habits_mode: ConnMode; habits_via: ConnVia; habits_detail: string; habits_open_url: string
  habits_url: string; habits_token: string; habits_public_url: string
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  weather_enabled: boolean; weather_lat: number; weather_lon: number; weather_place: string; weather_last: string
  prefer_device: string
  max_upload_bytes: number; setup_complete: boolean; base_url: string
}

export type Agg = 'sum' | 'avg' | 'last' | 'max' | 'min'
export type Category = 'activity' | 'body' | 'heart' | 'vitals' | 'sleep' | 'nutrition' | 'mobility' | 'mind' | 'hearing' | 'other'

export interface Metric {
  key: string; name: string; unit: string; imperial_unit: string; agg: Agg; category: Category; decimals: number
  icon: string; color: string; higher_is_better: boolean | null; pair: boolean; manual: boolean; goal_dir: '' | 'at_least' | 'at_most'
}
export interface Catalog { metrics: Metric[]; categories: { key: Category; name: string }[]; workout_kinds: WorkoutKind[]; manual: string[] }
export interface WorkoutKind { key: string; name: string; icon: string }

export interface DailyPoint { date: string; value: number; value2?: number; min?: number; max?: number; n: number }
export interface Sample { id: number; metric: string; start_at: string; end_at: string; date: string; value: number; value2?: number; value_min?: number; value_max?: number; unit: string; source: string; device: string; note: string }

export interface Goal { id: number; metric: string; target: number; direction: 'at_least' | 'at_most'; period: 'day' | 'week'; active: boolean; created_at: string }
export interface GoalView extends Goal { metric_info: Metric; today: number | null; pct: number; met: boolean; streak: number; best: number; last30: number }

export interface MetricView extends Metric { days: number; first: string; last: string; latest: number; latest2?: number; goal?: Goal; spark: (number | null)[]; change?: number }

export interface MetricPage {
  metric: Metric; from: string; to: string; points: DailyPoint[]; weekly: DailyPoint[]
  stats: { days: number; avg?: number; min?: number; max?: number; sum?: number; first?: number; last?: number; prev_avg?: number; change_pct?: number; per_week?: number }
  goal: Goal | null; goal_hits: Record<string, boolean> | null; streak: number; best_streak: number; latest: DailyPoint | null; recent: Sample[]
  weekday: { weekday: number; name: string; avg: number; n: number }[]
}

export interface Workout {
  id: number; kind: string; name: string; start_at: string; end_at: string; date: string; duration_s: number; distance_m: number; energy_kcal: number
  avg_hr: number; max_hr: number; elevation_m: number; steps: number; temperature: number | null; humidity: number | null
  source: string; device: string; external_id: string; notes: string; habit_id: number | null; habit_marked_at: string; habit_error: string; created_at: string; updated_at: string
}
export interface WorkoutKindStat { kind: string; name: string; count: number; duration_s: number; distance_m: number; energy_kcal: number }
export interface WorkoutStats { from: string; to: string; by_kind: WorkoutKindStat[]; days: Record<string, DailyPoint>; weekly: Record<string, number> }

export interface SleepSession { id: number; start_at: string; end_at: string; date: string; in_bed_s: number; asleep_s: number; core_s: number; deep_s: number; rem_s: number; awake_s: number; source: string; device: string; note: string }

export interface MedSlot { time: string; status: '' | 'taken' | 'skipped'; taken_at: string; log_id: number; note: string }
export interface Medication {
  id: number; name: string; dose: string; form: string; times: string; days: string; notes: string; active: boolean; start_date: string; end_date: string
  remind: boolean; stock: number; color: string; created_at: string; updated_at: string; slots?: MedSlot[]; adherence: number
}
export interface MedLog { id: number; medication_id: number; name: string; dose: string; date: string; slot: string; taken_at: string; status: 'taken' | 'skipped'; note: string }
export interface Symptom { id: number; name: string; severity: number; at: string; date: string; duration_min: number; note: string; tags: string; created_at: string }

export interface HabitRule { id: number; trigger: 'workout' | 'goal'; match: string; habit_id: number; habit_name: string; enabled: boolean; created_at: string }
export interface Habit { id: number; name: string; color: string; days: string }

export interface Tile { metric: Metric; value: number | null; value2?: number; date: string; goal?: Goal; pct: number; spark: (number | null)[]; streak: number; is_today: boolean }
export interface WeatherDay { date: string; code: number; condition: string; t_max: number | null; t_min: number | null; precip_mm: number | null; sunshine_h: number | null; daylight_h: number | null; place: string }
export interface HomeData {
  date: string; tiles: Tile[]; workouts: Workout[]; nights: SleepSession[]; medications: Medication[]; symptoms: Symptom[]
  goals_met: number; goals_total: number; goals_hit: Record<string, boolean>; last_sync: string; has_data: boolean; metrics_count: number; weather: WeatherDay | null; units: string
}

export interface Trend { metric: Metric; avg: number; prev_avg: number; change_pct: number; best: DailyPoint; worst: DailyPoint; days: number; weekly: DailyPoint[]; per_week: number }
export interface Correlation { a: string; b: string; a_name: string; b_name: string; r: number; n: number; text: string }
export interface Insights {
  from: string; to: string; days: number; trends: Trend[]; patterns: Record<string, { weekday: number; name: string; avg: number; n: number }[]>
  workouts: { by_kind: WorkoutKindStat[]; count: number; prev_count: number; minutes: number; prev_minutes: number }
  sleep: { nights: number; bed_avg?: string; wake_avg?: string; bed_sd_min?: number; wake_sd_min?: number; asleep_avg_min?: number; asleep_sd_min?: number; score?: number }
  correlations: Correlation[]
  weather: { days: number; by_condition: Record<string, unknown>[]; by_temperature: Record<string, unknown>[]; by_rain: Record<string, unknown>[]; metrics: string[] } | null
  goals: { metric: string; name: string; target: number; direction: string; met: number; days: number; pct: number; streak: number; best: number }[]
}

export interface Import { id: number; kind: string; file: string; status: 'running' | 'done' | 'error'; progress: string; samples: number; workouts: number; sleep: number; skipped: number; error: string; started_at: string; finished_at: string }
export interface Sources { last_sync: Record<string, string>; devices: string[]; imports: Import[]; base_url: string }
export interface AIStatus { ready: boolean; provider: '' | 'pocket' | 'openai'; detail: string }

export interface Counts { steps: number; active_kcal: number; exercise_min: number; sleep_min: number; sleep_text: string; resting_hr: number; weight: number; weight_text: string; workouts_week: number; goals_met: number; goals_total: number; goals_text: string; meds_due: number }

export const SEVERITY: Record<number, string> = { 1: 'Mild', 2: 'Noticeable', 3: 'Moderate', 4: 'Bad', 5: 'Severe' }
export const FORMS = ['tablet', 'capsule', 'drops', 'spray', 'injection', 'cream', 'inhaler', 'powder', 'other']
export const WEEKDAYS = [[1, 'Mon'], [2, 'Tue'], [3, 'Wed'], [4, 'Thu'], [5, 'Fri'], [6, 'Sat'], [7, 'Sun']] as const
export const RANGES: { id: string; label: string }[] = [{ id: 'w', label: '7 days' }, { id: 'm', label: '30 days' }, { id: '3m', label: '3 months' }, { id: '6m', label: '6 months' }, { id: 'y', label: 'Year' }, { id: 'all', label: 'All' }]

export function cap(s: string): string { return s ? s[0].toUpperCase() + s.slice(1) : '' }
