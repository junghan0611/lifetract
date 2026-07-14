package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The ledger records what the DB holds, not what the importer meant to give it.
//
// Every importer used to `count++` after a bare `stmt.Exec(...)`, so a row the
// database refused still counted as imported. The ledger stored that number, and
// the loss guard measured the next run against it — a guard whose baseline is a
// number the import made up.

// A duplicate id is dropped by INSERT OR IGNORE. The row never lands, so it must
// never be counted as landed, and the gap must be said out loud.
func TestRowsAreCountedInTheDBNotInOurIntentions(t *testing.T) {
	cfg, shealth := lossCfg(t)

	// Same uuid twice: the second insert is ignored by the DB.
	appendHeartRow(t, shealth, "2025-01-20 10:00:00.000", "hr-dup", "70.0")
	appendHeartRow(t, shealth, "2025-01-20 11:00:00.000", "hr-dup", "75.0")

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	hr := stream(t, result, "heart_rate")

	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var actual int
	if err := db.QueryRow(`SELECT COUNT(*) FROM heart_rate`).Scan(&actual); err != nil {
		t.Fatal(err)
	}

	if hr.Rows != actual {
		t.Errorf("reported %d rows, the table holds %d — the ledger is recording intentions", hr.Rows, actual)
	}
	if !strings.Contains(strings.Join(result.Rejected, "\n"), "landed") {
		t.Errorf("the DB dropped a row and nothing said so: %v", result.Rejected)
	}
}

// A source that breaks mid-read must fail, not return the rows it managed to get.
// A short count looks exactly like an ordinary smaller number — which is the shape
// every loss in this repo has had.
func TestBrokenSourceFailsInsteadOfCountingShort(t *testing.T) {
	cfg, _ := lossCfg(t)

	// aTimeLogger hands back a start time that is not a number. The scan into an
	// int fails; the old code ignored the error and simply imported fewer blocks.
	atl, err := sql.Open("sqlite", cfg.ATimeLoggerDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := atl.Exec(`INSERT INTO time_interval2 VALUES (99, 'g99', 'not-a-time', 1767243600, '', 1, 0)`); err != nil {
		t.Fatal(err)
	}
	atl.Close()

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	atlRow := stream(t, result, "atl_interval")
	if atlRow.Status == statusOK {
		t.Errorf("atl_interval = ok on a source that could not be read (rows=%d)", atlRow.Rows)
	}
	if result.Status != statusWarn {
		t.Errorf("status = %q, want warning — the source broke", result.Status)
	}
	if !strings.Contains(strings.Join(result.Warnings, "\n"), "atl_interval") {
		t.Errorf("warnings = %v, want the unreadable stream named", result.Warnings)
	}
}

// The candidate is what the numbers describe, so the count has to come from it —
// not from the live DB that is still sitting next to it.
func TestCountsComeFromTheCandidateNotTheLiveDB(t *testing.T) {
	cfg, shealth := lossCfg(t)

	if _, err := execImport(cfg); err != nil { // build a live DB first
		t.Fatal(err)
	}

	// Now gut the export: the next candidate must report the small number, even
	// though the live DB next to it still holds the big one.
	matches, _ := filepath.Glob(filepath.Join(shealth, "com.samsung.shealth.tracker.heart_rate.*.csv"))
	truncateToHeader(t, matches[0])

	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if got := stream(t, second, "heart_rate").Rows; got != 0 {
		t.Errorf("heart_rate = %d rows, want 0 — the count was read from the wrong database", got)
	}
	if second.Status != statusWarn {
		t.Errorf("status = %q, want warning — the stream went to zero", second.Status)
	}
}

// A first import that could read nothing must not become the live DB. It used to:
// with no DB to protect, any run was promoted, so a broken export installed an
// empty database and every query afterwards answered [] — the tool reporting a
// life with nothing in it because it could not open a single file.
func TestEmptyFirstImportIsNotPromoted(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		DataDir:       dir,
		ShealthDir:    filepath.Join(dir, "samsunghealth_gtgkjh"), // never created
		ATimeLoggerDB: filepath.Join(dir, "atimelogger", "database.db3"),
		Days:          9999,
		Exec:          true,
	}

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalRows != 0 {
		t.Fatalf("fixture: total_rows = %d, want 0", result.TotalRows)
	}
	if result.Status != statusWarn {
		t.Errorf("status = %q, want warning — no source could be read", result.Status)
	}
	if result.CandidatePath == "" {
		t.Error("an empty database was promoted to the live path")
	}
	if dbExists(cfg) {
		t.Error("lifetract.db exists — every query would now answer [] forever")
	}
}

// The candidate exists so the sound DB survives a bad run. A promotion that
// deletes the live file before the rename gives that back: if the rename fails,
// the database it was protecting is already gone.
func TestPromoteNeverUnlinksTheLiveDBFirst(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "lifetract.db")
	if err := os.WriteFile(live, []byte("the sound database"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A candidate that cannot be renamed: it does not exist.
	err := promoteDB(filepath.Join(dir, "lifetract.db.candidate"), live)
	if err == nil {
		t.Fatal("promote of a missing candidate succeeded")
	}

	b, readErr := os.ReadFile(live)
	if readErr != nil {
		t.Fatalf("the live DB is gone after a failed promotion: %v", readErr)
	}
	if string(b) != "the sound database" {
		t.Errorf("live DB = %q, want it untouched", b)
	}
}

// A file full of rows the tool cannot read is not an empty file.
//
// Rename one header in a Samsung export — the schema drifts, it has happened — and
// every row of that stream skips: 27,598 rows in the file, none landed. The old
// code reported (0 rows, no error), the table said "empty", and on a FIRST import
// there is no baseline to shrink against, so nothing warned. A partial database
// promoted itself and that stream answered [] from then on.
func TestRowsWeCannotReadAreNotAnEmptyStream(t *testing.T) {
	cfg, shealth := lossCfg(t)

	// The file stays syntactically perfect; only the required column is renamed.
	path := filepath.Join(shealth, stressCSV)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	drifted := strings.Replace(string(b), "start_time", "start_time_v2", 1)
	if drifted == string(b) {
		t.Fatal("fixture has no start_time header to rename")
	}
	if err := os.WriteFile(path, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}

	// First import: no baseline at all. This is the case that used to sail through.
	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	st := stream(t, result, "stress")
	if st.Invalid == 0 {
		t.Errorf("stress invalid = 0 — the rows the tool could not read were not counted")
	}
	if result.Status != statusWarn {
		t.Errorf("status = %q, want warning — every row of a stream was unreadable", result.Status)
	}
	if dbExists(cfg) {
		t.Error("a DB missing a whole stream was promoted — it would answer [] for stress forever")
	}
	if !strings.Contains(strings.Join(result.Warnings, "\n"), "could not be read") {
		t.Errorf("warnings = %v, want the unreadable rows named", result.Warnings)
	}
}
