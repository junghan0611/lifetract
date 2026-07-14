package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecImport(t *testing.T) {
	// Use temp dir for the DB
	tmpDir := t.TempDir()
	cfg := &Config{
		DataDir:       tmpDir,
		ShealthDir:    "testdata/samsunghealth",
		ShealthDirs:   []string{"testdata/samsunghealth"},
		ATimeLoggerDB: fakeATL(t, filepath.Join(tmpDir, "atimelogger", "database.db3")),
		Days:          9999,
		Exec:          true,
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
		DataDir:       tmpDir,
		ShealthDir:    "testdata/samsunghealth",
		ShealthDirs:   []string{"testdata/samsunghealth"},
		ATimeLoggerDB: "testdata/nonexistent.db3",
		Days:          9999,
		Exec:          true,
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

func TestNumRowInt(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantBad bool
	}{
		{"85", 85, false},
		{"85.0", 85, false}, // the export writes integers as floats
		{"0", 0, false},
		{"", 0, false}, // absent is a value the export does not have
		{"  42  ", 42, false},
		{"garbage", 0, true}, // present and unreadable: the file changed shape
	}
	for _, tt := range tests {
		var n numRow
		got := n.int("f", tt.input)
		if got != tt.want || n.bad() != tt.wantBad {
			t.Errorf("int(%q) = %d, bad=%v; want %d, bad=%v", tt.input, got, n.bad(), tt.want, tt.wantBad)
		}
	}
}

func TestNumRowFloat(t *testing.T) {
	tests := []struct {
		input   string
		want    float64
		wantBad bool
	}{
		{"72.5", 72.5, false},
		{"0", 0, false},
		{"", 0, false},
		{"garbage", 0, true},
	}
	for _, tt := range tests {
		var n numRow
		got := n.float("f", tt.input)
		if got != tt.want || n.bad() != tt.wantBad {
			t.Errorf("float(%q) = %f, bad=%v; want %f, bad=%v", tt.input, got, n.bad(), tt.want, tt.wantBad)
		}
	}
}

// One bad field poisons the row, and the row keeps the FIRST complaint — so the
// message names the field that actually broke, not the last one read after it.
func TestNumRowRemembersTheFirstBadField(t *testing.T) {
	var n numRow
	n.float("score", "garbage")
	n.float("min", "also-garbage")
	if !n.bad() {
		t.Fatal("row with two unreadable fields reported clean")
	}
	if !strings.Contains(n.err.Error(), "score") {
		t.Errorf("error names %v, want the first bad field (score)", n.err)
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
