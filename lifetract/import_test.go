package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecImport(t *testing.T) {
	// Use temp dir for the DB
	tmpDir := t.TempDir()
	cfg := &Config{
		DataDir:        tmpDir,
		ShealthDir:     "testdata/samsunghealth",
		ShealthDirs:    []string{"testdata/samsunghealth"},
		ATimeLoggerDB:  "testdata/nonexistent.db3", // will fail gracefully
		Days:           9999,
		Exec:           true,
	}

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalRows == 0 {
		t.Error("total_rows should be > 0")
	}
	if result.DBSizeMB <= 0 {
		t.Error("db_size_mb should be > 0")
	}

	// Verify DB was created
	dbFile := filepath.Join(tmpDir, "lifetract.db")
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("DB file not created: %v", err)
	}

	// Verify tables have data
	db, err := openDB(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tables := map[string]int{
		"sleep":       3,
		"sleep_stage": 6,
		"heart_rate":  5,
		"steps_daily": 3,
		"stress":      3,
		"exercise":    2,
	}

	for table, expectedMin := range tables {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if err != nil {
			t.Errorf("%s: query error: %v", table, err)
			continue
		}
		if count < expectedMin {
			t.Errorf("%s: got %d rows, want >= %d", table, count, expectedMin)
		}
	}

	// Verify import_log
	var logCount int
	db.QueryRow("SELECT COUNT(*) FROM import_log").Scan(&logCount)
	if logCount == 0 {
		t.Error("import_log should have entries")
	}
}

func TestExecImportIdempotent(t *testing.T) {
	// Running import twice should not duplicate data
	tmpDir := t.TempDir()
	cfg := &Config{
		DataDir:     tmpDir,
		ShealthDir:  "testdata/samsunghealth",
		ShealthDirs: []string{"testdata/samsunghealth"},
		ATimeLoggerDB: "testdata/nonexistent.db3",
		Days:        9999,
		Exec:        true,
	}

	r1, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	r2, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if r1.TotalRows != r2.TotalRows {
		t.Errorf("idempotent import: r1=%d r2=%d", r1.TotalRows, r2.TotalRows)
	}
}

func TestParseIntFloat(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"85", 85},
		{"85.0", 85},
		{"0", 0},
		{"", 0},
		{"  42  ", 42},
	}
	for _, tt := range tests {
		got := parseInt(tt.input)
		if got != tt.want {
			t.Errorf("parseInt(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"72.5", 72.5},
		{"0", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseFloat(tt.input)
		if got != tt.want {
			t.Errorf("parseFloat(%q) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	rec := map[string]string{
		"a": "",
		"b": "hello",
		"c": "world",
	}
	got := firstNonEmpty(rec, "a", "b", "c")
	if got != "hello" {
		t.Errorf("firstNonEmpty = %q, want hello", got)
	}

	got2 := firstNonEmpty(rec, "x", "y")
	if got2 != "" {
		t.Errorf("firstNonEmpty missing = %q, want empty", got2)
	}
}
