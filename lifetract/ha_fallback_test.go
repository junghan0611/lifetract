package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockHAFallback wires the live entity_ids that ha_fallback queries to fake
// state / history payloads. Per-path: exact path → body. Per-prefix is used
// for the history endpoint (the path embeds a timestamp).
func mockHAFallback(t *testing.T, states map[string]string, history map[string]string) *HAClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if body, ok := states[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, body)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/history/period/") {
			entity := r.URL.Query().Get("filter_entity_id")
			if body, ok := history[entity]; ok {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
				return
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return &HAClient{
		BaseURL: srv.URL,
		Token:   "test-token",
		HTTP:    srv.Client(),
	}
}

// TestEnrichTodayFillsGapsFromHA — DB returned all-zero, HA fills it in.
func TestEnrichTodayFillsGapsFromHA(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	c := mockHAFallback(t,
		map[string]string{
			"/api/states/sensor.sm_s942n_s26_glgman_daily_steps": fmt.Sprintf(`{
				"entity_id":"sensor.sm_s942n_s26_glgman_daily_steps",
				"state":"4771","attributes":{"unit_of_measurement":"steps"},
				"last_changed":%q,"last_updated":%q}`, now, now),
			"/api/states/sensor.sm_s942n_s26_glgman_heart_rate": fmt.Sprintf(`{
				"entity_id":"sensor.sm_s942n_s26_glgman_heart_rate",
				"state":"127","attributes":{"unit_of_measurement":"bpm"},
				"last_changed":%q,"last_updated":%q}`, now, now),
		},
		map[string]string{
			"sensor.sm_s942n_s26_glgman_sleep_duration": fmt.Sprintf(`[[
				{"entity_id":"sensor.sm_s942n_s26_glgman_sleep_duration","state":"104","attributes":{"unit_of_measurement":"min"},"last_changed":%q,"last_updated":%q},
				{"entity_id":"sensor.sm_s942n_s26_glgman_sleep_duration","state":"185","attributes":{"unit_of_measurement":"min"},"last_changed":%q,"last_updated":%q}
			]]`, now, now, now, now),
		},
	)

	cfg := &Config{DataDir: "testdata"} // no DB → todaySleepStale returns false
	result := &TodayResult{
		Date:   time.Now().Format("2006-01-02"),
		Source: "db",
	}

	enrichTodayFromHAClient(cfg, result, c)

	if result.Steps != 4771 {
		t.Errorf("Steps = %d, want 4771", result.Steps)
	}
	if result.AvgHR != 127 {
		t.Errorf("AvgHR = %v, want 127", result.AvgHR)
	}
	// 104 + 185 = 289 min = 4.8 h
	if result.SleepHours < 4.7 || result.SleepHours > 4.9 {
		t.Errorf("SleepHours = %v, want ~4.8", result.SleepHours)
	}
	if result.Source != "db+ha" {
		t.Errorf("Source = %q, want %q", result.Source, "db+ha")
	}
	if len(result.HASources) != 3 {
		t.Errorf("HASources = %v, want 3 entries", result.HASources)
	}
}

// TestEnrichTodaySkipsFilledFields — when DB already has values, HA isn't
// queried for those slots. We confirm by leaving HA unmocked for steps and
// expecting no error (steps stays as-is, no panic).
func TestEnrichTodaySkipsFilledFields(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	c := mockHAFallback(t,
		// Only register sleep + heart endpoints. Steps endpoint is NOT registered
		// → if enrich tries to hit it, the mock returns 404 and the path is skipped.
		map[string]string{
			"/api/states/sensor.sm_s942n_s26_glgman_heart_rate": fmt.Sprintf(`{
				"entity_id":"sensor.sm_s942n_s26_glgman_heart_rate",
				"state":"80","attributes":{"unit_of_measurement":"bpm"},
				"last_changed":%q,"last_updated":%q}`, now, now),
		},
		map[string]string{
			"sensor.sm_s942n_s26_glgman_sleep_duration": `[[]]`,
		},
	)

	cfg := &Config{DataDir: "testdata"}
	result := &TodayResult{
		Date:       time.Now().Format("2006-01-02"),
		Source:     "db",
		Steps:      9999,    // already set → no HA
		SleepHours: 7.5,     // already set → no HA (and not stale: no DB)
		AvgHR:      0,       // gap → HA fills
	}

	enrichTodayFromHAClient(cfg, result, c)

	if result.Steps != 9999 {
		t.Errorf("Steps overwritten: %d", result.Steps)
	}
	if result.SleepHours != 7.5 {
		t.Errorf("SleepHours overwritten: %v", result.SleepHours)
	}
	if result.AvgHR != 80 {
		t.Errorf("AvgHR = %v, want 80", result.AvgHR)
	}
	if len(result.HASources) != 1 || result.HASources[0] != "heart_rate" {
		t.Errorf("HASources = %v, want [heart_rate]", result.HASources)
	}
	if result.Source != "db+ha" {
		t.Errorf("Source = %q, want %q", result.Source, "db+ha")
	}
}

// TestEnrichTodayHistoryIgnoresOldEntries — sleep entries older than 36h
// must be filtered out. Mix old + recent → only recent counts.
func TestEnrichTodayHistoryIgnoresOldEntries(t *testing.T) {
	old := time.Now().Add(-72 * time.Hour).UTC().Format("2006-01-02T15:04:05Z07:00")
	recent := time.Now().Add(-2 * time.Hour).UTC().Format("2006-01-02T15:04:05Z07:00")
	c := mockHAFallback(t,
		map[string]string{},
		map[string]string{
			"sensor.sm_s942n_s26_glgman_sleep_duration": fmt.Sprintf(`[[
				{"entity_id":"sensor.sm_s942n_s26_glgman_sleep_duration","state":"500","attributes":{"unit_of_measurement":"min"},"last_changed":%q,"last_updated":%q},
				{"entity_id":"sensor.sm_s942n_s26_glgman_sleep_duration","state":"60","attributes":{"unit_of_measurement":"min"},"last_changed":%q,"last_updated":%q}
			]]`, old, old, recent, recent),
		},
	)

	cfg := &Config{DataDir: "testdata"}
	result := &TodayResult{
		Date:       time.Now().Format("2006-01-02"),
		Source:     "db",
		Steps:      1,
		AvgHR:      1,
		SleepHours: 0,
	}

	enrichTodayFromHAClient(cfg, result, c)

	// 500 (old, dropped) + 60 (recent) → only 60min = 1.0h
	if result.SleepHours < 0.9 || result.SleepHours > 1.1 {
		t.Errorf("SleepHours = %v, want ~1.0 (old entry should be dropped)", result.SleepHours)
	}
}

// TestEnrichTodaySleepByEndTimeDay — the real-world inflation bug: HA history
// holds whole sessions from several nights (synced late, so LastChanged lands
// them all inside the 36h window). Only sessions whose endTime is *today* may
// be summed; prior nights must be excluded. Two of today's segments (a split
// overnight) sum; yesterday's and a two-days-ago session drop.
func TestEnrichTodaySleepByEndTimeDay(t *testing.T) {
	now := time.Now()
	// endTime attributes: two today (split overnight), one yesterday, one older.
	todayEndA := now.Add(-9 * time.Hour).Format(time.RFC3339)
	todayEndB := now.Add(-7 * time.Hour).Format(time.RFC3339)
	yestEnd := now.AddDate(0, 0, -1).Format(time.RFC3339)
	oldEnd := now.AddDate(0, 0, -2).Format(time.RFC3339)
	// All synced recently (LastChanged within 36h) — the old buggy path summed all.
	lc := now.Add(-1 * time.Hour).UTC().Format("2006-01-02T15:04:05Z07:00")
	mk := func(state, end string) string {
		return fmt.Sprintf(`{"entity_id":"sensor.sm_s942n_s26_glgman_sleep_duration","state":%q,"attributes":{"unit_of_measurement":"min","endTime":%q},"last_changed":%q,"last_updated":%q}`, state, end, lc, lc)
	}
	c := mockHAFallback(t,
		map[string]string{},
		map[string]string{
			"sensor.sm_s942n_s26_glgman_sleep_duration": "[[" +
				mk("385", oldEnd) + "," + mk("395", yestEnd) + "," +
				mk("228", todayEndA) + "," + mk("127", todayEndB) + "]]",
		},
	)

	cfg := &Config{DataDir: "testdata"}
	result := &TodayResult{Date: now.Format("2006-01-02"), Source: "db", SleepHours: 0}
	enrichTodayFromHAClient(cfg, result, c)

	// 228 + 127 = 355 min = 5.9h. NOT 385+395+228+127 = 18.9h.
	if result.SleepHours < 5.8 || result.SleepHours > 6.0 {
		t.Errorf("SleepHours = %v, want ~5.9 (today's two segments only)", result.SleepHours)
	}
}

// TestEnrichTodaySleepDedupsResync — the same session re-emitted (identical
// endTime) must count once, not twice, even if the value was updated.
func TestEnrichTodaySleepDedupsResync(t *testing.T) {
	now := time.Now()
	end := now.Add(-6 * time.Hour).Format(time.RFC3339)
	lc := now.Add(-1 * time.Hour).UTC().Format("2006-01-02T15:04:05Z07:00")
	mk := func(state string) string {
		return fmt.Sprintf(`{"entity_id":"sensor.sm_s942n_s26_glgman_sleep_duration","state":%q,"attributes":{"unit_of_measurement":"min","endTime":%q},"last_changed":%q,"last_updated":%q}`, state, end, lc, lc)
	}
	c := mockHAFallback(t,
		map[string]string{},
		map[string]string{
			"sensor.sm_s942n_s26_glgman_sleep_duration": "[[" + mk("400") + "," + mk("420") + "]]",
		},
	)
	cfg := &Config{DataDir: "testdata"}
	result := &TodayResult{Date: now.Format("2006-01-02"), Source: "db", SleepHours: 0}
	enrichTodayFromHAClient(cfg, result, c)

	// Same endTime → keep max 420min = 7.0h, not 400+420 = 13.7h.
	if result.SleepHours < 6.9 || result.SleepHours > 7.1 {
		t.Errorf("SleepHours = %v, want ~7.0 (dedup by endTime)", result.SleepHours)
	}
}

// TestEnrichTodayUnreachableHASilent — when HA returns errors, fallback
// must NOT mutate the result.
func TestEnrichTodayUnreachableHASilent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := &HAClient{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()}

	cfg := &Config{DataDir: "testdata"}
	result := &TodayResult{
		Date:   time.Now().Format("2006-01-02"),
		Source: "db",
	}

	enrichTodayFromHAClient(cfg, result, c)

	if result.Steps != 0 || result.AvgHR != 0 || result.SleepHours != 0 {
		t.Errorf("unexpected mutation: %+v", result)
	}
	if result.Source != "db" {
		t.Errorf("Source = %q, want unchanged 'db'", result.Source)
	}
	if len(result.HASources) != 0 {
		t.Errorf("HASources = %v, want empty", result.HASources)
	}
}

// TestHaFallbackDisabledByEnv — LIFETRACT_NO_HA=1 disables the fallback
// at the entry point.
func TestHaFallbackDisabledByEnv(t *testing.T) {
	t.Setenv("LIFETRACT_NO_HA", "1")
	if dialHAFallback() != nil {
		t.Error("LIFETRACT_NO_HA=1 should disable HA fallback")
	}
}

// TestEnrichTimelineEntryFillsHealthFromHA — TimelineEntry path used by
// cmdRead today / dbQueryDay today.
func TestEnrichTimelineEntryFillsHealthFromHA(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	c := mockHAFallback(t,
		map[string]string{
			"/api/states/sensor.sm_s942n_s26_glgman_daily_steps": fmt.Sprintf(`{
				"entity_id":"sensor.sm_s942n_s26_glgman_daily_steps",
				"state":"3210","attributes":{"unit_of_measurement":"steps"},
				"last_changed":%q,"last_updated":%q}`, now, now),
			"/api/states/sensor.sm_s942n_s26_glgman_heart_rate": fmt.Sprintf(`{
				"entity_id":"sensor.sm_s942n_s26_glgman_heart_rate",
				"state":"68","attributes":{"unit_of_measurement":"bpm"},
				"last_changed":%q,"last_updated":%q}`, now, now),
		},
		map[string]string{
			"sensor.sm_s942n_s26_glgman_sleep_duration": fmt.Sprintf(`[[
				{"entity_id":"sensor.sm_s942n_s26_glgman_sleep_duration","state":"420","attributes":{"unit_of_measurement":"min"},"last_changed":%q,"last_updated":%q}
			]]`, now, now),
		},
	)

	cfg := &Config{DataDir: "testdata"}
	entry := &TimelineEntry{
		ID:   "20260526T000000",
		Date: time.Now().Format("2006-01-02"),
		// Health is nil — empty day
	}

	enrichTimelineEntryFromHAClient(cfg, entry, c)

	if entry.Health == nil {
		t.Fatal("Health should be populated")
	}
	if entry.Health.Steps != 3210 {
		t.Errorf("Steps = %d, want 3210", entry.Health.Steps)
	}
	if entry.Health.AvgHR != 68 {
		t.Errorf("AvgHR = %v, want 68", entry.Health.AvgHR)
	}
	if entry.Health.SleepHours != 7.0 {
		t.Errorf("SleepHours = %v, want 7.0", entry.Health.SleepHours)
	}
	if len(entry.HASources) != 3 {
		t.Errorf("HASources = %v, want 3 entries", entry.HASources)
	}
}

func TestIsToday(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	if !isToday(today) {
		t.Error("isToday(today) should be true")
	}
	if isToday("2020-01-01") {
		t.Error("isToday(2020-01-01) should be false")
	}
}
