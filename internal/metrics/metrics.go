// Package metrics is the catalog of what Pocket Health records: every metric
// has a key, a canonical unit the database stores, how a day is rolled up
// (sum, average, last value…) and the names it goes by in Apple Health and
// in the Health Auto Export app. Unknown metrics from an import are kept
// under their own name with the unit they came in, never dropped.
package metrics

import (
	"math"
	"sort"
	"strings"
)

// Agg is how samples of a day become the day's number.
type Agg string

const (
	Sum  Agg = "sum"  // steps, distance, energy, water
	Avg  Agg = "avg"  // heart rate, SpO2, glucose
	Last Agg = "last" // weight, body fat, height: the latest reading counts
	Max  Agg = "max"
	Min  Agg = "min"
)

// Category groups metrics on the Metrics page.
type Category string

const (
	Activity  Category = "activity"
	Body      Category = "body"
	Heart     Category = "heart"
	Vitals    Category = "vitals"
	SleepCat  Category = "sleep"
	Nutrition Category = "nutrition"
	Mobility  Category = "mobility"
	Mind      Category = "mind"
	Hearing   Category = "hearing"
	Cycle     Category = "cycle"
	Other     Category = "other"
)

// Metric describes one kind of measurement.
type Metric struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Unit     string   `json:"unit"`          // canonical unit stored
	Imperial string   `json:"imperial_unit"` // what to show in imperial mode ("" = same)
	Agg      Agg      `json:"agg"`
	Category Category `json:"category"`
	Decimals int      `json:"decimals"`
	Icon     string   `json:"icon"`
	Color    string   `json:"color"`
	// HigherIsBetter steers the trend arrow colour (nil = neutral).
	HigherIsBetter *bool `json:"higher_is_better"`
	// Pair metrics store two numbers (blood pressure: systolic/diastolic).
	Pair bool `json:"pair"`
	// Manual lists metrics people type in by hand (the Log dialog).
	Manual bool `json:"manual"`
	// Goal is the default goal direction when someone sets a goal.
	GoalDir string `json:"goal_dir"` // at_least | at_most | ""
	// SumSources adds up a summed metric across sources (each its own
	// sessions, like mindful minutes from the watch and from Pocket Still);
	// within one source the one-device-per-day rule still applies.
	SumSources bool `json:"sum_sources,omitempty"`

	hae   []string // Health Auto Export names
	apple []string // HKQuantityTypeIdentifier… names
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

// Catalog in display order.
var Catalog = []Metric{
	// ---- activity ----
	{Key: "steps", Name: "Steps", Unit: "count", Agg: Sum, Category: Activity, Icon: "footprints", Color: "#f97316", HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"step_count"}, apple: []string{"StepCount"}},
	{Key: "distance", Name: "Walking + running distance", Unit: "km", Imperial: "mi", Agg: Sum, Category: Activity, Decimals: 2, Icon: "route", Color: "#f97316", HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"walking_running_distance"}, apple: []string{"DistanceWalkingRunning"}},
	{Key: "active_energy", Name: "Active energy", Unit: "kcal", Agg: Sum, Category: Activity, Icon: "flame", Color: "#ef4444", HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"active_energy"}, apple: []string{"ActiveEnergyBurned"}},
	{Key: "resting_energy", Name: "Resting energy", Unit: "kcal", Agg: Sum, Category: Activity, Icon: "flame", Color: "#f59e0b", hae: []string{"basal_energy_burned"}, apple: []string{"BasalEnergyBurned"}},
	{Key: "exercise_min", Name: "Exercise minutes", Unit: "min", Agg: Sum, Category: Activity, Icon: "timer", Color: "#22c55e", HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"apple_exercise_time"}, apple: []string{"AppleExerciseTime"}},
	{Key: "stand_hours", Name: "Stand hours", Unit: "h", Agg: Sum, Category: Activity, Icon: "stand", Color: "#06b6d4", HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"apple_stand_hour"}, apple: []string{"AppleStandHour"}},
	{Key: "stand_min", Name: "Stand minutes", Unit: "min", Agg: Sum, Category: Activity, Icon: "stand", Color: "#06b6d4", hae: []string{"apple_stand_time"}, apple: []string{"AppleStandTime"}},
	{Key: "flights", Name: "Flights climbed", Unit: "count", Agg: Sum, Category: Activity, Icon: "stairs", Color: "#8b5cf6", HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"flights_climbed"}, apple: []string{"FlightsClimbed"}},
	{Key: "cycling_distance", Name: "Cycling distance", Unit: "km", Imperial: "mi", Agg: Sum, Category: Activity, Decimals: 2, Icon: "bike", Color: "#0ea5e9", HigherIsBetter: yes(), hae: []string{"cycling_distance"}, apple: []string{"DistanceCycling"}},
	{Key: "swimming_distance", Name: "Swimming distance", Unit: "m", Imperial: "yd", Agg: Sum, Category: Activity, Icon: "swim", Color: "#0ea5e9", HigherIsBetter: yes(), hae: []string{"swimming_distance"}, apple: []string{"DistanceSwimming"}},
	{Key: "swimming_strokes", Name: "Swimming strokes", Unit: "count", Agg: Sum, Category: Activity, Icon: "swim", Color: "#0ea5e9", hae: []string{"swimming_stroke_count"}, apple: []string{"SwimmingStrokeCount"}},
	{Key: "push_count", Name: "Pushes", Unit: "count", Agg: Sum, Category: Activity, Icon: "activity", Color: "#f97316", hae: []string{"push_count"}, apple: []string{"PushCount"}},
	{Key: "wheelchair_distance", Name: "Wheelchair distance", Unit: "km", Imperial: "mi", Agg: Sum, Category: Activity, Decimals: 2, Icon: "route", Color: "#f97316", hae: []string{"distance_wheelchair"}, apple: []string{"DistanceWheelchair"}},
	{Key: "time_in_daylight", Name: "Time in daylight", Unit: "min", Agg: Sum, Category: Activity, Icon: "sun", Color: "#f59e0b", HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"time_in_daylight"}, apple: []string{"TimeInDaylight"}},
	{Key: "physical_effort", Name: "Physical effort", Unit: "kcal/hr·kg", Agg: Avg, Category: Activity, Decimals: 1, Icon: "activity", Color: "#f97316", hae: []string{"physical_effort"}, apple: []string{"PhysicalEffort"}},
	// ---- body ----
	{Key: "weight", Name: "Weight", Unit: "kg", Imperial: "lb", Agg: Last, Category: Body, Decimals: 1, Icon: "scale", Color: "#8b5cf6", Manual: true, GoalDir: "at_most", hae: []string{"weight_body_mass"}, apple: []string{"BodyMass"}},
	{Key: "body_fat", Name: "Body fat", Unit: "%", Agg: Last, Category: Body, Decimals: 1, Icon: "percent", Color: "#8b5cf6", Manual: true, HigherIsBetter: no(), GoalDir: "at_most", hae: []string{"body_fat_percentage"}, apple: []string{"BodyFatPercentage"}},
	{Key: "lean_mass", Name: "Lean body mass", Unit: "kg", Imperial: "lb", Agg: Last, Category: Body, Decimals: 1, Icon: "scale", Color: "#8b5cf6", Manual: true, HigherIsBetter: yes(), hae: []string{"lean_body_mass"}, apple: []string{"LeanBodyMass"}},
	{Key: "bmi", Name: "Body mass index", Unit: "kg/m²", Agg: Last, Category: Body, Decimals: 1, Icon: "scale", Color: "#8b5cf6", hae: []string{"body_mass_index"}, apple: []string{"BodyMassIndex"}},
	{Key: "height", Name: "Height", Unit: "cm", Imperial: "in", Agg: Last, Category: Body, Decimals: 1, Icon: "ruler", Color: "#8b5cf6", Manual: true, hae: []string{"height"}, apple: []string{"Height"}},
	{Key: "waist", Name: "Waist circumference", Unit: "cm", Imperial: "in", Agg: Last, Category: Body, Decimals: 1, Icon: "ruler", Color: "#8b5cf6", Manual: true, HigherIsBetter: no(), GoalDir: "at_most", hae: []string{"waist_circumference"}, apple: []string{"WaistCircumference"}},
	{Key: "body_temp", Name: "Body temperature", Unit: "°C", Imperial: "°F", Agg: Avg, Category: Body, Decimals: 1, Icon: "thermometer", Color: "#ef4444", Manual: true, hae: []string{"body_temperature"}, apple: []string{"BodyTemperature"}},
	{Key: "wrist_temp", Name: "Sleeping wrist temperature", Unit: "°C", Imperial: "°F", Agg: Avg, Category: Body, Decimals: 2, Icon: "thermometer", Color: "#ef4444", hae: []string{"apple_sleeping_wrist_temperature"}, apple: []string{"AppleSleepingWristTemperature"}},
	// ---- heart ----
	{Key: "heart_rate", Name: "Heart rate", Unit: "bpm", Agg: Avg, Category: Heart, Icon: "heart-pulse", Color: "#ef4444", Manual: true, hae: []string{"heart_rate"}, apple: []string{"HeartRate"}},
	{Key: "resting_hr", Name: "Resting heart rate", Unit: "bpm", Agg: Avg, Category: Heart, Icon: "heart", Color: "#ef4444", Manual: true, HigherIsBetter: no(), hae: []string{"resting_heart_rate"}, apple: []string{"RestingHeartRate"}},
	{Key: "walking_hr", Name: "Walking heart rate", Unit: "bpm", Agg: Avg, Category: Heart, Icon: "heart", Color: "#ef4444", HigherIsBetter: no(), hae: []string{"walking_heart_rate_average"}, apple: []string{"WalkingHeartRateAverage"}},
	{Key: "hrv", Name: "Heart rate variability", Unit: "ms", Agg: Avg, Category: Heart, Icon: "activity", Color: "#ec4899", HigherIsBetter: yes(), hae: []string{"heart_rate_variability"}, apple: []string{"HeartRateVariabilitySDNN"}},
	{Key: "vo2max", Name: "VO₂ max", Unit: "mL/kg·min", Agg: Last, Category: Heart, Decimals: 1, Icon: "lungs", Color: "#ec4899", HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"vo2_max"}, apple: []string{"VO2Max"}},
	{Key: "hr_recovery", Name: "Heart rate recovery", Unit: "bpm", Agg: Avg, Category: Heart, Icon: "heart", Color: "#ec4899", HigherIsBetter: yes(), hae: []string{"heart_rate_recovery_one_minute"}, apple: []string{"HeartRateRecoveryOneMinute"}},
	{Key: "blood_pressure", Name: "Blood pressure", Unit: "mmHg", Agg: Avg, Category: Heart, Icon: "gauge", Color: "#dc2626", Pair: true, Manual: true, HigherIsBetter: no(), hae: []string{"blood_pressure"}, apple: []string{"BloodPressureSystolic", "BloodPressureDiastolic"}},
	{Key: "afib_burden", Name: "AFib burden", Unit: "%", Agg: Avg, Category: Heart, Decimals: 1, Icon: "heart", Color: "#dc2626", HigherIsBetter: no(), hae: []string{"atrial_fibrillation_burden"}, apple: []string{"AtrialFibrillationBurden"}},
	// ---- vitals ----
	{Key: "spo2", Name: "Blood oxygen", Unit: "%", Agg: Avg, Category: Vitals, Decimals: 1, Icon: "droplet", Color: "#3b82f6", Manual: true, HigherIsBetter: yes(), hae: []string{"blood_oxygen_saturation"}, apple: []string{"OxygenSaturation"}},
	{Key: "resp_rate", Name: "Respiratory rate", Unit: "br/min", Agg: Avg, Category: Vitals, Decimals: 1, Icon: "lungs", Color: "#3b82f6", hae: []string{"respiratory_rate"}, apple: []string{"RespiratoryRate"}},
	{Key: "blood_glucose", Name: "Blood glucose", Unit: "mg/dL", Imperial: "", Agg: Avg, Category: Vitals, Icon: "droplet", Color: "#a855f7", Manual: true, hae: []string{"blood_glucose"}, apple: []string{"BloodGlucose"}},
	{Key: "insulin", Name: "Insulin delivery", Unit: "IU", Agg: Sum, Category: Vitals, Decimals: 1, Icon: "syringe", Color: "#a855f7", Manual: true, hae: []string{"insulin_delivery"}, apple: []string{"InsulinDelivery"}},
	{Key: "peak_flow", Name: "Peak expiratory flow", Unit: "L/min", Agg: Avg, Category: Vitals, Icon: "lungs", Color: "#3b82f6", Manual: true, HigherIsBetter: yes(), hae: []string{"peak_expiratory_flow_rate"}, apple: []string{"PeakExpiratoryFlowRate"}},
	{Key: "inhaler", Name: "Inhaler usage", Unit: "count", Agg: Sum, Category: Vitals, Icon: "lungs", Color: "#3b82f6", Manual: true, HigherIsBetter: no(), hae: []string{"inhaler_usage"}, apple: []string{"InhalerUsage"}},
	{Key: "falls", Name: "Falls", Unit: "count", Agg: Sum, Category: Vitals, Icon: "alert", Color: "#dc2626", HigherIsBetter: no(), hae: []string{"number_of_times_fallen"}, apple: []string{"NumberOfTimesFallen"}},
	// ---- sleep (day values come from the sleep sessions) ----
	{Key: "sleep_asleep", Name: "Time asleep", Unit: "min", Agg: Sum, Category: SleepCat, Icon: "moon", Color: "#6366f1", HigherIsBetter: yes(), GoalDir: "at_least"},
	{Key: "sleep_in_bed", Name: "Time in bed", Unit: "min", Agg: Sum, Category: SleepCat, Icon: "bed", Color: "#6366f1"},
	{Key: "sleep_deep", Name: "Deep sleep", Unit: "min", Agg: Sum, Category: SleepCat, Icon: "moon", Color: "#4338ca", HigherIsBetter: yes()},
	{Key: "sleep_rem", Name: "REM sleep", Unit: "min", Agg: Sum, Category: SleepCat, Icon: "moon", Color: "#818cf8", HigherIsBetter: yes()},
	{Key: "sleep_core", Name: "Core sleep", Unit: "min", Agg: Sum, Category: SleepCat, Icon: "moon", Color: "#a5b4fc"},
	{Key: "sleep_awake", Name: "Awake in bed", Unit: "min", Agg: Sum, Category: SleepCat, Icon: "sun", Color: "#f59e0b", HigherIsBetter: no()},
	// ---- nutrition ----
	{Key: "water", Name: "Water", Unit: "mL", Imperial: "fl oz", Agg: Sum, Category: Nutrition, Icon: "droplet", Color: "#0ea5e9", Manual: true, HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"dietary_water"}, apple: []string{"DietaryWater"}},
	{Key: "caffeine", Name: "Caffeine", Unit: "mg", Agg: Sum, Category: Nutrition, Icon: "coffee", Color: "#92400e", Manual: true, HigherIsBetter: no(), GoalDir: "at_most", hae: []string{"dietary_caffeine"}, apple: []string{"DietaryCaffeine"}},
	{Key: "dietary_energy", Name: "Dietary energy", Unit: "kcal", Agg: Sum, Category: Nutrition, Icon: "utensils", Color: "#84cc16", Manual: true, GoalDir: "at_most", hae: []string{"dietary_energy"}, apple: []string{"DietaryEnergyConsumed"}},
	{Key: "protein", Name: "Protein", Unit: "g", Agg: Sum, Category: Nutrition, Icon: "utensils", Color: "#84cc16", Manual: true, HigherIsBetter: yes(), GoalDir: "at_least", hae: []string{"protein"}, apple: []string{"DietaryProtein"}},
	{Key: "carbs", Name: "Carbohydrates", Unit: "g", Agg: Sum, Category: Nutrition, Icon: "utensils", Color: "#84cc16", Manual: true, hae: []string{"carbohydrates"}, apple: []string{"DietaryCarbohydrates"}},
	{Key: "fat", Name: "Fat", Unit: "g", Agg: Sum, Category: Nutrition, Icon: "utensils", Color: "#84cc16", Manual: true, hae: []string{"total_fat"}, apple: []string{"DietaryFatTotal"}},
	{Key: "fiber", Name: "Fibre", Unit: "g", Agg: Sum, Category: Nutrition, Icon: "utensils", Color: "#84cc16", Manual: true, HigherIsBetter: yes(), hae: []string{"fiber"}, apple: []string{"DietaryFiber"}},
	{Key: "sugar", Name: "Sugar", Unit: "g", Agg: Sum, Category: Nutrition, Icon: "utensils", Color: "#84cc16", Manual: true, HigherIsBetter: no(), hae: []string{"dietary_sugar"}, apple: []string{"DietarySugar"}},
	{Key: "sodium", Name: "Sodium", Unit: "mg", Agg: Sum, Category: Nutrition, Icon: "utensils", Color: "#84cc16", Manual: true, HigherIsBetter: no(), hae: []string{"sodium"}, apple: []string{"DietarySodium"}},
	{Key: "alcohol", Name: "Alcohol", Unit: "drinks", Agg: Sum, Category: Nutrition, Icon: "glass", Color: "#b45309", Manual: true, HigherIsBetter: no(), GoalDir: "at_most", hae: []string{"number_of_alcoholic_beverages"}, apple: []string{"NumberOfAlcoholicBeverages"}},
	// ---- mobility ----
	{Key: "walking_speed", Name: "Walking speed", Unit: "km/h", Imperial: "mph", Agg: Avg, Category: Mobility, Decimals: 2, Icon: "footprints", Color: "#14b8a6", HigherIsBetter: yes(), hae: []string{"walking_speed"}, apple: []string{"WalkingSpeed"}},
	{Key: "step_length", Name: "Step length", Unit: "cm", Imperial: "in", Agg: Avg, Category: Mobility, Decimals: 1, Icon: "footprints", Color: "#14b8a6", hae: []string{"walking_step_length"}, apple: []string{"WalkingStepLength"}},
	{Key: "walking_asymmetry", Name: "Walking asymmetry", Unit: "%", Agg: Avg, Category: Mobility, Decimals: 1, Icon: "footprints", Color: "#14b8a6", HigherIsBetter: no(), hae: []string{"walking_asymmetry_percentage"}, apple: []string{"WalkingAsymmetryPercentage"}},
	{Key: "double_support", Name: "Double support time", Unit: "%", Agg: Avg, Category: Mobility, Decimals: 1, Icon: "footprints", Color: "#14b8a6", HigherIsBetter: no(), hae: []string{"walking_double_support_percentage"}, apple: []string{"WalkingDoubleSupportPercentage"}},
	{Key: "stair_up", Name: "Stair speed up", Unit: "m/s", Agg: Avg, Category: Mobility, Decimals: 2, Icon: "stairs", Color: "#14b8a6", HigherIsBetter: yes(), hae: []string{"stair_speed_up"}, apple: []string{"StairAscentSpeed"}},
	{Key: "stair_down", Name: "Stair speed down", Unit: "m/s", Agg: Avg, Category: Mobility, Decimals: 2, Icon: "stairs", Color: "#14b8a6", HigherIsBetter: yes(), hae: []string{"stair_speed_down"}, apple: []string{"StairDescentSpeed"}},
	{Key: "six_min_walk", Name: "Six-minute walk", Unit: "m", Imperial: "yd", Agg: Last, Category: Mobility, Icon: "route", Color: "#14b8a6", HigherIsBetter: yes(), hae: []string{"six_minute_walking_test_distance"}, apple: []string{"SixMinuteWalkTestDistance"}},
	{Key: "running_power", Name: "Running power", Unit: "W", Agg: Avg, Category: Mobility, Icon: "zap", Color: "#14b8a6", hae: []string{"running_power"}, apple: []string{"RunningPower"}},
	{Key: "running_speed", Name: "Running speed", Unit: "km/h", Imperial: "mph", Agg: Avg, Category: Mobility, Decimals: 2, Icon: "run", Color: "#14b8a6", HigherIsBetter: yes(), hae: []string{"running_speed"}, apple: []string{"RunningSpeed"}},
	{Key: "running_stride", Name: "Running stride length", Unit: "m", Agg: Avg, Category: Mobility, Decimals: 2, Icon: "run", Color: "#14b8a6", hae: []string{"running_stride_length"}, apple: []string{"RunningStrideLength"}},
	{Key: "ground_contact", Name: "Ground contact time", Unit: "ms", Agg: Avg, Category: Mobility, Icon: "run", Color: "#14b8a6", HigherIsBetter: no(), hae: []string{"running_ground_contact_time"}, apple: []string{"RunningGroundContactTime"}},
	{Key: "vertical_osc", Name: "Vertical oscillation", Unit: "cm", Agg: Avg, Category: Mobility, Decimals: 1, Icon: "run", Color: "#14b8a6", HigherIsBetter: no(), hae: []string{"running_vertical_oscillation"}, apple: []string{"RunningVerticalOscillation"}},
	{Key: "cycling_power", Name: "Cycling power", Unit: "W", Agg: Avg, Category: Mobility, Icon: "bike", Color: "#0ea5e9", hae: []string{"cycling_power"}, apple: []string{"CyclingPower"}},
	{Key: "cycling_cadence", Name: "Cycling cadence", Unit: "rpm", Agg: Avg, Category: Mobility, Icon: "bike", Color: "#0ea5e9", hae: []string{"cycling_cadence"}, apple: []string{"CyclingCadence"}},
	{Key: "cycling_ftp", Name: "Cycling FTP", Unit: "W", Agg: Last, Category: Mobility, Icon: "bike", Color: "#0ea5e9", HigherIsBetter: yes(), hae: []string{"cycling_functional_threshold_power"}, apple: []string{"CyclingFunctionalThresholdPower"}},
	// ---- mind ----
	{Key: "mindful_min", Name: "Mindful minutes", Unit: "min", Agg: Sum, Category: Mind, Icon: "brain", Color: "#06b6d4", Manual: true, HigherIsBetter: yes(), GoalDir: "at_least", SumSources: true, hae: []string{"mindful_minutes"}, apple: []string{"MindfulSession"}},
	{Key: "mood", Name: "Mood", Unit: "1–5", Agg: Avg, Category: Mind, Decimals: 1, Icon: "smile", Color: "#06b6d4", Manual: true, HigherIsBetter: yes()},
	{Key: "energy_level", Name: "Energy level", Unit: "1–5", Agg: Avg, Category: Mind, Decimals: 1, Icon: "zap", Color: "#06b6d4", Manual: true, HigherIsBetter: yes()},
	{Key: "stress", Name: "Stress", Unit: "1–5", Agg: Avg, Category: Mind, Decimals: 1, Icon: "brain", Color: "#06b6d4", Manual: true, HigherIsBetter: no()},
	// ---- hearing ----
	{Key: "env_audio", Name: "Environmental sound", Unit: "dB", Agg: Avg, Category: Hearing, Icon: "ear", Color: "#64748b", HigherIsBetter: no(), hae: []string{"environmental_audio_exposure"}, apple: []string{"EnvironmentalAudioExposure"}},
	{Key: "headphone_audio", Name: "Headphone audio", Unit: "dB", Agg: Avg, Category: Hearing, Icon: "headphones", Color: "#64748b", HigherIsBetter: no(), hae: []string{"headphone_audio_exposure"}, apple: []string{"HeadphoneAudioExposure"}},
	// ---- other ----
	{Key: "uv_exposure", Name: "UV exposure", Unit: "index", Agg: Max, Category: Other, Icon: "sun", Color: "#f59e0b", hae: []string{"uv_exposure"}, apple: []string{"UVExposure"}},
	{Key: "handwashing", Name: "Handwashing", Unit: "count", Agg: Sum, Category: Other, Icon: "droplet", Color: "#0ea5e9", hae: []string{"handwashing"}, apple: []string{"HandwashingEvent"}},
	{Key: "toothbrushing", Name: "Toothbrushing", Unit: "count", Agg: Sum, Category: Other, Icon: "smile", Color: "#0ea5e9", hae: []string{"toothbrushing"}, apple: []string{"ToothbrushingEvent"}},
	{Key: "sexual_activity", Name: "Sexual activity", Unit: "count", Agg: Sum, Category: Other, Icon: "heart", Color: "#ec4899", Manual: true, hae: []string{"sexual_activity"}, apple: []string{"SexualActivity"}},
	{Key: "basal_body_temp", Name: "Basal body temperature", Unit: "°C", Imperial: "°F", Agg: Avg, Category: Other, Decimals: 2, Icon: "thermometer", Color: "#ec4899", Manual: true, hae: []string{"basal_body_temperature"}, apple: []string{"BasalBodyTemperature"}},
	{Key: "electrodermal", Name: "Electrodermal activity", Unit: "µS", Agg: Avg, Category: Other, Decimals: 2, Icon: "zap", Color: "#64748b", hae: []string{"electrodermal_activity"}, apple: []string{"ElectrodermalActivity"}},
	// ---- read from Apple Health since 0.5.0 ----
	{Key: "hrv_rmssd", Name: "HRV (RMSSD)", Unit: "ms", Agg: Avg, Category: Heart, Icon: "activity", Color: "#a855f7", HigherIsBetter: yes(), apple: []string{"HeartRateVariabilityRMSSD"}},
	{Key: "move_min", Name: "Move minutes", Unit: "min", Agg: Sum, Category: Activity, Icon: "timer", Color: "#ef4444", HigherIsBetter: yes(), GoalDir: "at_least", apple: []string{"AppleMoveTime"}},
	{Key: "move_goal", Name: "Move goal", Unit: "kcal", Agg: Last, Category: Activity, Icon: "target", Color: "#ef4444"},
	{Key: "exercise_goal", Name: "Exercise goal", Unit: "min", Agg: Last, Category: Activity, Icon: "target", Color: "#22c55e"},
	{Key: "stand_goal", Name: "Stand goal", Unit: "h", Agg: Last, Category: Activity, Icon: "target", Color: "#06b6d4"},
	{Key: "nike_fuel", Name: "NikeFuel", Unit: "count", Agg: Sum, Category: Activity, Icon: "zap", Color: "#f97316", apple: []string{"NikeFuel"}},
	{Key: "breathing_disturbances", Name: "Breathing disturbances", Unit: "count", Agg: Avg, Category: SleepCat, Decimals: 1, Icon: "lungs", Color: "#6366f1", HigherIsBetter: no(), apple: []string{"AppleSleepingBreathingDisturbances"}},
	{Key: "walking_steadiness", Name: "Walking steadiness", Unit: "%", Agg: Last, Category: Mobility, Icon: "footprints", Color: "#14b8a6", HigherIsBetter: yes(), apple: []string{"AppleWalkingSteadiness"}},
	{Key: "xc_ski_speed", Name: "Cross-country skiing speed", Unit: "km/h", Imperial: "mph", Agg: Avg, Category: Mobility, Decimals: 1, Icon: "gauge", Color: "#0ea5e9", apple: []string{"CrossCountrySkiingSpeed"}},
	{Key: "paddle_speed", Name: "Paddling speed", Unit: "km/h", Imperial: "mph", Agg: Avg, Category: Mobility, Decimals: 1, Icon: "gauge", Color: "#0ea5e9", apple: []string{"PaddleSportsSpeed"}},
	{Key: "rowing_speed", Name: "Rowing speed", Unit: "km/h", Imperial: "mph", Agg: Avg, Category: Mobility, Decimals: 1, Icon: "gauge", Color: "#0ea5e9", apple: []string{"RowingSpeed"}},
	{Key: "blood_alcohol", Name: "Blood alcohol", Unit: "%", Agg: Max, Category: Vitals, Decimals: 3, Icon: "glass", Color: "#a855f7", Manual: true, HigherIsBetter: no(), apple: []string{"BloodAlcoholContent"}},
	{Key: "fvc", Name: "Forced vital capacity", Unit: "L", Agg: Last, Category: Vitals, Decimals: 2, Icon: "lungs", Color: "#3b82f6", Manual: true, HigherIsBetter: yes(), apple: []string{"ForcedVitalCapacity"}},
	{Key: "fev1", Name: "FEV1", Unit: "L", Agg: Last, Category: Vitals, Decimals: 2, Icon: "lungs", Color: "#3b82f6", Manual: true, HigherIsBetter: yes(), apple: []string{"ForcedExpiratoryVolume1"}},
	{Key: "perfusion_index", Name: "Perfusion index", Unit: "%", Agg: Avg, Category: Vitals, Decimals: 1, Icon: "droplet", Color: "#3b82f6", apple: []string{"PeripheralPerfusionIndex"}},
	{Key: "water_temp", Name: "Water temperature", Unit: "°C", Imperial: "°F", Agg: Avg, Category: Other, Decimals: 1, Icon: "thermometer", Color: "#0ea5e9", apple: []string{"WaterTemperature"}},
	{Key: "underwater_depth", Name: "Underwater depth", Unit: "m", Imperial: "ft", Agg: Max, Category: Other, Decimals: 1, Icon: "swim", Color: "#0ea5e9", apple: []string{"UnderwaterDepth"}},
	{Key: "sound_reduction", Name: "Sound reduction", Unit: "dB", Agg: Avg, Category: Hearing, Icon: "headphones", Color: "#64748b", apple: []string{"EnvironmentalSoundReduction"}},
	{Key: "state_of_mind", Name: "State of mind", Unit: "−1…1", Agg: Avg, Category: Mind, Decimals: 2, Icon: "smile", Color: "#06b6d4", HigherIsBetter: yes()},
	// notifications the watch or phone raised, counted per day
	{Key: "high_hr_events", Name: "High heart rate notifications", Unit: "count", Agg: Sum, Category: Heart, Icon: "heart", Color: "#ef4444", HigherIsBetter: no(), apple: []string{"HighHeartRateEvent"}},
	{Key: "low_hr_events", Name: "Low heart rate notifications", Unit: "count", Agg: Sum, Category: Heart, Icon: "heart", Color: "#ef4444", HigherIsBetter: no(), apple: []string{"LowHeartRateEvent"}},
	{Key: "irregular_rhythm_events", Name: "Irregular rhythm notifications", Unit: "count", Agg: Sum, Category: Heart, Icon: "heart", Color: "#ef4444", HigherIsBetter: no(), apple: []string{"IrregularHeartRhythmEvent"}},
	{Key: "hypertension_events", Name: "Hypertension notifications", Unit: "count", Agg: Sum, Category: Heart, Icon: "heart", Color: "#ef4444", HigherIsBetter: no(), apple: []string{"HypertensionEvent"}},
	{Key: "low_cardio_fitness_events", Name: "Low cardio fitness notifications", Unit: "count", Agg: Sum, Category: Heart, Icon: "heart", Color: "#ef4444", HigherIsBetter: no(), apple: []string{"LowCardioFitnessEvent"}},
	{Key: "sleep_apnea_events", Name: "Sleep apnea notifications", Unit: "count", Agg: Sum, Category: SleepCat, Icon: "bed", Color: "#ef4444", HigherIsBetter: no(), apple: []string{"SleepApneaEvent"}},
	{Key: "steadiness_events", Name: "Walking steadiness notifications", Unit: "count", Agg: Sum, Category: Mobility, Icon: "footprints", Color: "#ef4444", HigherIsBetter: no(), apple: []string{"AppleWalkingSteadinessEvent"}},
	{Key: "env_audio_events", Name: "Loud environment notifications", Unit: "count", Agg: Sum, Category: Hearing, Icon: "ear", Color: "#ef4444", HigherIsBetter: no(), apple: []string{"EnvironmentalAudioExposureEvent"}},
	{Key: "headphone_audio_events", Name: "Loud headphone notifications", Unit: "count", Agg: Sum, Category: Hearing, Icon: "headphones", Color: "#ef4444", HigherIsBetter: no(), apple: []string{"HeadphoneAudioExposureEvent"}},
	// nutrition beyond the basics
	{Key: "cholesterol", Name: "Cholesterol", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, HigherIsBetter: no(), apple: []string{"DietaryCholesterol"}},
	{Key: "fat_saturated", Name: "Saturated fat", Unit: "g", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, HigherIsBetter: no(), apple: []string{"DietaryFatSaturated"}},
	{Key: "fat_mono", Name: "Monounsaturated fat", Unit: "g", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryFatMonounsaturated"}},
	{Key: "fat_poly", Name: "Polyunsaturated fat", Unit: "g", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryFatPolyunsaturated"}},
	{Key: "calcium", Name: "Calcium", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryCalcium"}},
	{Key: "iron", Name: "Iron", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryIron"}},
	{Key: "potassium", Name: "Potassium", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryPotassium"}},
	{Key: "magnesium", Name: "Magnesium", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryMagnesium"}},
	{Key: "phosphorus", Name: "Phosphorus", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryPhosphorus"}},
	{Key: "zinc", Name: "Zinc", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryZinc"}},
	{Key: "copper", Name: "Copper", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryCopper"}},
	{Key: "manganese", Name: "Manganese", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryManganese"}},
	{Key: "chloride", Name: "Chloride", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryChloride"}},
	{Key: "selenium", Name: "Selenium", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietarySelenium"}},
	{Key: "iodine", Name: "Iodine", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryIodine"}},
	{Key: "chromium", Name: "Chromium", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryChromium"}},
	{Key: "molybdenum", Name: "Molybdenum", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryMolybdenum"}},
	{Key: "vitamin_a", Name: "Vitamin A", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryVitaminA"}},
	{Key: "vitamin_b6", Name: "Vitamin B6", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryVitaminB6"}},
	{Key: "vitamin_b12", Name: "Vitamin B12", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryVitaminB12"}},
	{Key: "vitamin_c", Name: "Vitamin C", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryVitaminC"}},
	{Key: "vitamin_d", Name: "Vitamin D", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryVitaminD"}},
	{Key: "vitamin_e", Name: "Vitamin E", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryVitaminE"}},
	{Key: "vitamin_k", Name: "Vitamin K", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryVitaminK"}},
	{Key: "thiamin", Name: "Thiamin (B1)", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryThiamin"}},
	{Key: "riboflavin", Name: "Riboflavin (B2)", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryRiboflavin"}},
	{Key: "niacin", Name: "Niacin (B3)", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryNiacin"}},
	{Key: "pantothenic_acid", Name: "Pantothenic acid (B5)", Unit: "mg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryPantothenicAcid"}},
	{Key: "biotin", Name: "Biotin (B7)", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryBiotin"}},
	{Key: "folate", Name: "Folate (B9)", Unit: "µg", Agg: Sum, Category: Nutrition, Decimals: 1, Icon: "utensils", Color: "#84cc16", Manual: true, apple: []string{"DietaryFolate"}},
	// cycle tracking: a level or a count per day, what was recorded in the note
	{Key: "menstrual_flow", Name: "Menstrual flow", Unit: "0–3", Agg: Max, Category: Cycle, Icon: "droplet", Color: "#ec4899", apple: []string{"MenstrualFlow"}},
	{Key: "spotting", Name: "Spotting", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"IntermenstrualBleeding"}},
	{Key: "ovulation_test", Name: "Ovulation test", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"OvulationTestResult"}},
	{Key: "pregnancy_test", Name: "Pregnancy test", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"PregnancyTestResult"}},
	{Key: "progesterone_test", Name: "Progesterone test", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"ProgesteroneTestResult"}},
	{Key: "contraceptive", Name: "Contraceptive", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"Contraceptive"}},
	{Key: "pregnancy", Name: "Pregnancy", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"Pregnancy"}},
	{Key: "lactation", Name: "Lactation", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"Lactation"}},
	{Key: "bleeding_during_pregnancy", Name: "Bleeding during pregnancy", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"BleedingDuringPregnancy"}},
	{Key: "bleeding_after_pregnancy", Name: "Bleeding after pregnancy", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"BleedingAfterPregnancy"}},
	{Key: "bleeding_after_menopause", Name: "Bleeding after menopause", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"BleedingAfterMenopause"}},
	{Key: "menopause", Name: "Menopause", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"MenopausalState"}},
	{Key: "irregular_cycle_events", Name: "Irregular cycle notifications", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"IrregularMenstrualCycles"}},
	{Key: "infrequent_cycle_events", Name: "Infrequent cycle notifications", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"InfrequentMenstrualCycles"}},
	{Key: "prolonged_period_events", Name: "Prolonged period notifications", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"ProlongedMenstrualPeriods"}},
	{Key: "persistent_spotting_events", Name: "Persistent spotting notifications", Unit: "count", Agg: Sum, Category: Cycle, Icon: "calendar", Color: "#ec4899", apple: []string{"PersistentIntermenstrualBleeding"}},
	{Key: "cervical_mucus", Name: "Cervical mucus", Unit: "1–5", Agg: Last, Category: Cycle, Icon: "droplet", Color: "#ec4899", apple: []string{"CervicalMucusQuality"}},
}

// CategoryNames in display order.
var CategoryNames = []struct {
	Key  Category `json:"key"`
	Name string   `json:"name"`
}{
	{Activity, "Activity"}, {Body, "Body"}, {Heart, "Heart"}, {Vitals, "Vitals"}, {SleepCat, "Sleep"},
	{Nutrition, "Nutrition"}, {Mobility, "Mobility & running"}, {Mind, "Mind"}, {Hearing, "Hearing"}, {Cycle, "Cycle tracking"}, {Other, "Other"},
}

var byKey = map[string]*Metric{}
var byHAE = map[string]*Metric{}
var byApple = map[string]*Metric{}

func init() {
	for i := range Catalog {
		m := &Catalog[i]
		byKey[m.Key] = m
		for _, h := range m.hae {
			byHAE[h] = m
		}
		for _, a := range m.apple {
			byApple["HKQuantityTypeIdentifier"+a] = m
			byApple["HKCategoryTypeIdentifier"+a] = m
			byApple[a] = m
		}
	}
}

// Get returns a catalog metric; for an unknown key a generic one (kept as it
// came: averaged, no conversion) so nothing from an import is lost.
func Get(key string) Metric {
	if m, ok := byKey[key]; ok {
		return *m
	}
	return Metric{Key: key, Name: Title(key), Agg: Avg, Category: Other, Decimals: 1, Icon: "activity", Color: "#64748b"}
}

// Known reports whether the key is in the catalog.
func Known(key string) bool { _, ok := byKey[key]; return ok }

// FromHAE maps a Health Auto Export metric name.
func FromHAE(name string) (Metric, bool) {
	if m, ok := byHAE[strings.ToLower(strings.TrimSpace(name))]; ok {
		return *m, true
	}
	return Metric{}, false
}

// FromApple maps an HKQuantityTypeIdentifier / HKCategoryTypeIdentifier.
func FromApple(typ string) (Metric, bool) {
	if m, ok := byApple[strings.TrimSpace(typ)]; ok {
		return *m, true
	}
	return Metric{}, false
}

// Title turns "walking_running_distance" into "Walking running distance".
func Title(key string) string {
	key = strings.ReplaceAll(strings.ReplaceAll(key, "_", " "), "-", " ")
	if key == "" {
		return ""
	}
	return strings.ToUpper(key[:1]) + key[1:]
}

// Keys lists every catalog key.
func Keys() []string {
	out := make([]string, 0, len(Catalog))
	for _, m := range Catalog {
		out = append(out, m.Key)
	}
	return out
}

// ManualKeys lists the metrics people type in by hand.
func ManualKeys() []string {
	var out []string
	for _, m := range Catalog {
		if m.Manual {
			out = append(out, m.Key)
		}
	}
	return out
}

// ---- unit conversion ----

// Normalize converts a value in a source unit into the metric's canonical
// unit. Unknown units are passed through unchanged (and reported).
func Normalize(m Metric, unit string, v float64) (float64, bool) {
	u := strings.ToLower(strings.TrimSpace(unit))
	u = strings.NewReplacer(" ", "", "_", "").Replace(u)
	if u == "" || u == strings.ToLower(m.Unit) || u == canon(m.Unit) {
		return v, true
	}
	switch m.Unit {
	case "kg":
		switch u {
		case "lb", "lbs":
			return v * 0.45359237, true
		case "g":
			return v / 1000, true
		case "st":
			return v * 6.35029318, true
		}
	case "km":
		switch u {
		case "mi", "mile", "miles":
			return v * 1.609344, true
		case "m":
			return v / 1000, true
		case "ft":
			return v * 0.0003048, true
		case "yd":
			return v * 0.0009144, true
		}
	case "m":
		switch u {
		case "cm":
			return v / 100, true
		case "mm":
			return v / 1000, true
		case "km":
			return v * 1000, true
		case "yd":
			return v * 0.9144, true
		case "ft":
			return v * 0.3048, true
		case "mi":
			return v * 1609.344, true
		}
	case "cm":
		switch u {
		case "in", "inch":
			return v * 2.54, true
		case "m":
			return v * 100, true
		case "ft":
			return v * 30.48, true
		case "mm":
			return v / 10, true
		}
	case "°C":
		switch u {
		case "degf", "°f", "f", "fahrenheit":
			return (v - 32) * 5 / 9, true
		case "degc", "°c", "c", "celsius":
			return v, true
		}
	case "mL":
		switch u {
		case "l", "liter", "litre":
			return v * 1000, true
		case "floz", "flozus", "fl.oz", "oz":
			return v * 29.5735, true
		case "flozuk", "flozimp":
			return v * 28.4131, true
		case "cup", "cups":
			return v * 236.588, true
		}
	case "mg/dL":
		switch u {
		case "mmol/l", "mmol<180.1558800000541/l>", "mmol":
			return v * 18.0182, true
		}
	case "kcal":
		switch u {
		case "cal":
			return v / 1000, true
		case "kj":
			return v / 4.184, true
		case "j":
			return v / 4184, true
		}
	case "min":
		switch u {
		case "s", "sec":
			return v / 60, true
		case "h", "hr", "hour", "hours":
			return v * 60, true
		case "ms":
			return v / 60000, true
		}
	case "h":
		switch u {
		case "min":
			return v / 60, true
		case "hour", "hours", "hr", "count":
			return v, true
		}
	case "km/h":
		switch u {
		case "m/s":
			return v * 3.6, true
		case "mph", "mi/hr", "mi/h":
			return v * 1.609344, true
		case "km/hr":
			return v, true
		}
	case "%":
		if u == "fraction" || u == "ratio" {
			return v * 100, true
		}
	case "bpm":
		if u == "count/min" || u == "count/s" || u == "/min" {
			return v, true
		}
	case "br/min":
		if u == "count/min" || u == "/min" {
			return v, true
		}
	case "count":
		return v, true
	case "ms":
		if u == "s" {
			return v * 1000, true
		}
	case "g":
		switch u {
		case "mg":
			return v / 1000, true
		case "kg":
			return v * 1000, true
		}
	case "mg":
		switch u {
		case "g":
			return v * 1000, true
		case "µg", "mcg", "ug":
			return v / 1000, true
		}
	case "dB":
		if u == "dbaspl" || u == "dba" || u == "db" {
			return v, true
		}
	case "m/s":
		if u == "km/h" {
			return v / 3.6, true
		}
	}
	return v, false
}

func canon(unit string) string {
	return strings.NewReplacer(" ", "", "_", "").Replace(strings.ToLower(unit))
}

// Display converts a canonical value for the person's unit system and
// returns value + unit label.
func Display(m Metric, v float64, imperial bool) (float64, string) {
	if !imperial || m.Imperial == "" {
		return v, m.Unit
	}
	switch m.Unit {
	case "kg":
		return v / 0.45359237, m.Imperial
	case "km":
		return v / 1.609344, m.Imperial
	case "m":
		return v / 0.9144, m.Imperial
	case "cm":
		return v / 2.54, m.Imperial
	case "°C":
		return v*9/5 + 32, m.Imperial
	case "mL":
		return v / 29.5735, m.Imperial
	case "km/h":
		return v / 1.609344, m.Imperial
	}
	return v, m.Unit
}

// Round to the metric's decimals.
func Round(m Metric, v float64) float64 {
	p := math.Pow(10, float64(m.Decimals))
	return math.Round(v*p) / p
}

// Aggregate folds a day's values with the metric's rule. Values must be in
// chronological order for Last.
func Aggregate(m Metric, values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	switch m.Agg {
	case Sum:
		s := 0.0
		for _, v := range values {
			s += v
		}
		return s
	case Last:
		return values[len(values)-1]
	case Max:
		x := values[0]
		for _, v := range values {
			x = math.Max(x, v)
		}
		return x
	case Min:
		x := values[0]
		for _, v := range values {
			x = math.Min(x, v)
		}
		return x
	}
	s := 0.0
	for _, v := range values {
		s += v
	}
	return s / float64(len(values))
}

// GuessAgg picks a rollup for a metric not in the catalog from its unit.
func GuessAgg(unit string) Agg {
	switch canon(unit) {
	case "count", "kcal", "cal", "kj", "km", "m", "mi", "min", "ml", "l", "g", "mg", "step", "steps", "h", "hr":
		return Sum
	}
	return Avg
}

// Sorted returns the catalog ordered by category then name (the Metrics page).
func Sorted() []Metric {
	out := append([]Metric(nil), Catalog...)
	order := map[Category]int{}
	for i, c := range CategoryNames {
		order[c.Key] = i
	}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Category] < order[out[j].Category] })
	return out
}

// BMI from weight (kg) and height (cm).
func BMI(kg, cm float64) float64 {
	if kg <= 0 || cm <= 0 {
		return 0
	}
	h := cm / 100
	return math.Round(kg/(h*h)*10) / 10
}
