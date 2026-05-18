package main

// Declarative registry of Home Assistant entities lifetract knows how to
// consume. Adding a new sensor is one line: append to KnownEntities.
//
// Kind groups entities by lifetract domain so callers (lazy ingest, today,
// read) can ask "give me the heart rate" without knowing the literal HA
// entity_id. A single Kind may map to multiple entities later (e.g. multiple
// devices) — keep that flexibility in mind.

// EntityKind is the lifetract-domain identifier for a class of sensor.
type EntityKind string

const (
	KindSleepDuration EntityKind = "sleep_duration"
	KindStepsDaily    EntityKind = "steps_daily"
	KindDistanceDaily EntityKind = "distance_daily"
	KindFloorsDaily   EntityKind = "floors_daily"
	KindHeartRate     EntityKind = "heart_rate"
	KindRestingHR     EntityKind = "resting_heart_rate"
	KindHRV           EntityKind = "heart_rate_variability"
	KindWeight        EntityKind = "weight"
	KindBodyFat       EntityKind = "body_fat"
	KindHeight        EntityKind = "height"
	KindCalories      EntityKind = "calories_burned"
	KindActiveCalories EntityKind = "active_calories_burned"
	KindBMR           EntityKind = "basal_metabolic_rate"
	KindHydration     EntityKind = "hydration"
	KindActivity      EntityKind = "detected_activity"
	KindLocation      EntityKind = "geocoded_location"
	KindBattery       EntityKind = "battery"
	KindSleepConfidence EntityKind = "sleep_confidence"
	KindRespiratoryRate EntityKind = "respiratory_rate"
	KindOxygenSaturation EntityKind = "oxygen_saturation"
	KindBodyTemperature EntityKind = "body_temperature"
	KindBloodGlucose  EntityKind = "blood_glucose"
	KindSystolicBP    EntityKind = "systolic_blood_pressure"
	KindDiastolicBP   EntityKind = "diastolic_blood_pressure"
)

// HAEntity declares one HA sensor and the lifetract-domain Kind it serves.
type HAEntity struct {
	EntityID string     // Home Assistant entity_id
	Kind     EntityKind // lifetract semantic category
	Unit     string     // canonical unit (matches HA unit_of_measurement when set)
}

// KnownEntities is the source-of-truth list. Add one line per sensor lifetract
// wants to consume. The order is irrelevant; lookups happen via maps below.
//
// Kept lean on purpose: only sensors that genuinely feed lifetract's domain
// belong here. The full set of 38 glgman entities is discoverable at runtime
// via `lifetract ha entities`.
var KnownEntities = []HAEntity{
	// Sleep
	{"sensor.sm_s942n_s26_glgman_sleep_duration", KindSleepDuration, "min"},
	{"sensor.sm_s942n_s26_glgman_sleep_confidence", KindSleepConfidence, "%"},

	// Movement / day totals
	{"sensor.sm_s942n_s26_glgman_daily_steps", KindStepsDaily, "steps"},
	{"sensor.sm_s942n_s26_glgman_daily_distance", KindDistanceDaily, "m"},
	{"sensor.sm_s942n_s26_glgman_daily_floors", KindFloorsDaily, "floors"},

	// Heart
	{"sensor.sm_s942n_s26_glgman_heart_rate", KindHeartRate, "bpm"},
	{"sensor.sm_s942n_s26_glgman_resting_heart_rate", KindRestingHR, "bpm"},
	{"sensor.sm_s942n_s26_glgman_heart_rate_variability", KindHRV, "ms"},

	// Body composition
	{"sensor.sm_s942n_s26_glgman_weight", KindWeight, "g"},
	{"sensor.sm_s942n_s26_glgman_body_fat", KindBodyFat, "%"},
	{"sensor.sm_s942n_s26_glgman_height", KindHeight, "m"},

	// Energy
	{"sensor.sm_s942n_s26_glgman_total_calories_burned", KindCalories, "kcal"},
	{"sensor.sm_s942n_s26_glgman_active_calories_burned", KindActiveCalories, "kcal"},
	{"sensor.sm_s942n_s26_glgman_basal_metabolic_rate", KindBMR, "kcal/day"},
	{"sensor.sm_s942n_s26_glgman_daily_hydration", KindHydration, "mL"},

	// Context
	{"sensor.sm_s942n_s26_glgman_detected_activity", KindActivity, ""},
	{"sensor.sm_s942n_s26_glgman_geocoded_location", KindLocation, ""},
	{"sensor.sm_s942n_s26_glgman_battery_level", KindBattery, "%"},

	// Vitals (mostly "unknown" until a measurement is taken — kept here so the
	// registry is honest about what HA exposes, even if values are stale)
	{"sensor.sm_s942n_s26_glgman_respiratory_rate", KindRespiratoryRate, "bpm"},
	{"sensor.sm_s942n_s26_glgman_oxygen_saturation", KindOxygenSaturation, "%"},
	{"sensor.sm_s942n_s26_glgman_body_temperature", KindBodyTemperature, "°C"},
	{"sensor.sm_s942n_s26_glgman_blood_glucose", KindBloodGlucose, "mg/dL"},
	{"sensor.sm_s942n_s26_glgman_systolic_blood_pressure", KindSystolicBP, "mmHg"},
	{"sensor.sm_s942n_s26_glgman_diastolic_blood_pressure", KindDiastolicBP, "mmHg"},
}

// EntitiesByKind groups KnownEntities by domain Kind. Built once at package init
// — call sites stay O(1).
var EntitiesByKind = func() map[EntityKind][]HAEntity {
	m := make(map[EntityKind][]HAEntity, len(KnownEntities))
	for _, e := range KnownEntities {
		m[e.Kind] = append(m[e.Kind], e)
	}
	return m
}()

// EntityByID returns the registered HAEntity for an entity_id, if any.
func EntityByID(id string) (HAEntity, bool) {
	for _, e := range KnownEntities {
		if e.EntityID == id {
			return e, true
		}
	}
	return HAEntity{}, false
}

// ResolveEntityRef accepts either a domain Kind ("heart_rate") or a literal
// HA entity_id ("sensor.sm_s942n_s26_glgman_heart_rate") and returns the
// concrete entity_id to fetch. When a Kind maps to multiple entities, the
// first registered one wins (callers needing all of them should iterate
// EntitiesByKind directly).
func ResolveEntityRef(ref string) (string, bool) {
	if es, ok := EntitiesByKind[EntityKind(ref)]; ok && len(es) > 0 {
		return es[0].EntityID, true
	}
	if _, ok := EntityByID(ref); ok {
		return ref, true
	}
	return "", false
}
