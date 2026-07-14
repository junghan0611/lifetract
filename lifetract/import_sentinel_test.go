package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Samsung export ships rows it never measured: 1970-01-01 (epoch zero) and
// 2000-01-01, standing in for "unknown". Imported as if real, they made
// `heart --to 2001-01-01` report a heart rate for a day decades before the watch
// existed — true in the database, false in the world.
//
// They are refused now. The whole weight of these tests is that refusing is not
// allowed to be silent, and is not allowed to look like a loss.

// appendHeartRow adds one raw heart-rate row with the given start_time to the
// fixture, keeping the column layout the importer reads.
func appendHeartRow(t *testing.T, shealth, startTime, uuid, hr string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(shealth, "com.samsung.shealth.tracker.heart_rate.*.csv"))
	if len(matches) != 1 {
		t.Fatalf("heart_rate fixture: %v", matches)
	}
	path := matches[0]

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimRight(string(b), "\n")
	row := ",21312,,1," + startTime + ",,,," + startTime + "," + startTime +
		",,0.0,0.0,,UTC+0900,test-device,,com.sec.android.app.shealth," +
		startTime + "," + uuid + "," + hr + ",\n"
	if err := os.WriteFile(path, []byte(body+"\n"+row), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSentinelRowsAreRejectedAndReported(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendHeartRow(t, shealth, "1970-01-01 00:00:00.000", "hr-epoch", "79.0")
	appendHeartRow(t, shealth, "2000-01-01 00:00:00.000", "hr-y2k", "93.0")

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	hr := stream(t, result, "heart_rate")
	if hr.Rejected != 2 {
		t.Errorf("heart_rate rejected = %d, want 2", hr.Rejected)
	}

	// Said out loud, in the payload, not only in a counter nobody reads.
	joined := strings.Join(result.Rejected, "\n")
	if !strings.Contains(joined, "heart_rate") || !strings.Contains(joined, "2 rows rejected") {
		t.Errorf("rejected = %q, want heart_rate and the count named", result.Rejected)
	}

	// And they must not be in the DB, or the query would still find 1970.
	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM heart_rate WHERE DATE(start_time) < '2010-01-01'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d placeholder rows reached the DB — the refusal did not happen", n)
	}
}

// The trap this filter sets for itself: rejecting rows makes the stream SHRINK
// against the ledger, and a shrink is what the loss guard refuses to promote.
// Left alone, the first import after the filter lands would jam permanently —
// the count can never climb back, so every future run would be "shrunk" too.
//
// A shrink the run can account for is not a loss.
func TestExplainedShrinkStillPromotes(t *testing.T) {
	cfg, shealth := lossCfg(t)

	// Baseline: the sentinel row is in the ledger, because the old binary took it.
	// This is exactly the state the live DB is in right now.
	appendHeartRow(t, shealth, "1970-01-01 00:00:00.000", "hr-epoch", "79.0")

	var first *ImportResult
	var err error
	withOldBinary(t, func() { first, err = execImport(cfg) })
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != statusOK {
		t.Fatalf("first import: %q (%v)", first.Status, first.Warnings)
	}
	legacyLedger(t, cfg)
	before := stream(t, first, "heart_rate").Rows

	// Now the filter exists. The stream loses exactly the row it refused.
	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	hr := stream(t, second, "heart_rate")
	if hr.Rows != before-1 || hr.Rejected != 1 {
		t.Fatalf("heart_rate = %d rows (prev %d), rejected %d — want one fewer, one rejected",
			hr.Rows, before, hr.Rejected)
	}
	if second.Status != statusOK {
		t.Errorf("status = %q, want ok — the drop is exactly what the run refused: %v",
			second.Status, second.Warnings)
	}
	if second.CandidatePath != "" {
		t.Errorf("candidate_path = %q — an accounted-for refusal must still promote", second.CandidatePath)
	}
	if len(second.Rejected) == 0 {
		t.Error("promoted quietly — the refusal has to be reported even when it costs nothing")
	}
}

// The rejects must not become a blanket. A drop bigger than what the run refused
// is still a loss, and still holds the DB back.
func TestShrinkBeyondTheRejectsIsStillALoss(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendHeartRow(t, shealth, "1970-01-01 00:00:00.000", "hr-epoch", "79.0")

	if _, err := execImport(cfg); err != nil { // baseline with all the real rows
		t.Fatal(err)
	}

	// The export comes back with the placeholder AND missing real rows.
	matches, _ := filepath.Glob(filepath.Join(shealth, "com.samsung.shealth.tracker.heart_rate.*.csv"))
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	// header(2) + one real row + the placeholder we appended last
	kept := append(lines[:3], lines[len(lines)-1])
	if err := os.WriteFile(matches[0], []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if second.Status != statusWarn {
		t.Errorf("status = %q, want warning — rows vanished beyond the ones we refused", second.Status)
	}
	if second.CandidatePath == "" {
		t.Error("a real loss was promoted — the rejects became a blanket for it")
	}
	joined := strings.Join(second.Warnings, "\n")
	if !strings.Contains(joined, "unaccounted") {
		t.Errorf("warnings = %q, want the unexplained part named", second.Warnings)
	}
}

// status/import payloads: rejected is always present, like warnings. A run that
// silently discards rows is the same silence as one that silently loses them.
func TestRejectedKeyIsAlwaysPresent(t *testing.T) {
	cfg, _ := lossCfg(t)

	result, err := execImport(cfg) // clean fixtures: nothing to reject
	if err != nil {
		t.Fatal(err)
	}

	b, _ := json.Marshal(result)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	raw, ok := m["rejected"]
	if !ok {
		t.Fatalf("no rejected key: %s", b)
	}
	if string(raw) == "null" {
		t.Error("rejected = null, want []")
	}
}

// withOldBinary runs fn as the binary that had no sentinel filter — the one that
// built the DB now on disk. The floor goes back up no matter how fn ends.
func withOldBinary(t *testing.T, fn func()) {
	t.Helper()
	orig := sentinelFloor
	sentinelFloor = time.Date(1900, 1, 1, 0, 0, 0, 0, KST)
	defer func() { sentinelFloor = orig }()
	fn()
}

// legacyLedger rewrites import_log without the rows_rejected column — the shape
// the live DB's ledger actually has, since it was written by a build that had no
// reject policy at all.
//
// Lowering the floor alone is not enough to imitate that build: this code would
// still write rows_rejected = 0, and 0 is a *claim* ("refused nothing") where the
// old build made none. The migration hinges on that difference, so the fixture has
// to reproduce the absence, not a zero standing in for it.
func legacyLedger(t *testing.T, cfg *Config) {
	t.Helper()
	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE il_legacy AS
			SELECT id, import_id, imported_at, source, table_name, rows_imported, source_path
			FROM import_log;
		DROP TABLE import_log;
		ALTER TABLE il_legacy RENAME TO import_log;
	`); err != nil {
		t.Fatal(err)
	}
}
