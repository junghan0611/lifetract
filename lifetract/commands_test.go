package main

import (
	"encoding/json"
	"testing"
)

// testConfig returns a Config pointing to testdata fixtures.
func testConfig(days int) *Config {
	return &Config{
		DataDir:       "testdata",
		ShealthDir:    "testdata/samsunghealth",
		ATimeLoggerDB: "testdata/atimelogger/database.db3", // doesn't exist → reports unavailable
		Days:          days,
	}
}

// --- SKILL.md: status ---

func TestCmdStatus(t *testing.T) {
	cfg := testConfig(7)
	result, err := cmdStatus(cfg)
	if err != nil {
		t.Fatal(err)
	}

	sr, ok := result.(*StatusResult)
	if !ok {
		t.Fatal("expected *StatusResult")
	}

	if !sr.SamsungHealth.Available {
		t.Error("samsung_health should be available")
	}
	if sr.SamsungHealth.CSVCount == 0 {
		t.Error("csv_count should be > 0")
	}
	if sr.ATimeLogger.Available {
		t.Error("atimelogger should be unavailable in testdata")
	}

	// Verify JSON serializable
	b, err := json.Marshal(sr)
	if err != nil {
		t.Fatal("json marshal:", err)
	}
	if len(b) == 0 {
		t.Error("empty json")
	}
}

// --- SKILL.md: sleep ---

func TestCmdSleep(t *testing.T) {
	cfg := testConfig(9999)
	result, err := cmdSleep(cfg)
	if err != nil {
		t.Fatal(err)
	}

	records, ok := result.([]SleepRecord)
	if !ok {
		t.Fatal("expected []SleepRecord")
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 sleep records, got %d", len(records))
	}

	// Verify Denote ID format
	for _, r := range records {
		if len(r.ID) != 15 || r.ID[8] != 'T' {
			t.Errorf("invalid denote ID: %q", r.ID)
		}
		if len(r.Date) != 10 || r.Date[4] != '-' {
			t.Errorf("invalid date: %q", r.Date)
		}
		if r.DurationHours <= 0 {
			t.Errorf("duration should be positive: %f", r.DurationHours)
		}
	}

	// First record (sorted desc) should be 2025-01-17
	if records[0].Date != "2025-01-17" {
		t.Errorf("first date = %q, want 2025-01-17", records[0].Date)
	}

	// Check sleep score parsed
	first := records[0]
	if first.SleepScore == 0 {
		t.Error("sleep_score should be parsed")
	}
}

func TestCmdSleepSummary(t *testing.T) {
	cfg := testConfig(9999)
	cfg.Summary = true
	result, err := cmdSleep(cfg)
	if err != nil {
		t.Fatal(err)
	}

	summary, ok := result.(*SleepSummary)
	if !ok {
		t.Fatal("expected *SleepSummary")
	}
	if summary.Sessions != 3 {
		t.Errorf("sessions = %d, want 3", summary.Sessions)
	}
	if summary.AvgDuration <= 0 {
		t.Error("avg_duration should be positive")
	}
}

// --- SKILL.md: sleep with stages ---

func TestSleepStages(t *testing.T) {
	cfg := testConfig(9999)
	result, err := cmdSleep(cfg)
	if err != nil {
		t.Fatal(err)
	}

	records := result.([]SleepRecord)

	// Find record for 2025-01-15 (sleep-uuid-001 has stages)
	var found *SleepRecord
	for i := range records {
		if records[i].ID == "20250115T233000" {
			found = &records[i]
			break
		}
	}
	if found == nil {
		t.Fatal("sleep record 20250115T233000 not found")
	}

	if found.Stages == nil {
		t.Fatal("stages should not be nil for sleep-uuid-001")
	}

	// Stage breakdown: Light=15+150=165, Deep=45+120=165, REM=90, Awake=12
	// (from testdata sleep_stage CSV)
	if found.Stages.LightMin == 0 {
		t.Error("light_min should be > 0")
	}
	if found.Stages.DeepMin == 0 {
		t.Error("deep_min should be > 0")
	}
}

// --- SKILL.md: steps ---

func TestCmdSteps(t *testing.T) {
	cfg := testConfig(9999)
	result, err := cmdSteps(cfg)
	if err != nil {
		t.Fatal(err)
	}

	records, ok := result.([]StepRecord)
	if !ok {
		t.Fatal("expected []StepRecord")
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 step records, got %d", len(records))
	}

	// Verify Denote Day ID format YYYYMMDDT000000
	for _, r := range records {
		if len(r.ID) != 15 || r.ID[8] != 'T' || r.ID[9:] != "000000" {
			t.Errorf("invalid day denote ID: %q", r.ID)
		}
		if r.Steps <= 0 {
			t.Errorf("steps should be positive: %d", r.Steps)
		}
	}

	// Check specific values
	for _, r := range records {
		if r.Date == "2025-01-15" && r.Steps != 8432 {
			t.Errorf("2025-01-15 steps = %d, want 8432", r.Steps)
		}
	}
}

// --- SKILL.md: heart ---

func TestCmdHeart(t *testing.T) {
	cfg := testConfig(9999)
	result, err := cmdHeart(cfg)
	if err != nil {
		t.Fatal(err)
	}

	records, ok := result.([]HeartRecord)
	if !ok {
		t.Fatal("expected []HeartRecord")
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 heart records (2 days), got %d", len(records))
	}

	for _, r := range records {
		if r.AvgHR <= 0 {
			t.Errorf("avg_hr should be positive: %f", r.AvgHR)
		}
		if r.MinHR <= 0 {
			t.Errorf("min_hr should be positive: %d", r.MinHR)
		}
		if r.MaxHR < r.MinHR {
			t.Errorf("max_hr < min_hr: %d < %d", r.MaxHR, r.MinHR)
		}
		if r.Samples <= 0 {
			t.Errorf("samples should be positive: %d", r.Samples)
		}
	}

	// 2025-01-15: 3 readings (72, 85, 65) → avg=74.0, min=65, max=85
	for _, r := range records {
		if r.Date == "2025-01-15" {
			if r.Samples != 3 {
				t.Errorf("2025-01-15 samples = %d, want 3", r.Samples)
			}
			if r.MinHR != 65 {
				t.Errorf("2025-01-15 min_hr = %d, want 65", r.MinHR)
			}
			if r.MaxHR != 85 {
				t.Errorf("2025-01-15 max_hr = %d, want 85", r.MaxHR)
			}
		}
	}
}

// --- SKILL.md: stress ---

func TestCmdStress(t *testing.T) {
	cfg := testConfig(9999)
	result, err := cmdStress(cfg)
	if err != nil {
		t.Fatal(err)
	}

	records, ok := result.([]StressRecord)
	if !ok {
		t.Fatal("expected []StressRecord")
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 stress records, got %d", len(records))
	}

	for _, r := range records {
		if r.ID == "" {
			t.Error("stress record should have denote ID")
		}
		// 2025-01-15: 2 readings (45, 62) → avg=53.5
		if r.Date == "2025-01-15" {
			if r.Samples != 2 {
				t.Errorf("2025-01-15 samples = %d, want 2", r.Samples)
			}
			if r.AvgScore < 53 || r.AvgScore > 54 {
				t.Errorf("2025-01-15 avg_score = %f, want ~53.5", r.AvgScore)
			}
		}
	}
}

// --- SKILL.md: exercise ---

func TestCmdExercise(t *testing.T) {
	cfg := testConfig(9999)
	result, err := cmdExercise(cfg)
	if err != nil {
		t.Fatal(err)
	}

	records, ok := result.([]ExerciseRecord)
	if !ok {
		t.Fatal("expected []ExerciseRecord")
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 exercise records, got %d", len(records))
	}

	// Check denote ID is event-level (not day-level)
	for _, r := range records {
		if len(r.ID) != 15 || r.ID[8] != 'T' {
			t.Errorf("invalid event denote ID: %q", r.ID)
		}
		if r.ID[9:] == "000000" {
			t.Errorf("exercise ID should be event-level, not day: %q", r.ID)
		}
	}

	// Check exercise types
	for _, r := range records {
		if r.Type == "" || r.Type[0:5] == "Type_" {
			t.Errorf("exercise type should be resolved: %q", r.Type)
		}
		if r.DurationMinutes <= 0 {
			t.Errorf("duration should be positive: %f", r.DurationMinutes)
		}
	}

	// Walking (1001) and Running (1002)
	types := map[string]bool{}
	for _, r := range records {
		types[r.Type] = true
	}
	if !types["Walking"] {
		t.Error("expected Walking exercise")
	}
	if !types["Running"] {
		t.Error("expected Running exercise")
	}
}

// --- SKILL.md: today ---

func TestCmdToday(t *testing.T) {
	cfg := testConfig(9999)
	result, err := cmdToday(cfg)
	if err != nil {
		t.Fatal(err)
	}

	tr, ok := result.(*TodayResult)
	if !ok {
		t.Fatal("expected *TodayResult")
	}

	if tr.Date == "" {
		t.Error("date should not be empty")
	}
	// Today may not match testdata, but the function should not crash
	_ = tr.Steps
	_ = tr.SleepHours
	_ = tr.AvgHR
	_ = tr.StressAvg
}

// --- SKILL.md: timeline ---

func TestCmdTimeline(t *testing.T) {
	cfg := testConfig(9999)
	result, err := cmdTimeline(cfg)
	if err != nil {
		t.Fatal(err)
	}

	entries, ok := result.([]TimelineEntry)
	if !ok {
		t.Fatal("expected []TimelineEntry")
	}
	if len(entries) == 0 {
		t.Fatal("timeline should not be empty")
	}

	// All entries should have Denote Day ID
	for _, e := range entries {
		if len(e.ID) != 15 || e.ID[8] != 'T' || e.ID[9:] != "000000" {
			t.Errorf("timeline ID should be day-level: %q", e.ID)
		}
		if e.Date == "" {
			t.Error("date should not be empty")
		}
	}

	// 2025-01-15 should have steps + heart + stress + exercise + sleep
	var jan15 *TimelineEntry
	for i := range entries {
		if entries[i].Date == "2025-01-15" {
			jan15 = &entries[i]
			break
		}
	}
	if jan15 == nil {
		t.Fatal("2025-01-15 not found in timeline")
	}
	if jan15.ID != "20250115T000000" {
		t.Errorf("2025-01-15 ID = %q, want 20250115T000000", jan15.ID)
	}
	if jan15.Health == nil {
		t.Fatal("2025-01-15 health should not be nil")
	}
	if jan15.Health.Steps != 8432 {
		t.Errorf("steps = %d, want 8432", jan15.Health.Steps)
	}
	if jan15.Health.AvgHR == 0 {
		t.Error("avg_hr should not be 0")
	}
	if jan15.Health.StressAvg == 0 {
		t.Error("stress_avg should not be 0")
	}
	if len(jan15.Exercise) == 0 {
		t.Error("exercise should not be empty for 2025-01-15")
	}
}

// --- SKILL.md: read (by Denote ID) ---

func TestCmdReadDayID(t *testing.T) {
	cfg := testConfig(9999)
	cfg.ReadID = "20250115T000000"
	result, err := cmdRead(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Should return a day detail
	b, _ := json.Marshal(result)
	var m map[string]interface{}
	json.Unmarshal(b, &m)

	if m["id"] != "20250115T000000" {
		t.Errorf("id = %v, want 20250115T000000", m["id"])
	}
	if m["date"] != "2025-01-15" {
		t.Errorf("date = %v, want 2025-01-15", m["date"])
	}
	if m["health"] == nil {
		t.Error("health should not be nil")
	}
}

func TestCmdReadDateShorthand(t *testing.T) {
	cfg := testConfig(9999)
	cfg.ReadID = "2025-01-15"
	result, err := cmdRead(cfg)
	if err != nil {
		t.Fatal(err)
	}

	b, _ := json.Marshal(result)
	var m map[string]interface{}
	json.Unmarshal(b, &m)

	if m["id"] != "20250115T000000" {
		t.Errorf("id = %v, want 20250115T000000", m["id"])
	}
}

func TestCmdReadEventID(t *testing.T) {
	cfg := testConfig(9999)
	cfg.ReadID = "20250115T233000" // sleep session
	result, err := cmdRead(cfg)
	if err != nil {
		t.Fatal(err)
	}

	sr, ok := result.(SleepRecord)
	if !ok {
		t.Fatalf("expected SleepRecord, got %T", result)
	}
	if sr.ID != "20250115T233000" {
		t.Errorf("id = %q, want 20250115T233000", sr.ID)
	}
	if sr.DurationHours <= 0 {
		t.Error("duration should be positive")
	}
}

func TestCmdReadNotFound(t *testing.T) {
	cfg := testConfig(9999)
	cfg.ReadID = "20990101T120000"
	_, err := cmdRead(cfg)
	if err == nil {
		t.Error("expected error for non-existent event")
	}
}

func TestCmdReadNoID(t *testing.T) {
	cfg := testConfig(9999)
	cfg.ReadID = ""
	_, err := cmdRead(cfg)
	if err == nil {
		t.Error("expected error for empty ID")
	}
}

// --- SKILL.md: time (aTimeLogger stub) ---

func TestCmdTime(t *testing.T) {
	cfg := testConfig(9999)
	result, err := cmdTime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Should return a map with error/note since DB doesn't exist in testdata
	b, _ := json.Marshal(result)
	var m map[string]interface{}
	json.Unmarshal(b, &m)

	if m["error"] == nil && m["note"] == nil {
		t.Error("expected error or note for missing aTimeLogger DB")
	}
}

// --- JSON output format ---

func TestJSONOutputFormat(t *testing.T) {
	cfg := testConfig(9999)

	// All commands should produce valid JSON
	commands := []struct {
		name string
		fn   func(*Config) (interface{}, error)
	}{
		{"status", cmdStatus},
		{"today", cmdToday},
		{"timeline", cmdTimeline},
		{"sleep", cmdSleep},
		{"steps", cmdSteps},
		{"heart", cmdHeart},
		{"stress", cmdStress},
		{"exercise", cmdExercise},
		{"time", cmdTime},
	}

	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			result, err := cmd.fn(cfg)
			if err != nil {
				t.Skipf("command error (expected for some): %v", err)
			}

			b, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("json marshal failed: %v", err)
			}
			if len(b) == 0 {
				t.Error("empty JSON output")
			}

			// Verify it's valid JSON by unmarshaling
			var v interface{}
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
		})
	}
}
