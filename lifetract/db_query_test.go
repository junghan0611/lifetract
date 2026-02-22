package main

import (
	"testing"
)

// TestDBQueryAfterImport tests all DB query functions after importing test data.
func TestDBQueryAfterImport(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		DataDir:        tmpDir,
		ShealthDir:     "testdata/samsunghealth",
		ShealthDirs:    []string{"testdata/samsunghealth"},
		ATimeLoggerDB:  "testdata/nonexistent.db3",
		Days:           9999,
		Exec:           true,
	}

	// Import first
	_, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Now test queries
	t.Run("sleep", func(t *testing.T) {
		records, err := dbQuerySleep(cfg, 9999)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) < 3 {
			t.Errorf("sleep: got %d, want >= 3", len(records))
		}
		for _, r := range records {
			if r.ID == "" || r.Date == "" {
				t.Errorf("sleep record missing ID or Date: %+v", r)
			}
			if r.DurationHours <= 0 {
				t.Errorf("sleep duration <= 0: %+v", r)
			}
		}
	})

	t.Run("steps", func(t *testing.T) {
		records, err := dbQuerySteps(cfg, 9999)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) < 2 {
			t.Errorf("steps: got %d, want >= 2", len(records))
		}
		for _, r := range records {
			if r.Steps <= 0 {
				t.Errorf("steps <= 0: %+v", r)
			}
		}
	})

	t.Run("heart", func(t *testing.T) {
		records, err := dbQueryHeart(cfg, 9999)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) < 2 {
			t.Errorf("heart: got %d, want >= 2", len(records))
		}
		for _, r := range records {
			if r.AvgHR <= 0 {
				t.Errorf("heart avg <= 0: %+v", r)
			}
		}
	})

	t.Run("stress", func(t *testing.T) {
		records, err := dbQueryStress(cfg, 9999)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) < 2 {
			t.Errorf("stress: got %d, want >= 2", len(records))
		}
	})

	t.Run("exercise", func(t *testing.T) {
		records, err := dbQueryExercise(cfg, 9999)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) < 2 {
			t.Errorf("exercise: got %d, want >= 2", len(records))
		}
		for _, r := range records {
			if r.DurationMinutes <= 0 {
				t.Errorf("exercise duration <= 0: %+v", r)
			}
		}
	})

	t.Run("timeline", func(t *testing.T) {
		entries, err := dbQueryTimeline(cfg, 9999)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) < 2 {
			t.Errorf("timeline: got %d, want >= 2", len(entries))
		}
		// Verify sorted desc
		for i := 1; i < len(entries); i++ {
			if entries[i].Date > entries[i-1].Date {
				t.Errorf("timeline not sorted desc: %s > %s", entries[i].Date, entries[i-1].Date)
			}
		}
	})
}

// TestDBQueryDay tests day detail from DB.
func TestDBQueryDay(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		DataDir:        tmpDir,
		ShealthDir:     "testdata/samsunghealth",
		ShealthDirs:    []string{"testdata/samsunghealth"},
		ATimeLoggerDB:  "testdata/nonexistent.db3",
		Days:           9999,
		Exec:           true,
	}

	_, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Query a day that exists in test data (2025-10-04)
	day, _ := parseDenoteID("20251004T000000")
	result, err := dbQueryDay(cfg, day)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("day result is nil")
	}
}

// TestDBQueryEvent tests event lookup from DB.
func TestDBQueryEvent(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		DataDir:        tmpDir,
		ShealthDir:     "testdata/samsunghealth",
		ShealthDirs:    []string{"testdata/samsunghealth"},
		ATimeLoggerDB:  "testdata/nonexistent.db3",
		Days:           9999,
		Exec:           true,
	}

	_, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Query non-existent event
	day, _ := parseDenoteID("20990101T120000")
	_, err = dbQueryEvent(cfg, day, "20990101T120000")
	if err == nil {
		t.Error("expected error for non-existent event")
	}
}

// TestDBModeInStatus verifies status shows DB mode.
func TestDBModeInStatus(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		DataDir:        tmpDir,
		ShealthDir:     "testdata/samsunghealth",
		ShealthDirs:    []string{"testdata/samsunghealth"},
		ATimeLoggerDB:  "testdata/nonexistent.db3",
		Days:           7,
	}

	// Before import: csv mode
	result, _ := cmdStatus(cfg)
	status := result.(*StatusResult)
	if status.Database.Mode != "csv" {
		t.Errorf("before import: mode = %q, want csv", status.Database.Mode)
	}

	// After import: db mode
	cfg.Exec = true
	execImport(cfg)
	cfg.Exec = false

	result, _ = cmdStatus(cfg)
	status = result.(*StatusResult)
	if status.Database.Mode != "db" {
		t.Errorf("after import: mode = %q, want db", status.Database.Mode)
	}
	if status.Database.SizeMB <= 0 {
		t.Error("db size should be > 0")
	}
}
