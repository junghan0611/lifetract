package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A stream that goes to zero must say so. On 2026-07-14 stress went 27,598 → 0
// and import said "ok"; the tests were green and only the total row count, seen
// by eye, gave it away. These tests are that human's second look, written down.

const stressCSV = "com.samsung.shealth.stress.20251006104703.csv"

func copyShealth(t *testing.T, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("testdata/samsunghealth/*.csv")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, filepath.Base(f)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func lossCfg(t *testing.T) (*Config, string) {
	t.Helper()
	dir := t.TempDir()
	shealth := filepath.Join(dir, "samsunghealth_gtgkjh")
	copyShealth(t, shealth)
	return &Config{
		DataDir:       dir,
		ShealthDir:    shealth,
		ShealthDirs:   []string{shealth},
		ATimeLoggerDB: "testdata/nonexistent.db3", // absent on purpose: never had rows, so never warns
		Days:          9999,
		Exec:          true,
	}, shealth
}

func stream(t *testing.T, r *ImportResult, name string) TableResult {
	t.Helper()
	for _, tr := range r.Tables {
		if tr.Name == name {
			return tr
		}
	}
	t.Fatalf("stream %q not in result", name)
	return TableResult{}
}

// truncateToHeader leaves the file readable and empty — the silent shape. The CSV
// parses, no error is raised, and zero rows come back. This is exactly what "ok"
// used to cover up.
func truncateToHeader(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitN(string(b), "\n", 3)
	if len(lines) < 2 {
		t.Fatalf("fixture too short: %s", path)
	}
	head := lines[0] + "\n" + lines[1] + "\n"
	if err := os.WriteFile(path, []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSilentZeroIsNotOK(t *testing.T) {
	cfg, shealth := lossCfg(t)

	first, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != statusOK {
		t.Fatalf("first import: status = %q, want ok (warnings: %v)", first.Status, first.Warnings)
	}
	before := stream(t, first, "stress").Rows
	if before == 0 {
		t.Fatal("fixture has no stress rows — the test would prove nothing")
	}

	truncateToHeader(t, filepath.Join(shealth, stressCSV))

	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if second.Status != statusWarn {
		t.Errorf("status = %q, want %q — a lost stream is not ok", second.Status, statusWarn)
	}
	st := stream(t, second, "stress")
	if st.Rows != 0 {
		t.Fatalf("stress rows = %d, want 0 (the fixture was emptied)", st.Rows)
	}
	if st.Status != statusEmpty {
		t.Errorf("stress status = %q, want %q", st.Status, statusEmpty)
	}
	if st.PrevRows == nil || *st.PrevRows != before {
		t.Errorf("stress prev_rows = %v, want %d — the ledger forgot what it was worth", st.PrevRows, before)
	}
	if st.Delta == nil || *st.Delta != -before {
		t.Errorf("stress delta = %v, want %d", st.Delta, -before)
	}

	// The warning has to name the stream and the count that vanished, because the
	// number is what a human notices.
	joined := strings.Join(second.Warnings, "\n")
	if !strings.Contains(joined, "stress") || !strings.Contains(joined, comma(before)) {
		t.Errorf("warnings = %q, want stress and %s named", second.Warnings, comma(before))
	}

	// The total fell, and the result says by how much without anyone doing the
	// subtraction in their head.
	if second.PrevTotalRows <= second.TotalRows {
		t.Errorf("prev_total_rows = %d, total_rows = %d — the drop is invisible",
			second.PrevTotalRows, second.TotalRows)
	}
}

// A missing CSV is the loud shape: the importer errors, rows are zero, and the
// old code still called the run a success because nothing checked the stream
// against what it used to be.
func TestMissingCSVWarnsAgainstTheLedger(t *testing.T) {
	cfg, shealth := lossCfg(t)

	first, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := stream(t, first, "stress").Rows

	if err := os.Remove(filepath.Join(shealth, stressCSV)); err != nil {
		t.Fatal(err)
	}

	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusWarn {
		t.Errorf("status = %q, want %q", second.Status, statusWarn)
	}
	st := stream(t, second, "stress")
	if st.Status == statusOK || st.Status == "" {
		t.Errorf("stress status = %q, want the importer's complaint", st.Status)
	}
	joined := strings.Join(second.Warnings, "\n")
	if !strings.Contains(joined, comma(before)) {
		t.Errorf("warnings = %q, want the lost count %s", second.Warnings, comma(before))
	}
}

// The stream stays lost until it comes back. A warning that fires once and then
// goes quiet is the silence again, one import later: the ledger keeps the last
// count the stream was actually worth, not merely the last count it reported.
func TestLossKeepsWarningOnTheNextImport(t *testing.T) {
	cfg, shealth := lossCfg(t)

	first, _ := execImport(cfg)
	before := stream(t, first, "stress").Rows

	truncateToHeader(t, filepath.Join(shealth, stressCSV))
	execImport(cfg) // the import that loses it — warns

	third, err := execImport(cfg) // still empty, still wrong
	if err != nil {
		t.Fatal(err)
	}
	if third.Status != statusWarn {
		t.Errorf("status = %q, want %q — the stream is still lost", third.Status, statusWarn)
	}
	st := stream(t, third, "stress")
	if st.PrevRows == nil || *st.PrevRows != 0 {
		t.Errorf("prev_rows = %v, want 0 (last import was already empty)", st.PrevRows)
	}
	joined := strings.Join(third.Warnings, "\n")
	if !strings.Contains(joined, comma(before)) {
		t.Errorf("warnings = %q, want the last good count %s", third.Warnings, comma(before))
	}
}

// A partial drop is an event, and it is reported as one. Samsung exports are
// cumulative dumps, so fewer rows than last time means something was dropped on
// the way in.
func TestShrinkIsReported(t *testing.T) {
	cfg, shealth := lossCfg(t)

	first, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := stream(t, first, "stress").Rows
	if before < 2 {
		t.Skip("fixture too small to shrink")
	}

	// Drop the last data row.
	path := filepath.Join(shealth, stressCSV)
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	os.WriteFile(path, []byte(strings.Join(lines[:len(lines)-1], "\n")+"\n"), 0o644)

	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	st := stream(t, second, "stress")
	if st.Rows != before-1 {
		t.Fatalf("stress rows = %d, want %d", st.Rows, before-1)
	}
	if st.Status != statusShrunk {
		t.Errorf("stress status = %q, want %q", st.Status, statusShrunk)
	}
	if second.Status != statusWarn {
		t.Errorf("status = %q, want %q", second.Status, statusWarn)
	}
}

// The first import has nothing to compare against, and says that instead of
// crying wolf. aTimeLogger is absent in these fixtures and never had rows — an
// empty stream with no history is not a loss.
func TestFirstImportIsNotAWarning(t *testing.T) {
	cfg, _ := lossCfg(t)

	first, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != statusOK {
		t.Errorf("status = %q, want ok — nothing was lost, there was nothing to lose", first.Status)
	}
	if len(first.Warnings) != 0 {
		t.Errorf("warnings = %v, want none on a first import", first.Warnings)
	}
	if first.Note == "" {
		t.Error("note is empty — a first import should say it has no baseline")
	}
	if first.BaselineAt != "" {
		t.Errorf("baseline_at = %q, want empty", first.BaselineAt)
	}
}

// The ledger has to survive the import that replaces the DB. It lives inside the
// file execImport deletes, so without carry-forward every import is the first
// import — and a first import can never notice a loss.
func TestLedgerSurvivesTheImportThatDeletesTheDB(t *testing.T) {
	cfg, _ := lossCfg(t)

	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}
	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(second.BaselineAt, "20") {
		t.Errorf("baseline_at = %q, want the previous import's timestamp", second.BaselineAt)
	}
	if second.PrevTotalRows != second.TotalRows {
		t.Errorf("prev_total_rows = %d, total_rows = %d — an unchanged re-import must match",
			second.PrevTotalRows, second.TotalRows)
	}

	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Two generations of stress, not one: the second import kept the first's row.
	var generations int
	db.QueryRow(`SELECT COUNT(*) FROM import_log WHERE table_name = 'stress'`).Scan(&generations)
	if generations < 2 {
		t.Errorf("stress ledger rows = %d, want >= 2 — the history died with the DB", generations)
	}

	// And a stream that imported nothing is on the record too. The zero is the
	// fact we need next time.
	var zeros int
	db.QueryRow(`SELECT COUNT(*) FROM import_log WHERE table_name = 'atl_interval' AND rows_imported = 0`).Scan(&zeros)
	if zeros == 0 {
		t.Error("no zero-row entry logged — an empty stream left no trace")
	}
}

// One import is one run. imported_at used to be time.Now() per row, so a run that
// crossed a second boundary landed under two or three stamps — two imports left
// five of them on 2026-07-14. Anything reconstructing "the previous import" by
// grouping on the stamp would have compared against half a run, and compared
// quietly. import_id is the group; the stamp only describes it.
func TestOneImportIsOneRun(t *testing.T) {
	// A clock that ticks on every read. The real defect was a stamp taken per row,
	// and a fixture this small imports inside a single second — so a still clock
	// would let the bug pass. This one moves, the way the wall clock moved through
	// the middle of the real import.
	orig := nowStamp
	t.Cleanup(func() { nowStamp = orig })
	tick := 0
	nowStamp = func() string {
		tick++
		return time.Date(2026, 7, 14, 12, 25, 49+tick, 0, KST).Format(time.RFC3339)
	}

	cfg, _ := lossCfg(t)

	first, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first.ImportID != 1 || second.ImportID != 2 {
		t.Errorf("import_id = %d then %d, want 1 then 2", first.ImportID, second.ImportID)
	}

	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT import_id, COUNT(DISTINCT imported_at), COUNT(*), COUNT(DISTINCT table_name)
		FROM import_log GROUP BY import_id ORDER BY import_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	runs := 0
	for rows.Next() {
		var id, stamps, n, streams int
		rows.Scan(&id, &stamps, &n, &streams)
		runs++
		if stamps != 1 {
			t.Errorf("run %d: %d distinct imported_at, want 1 — the run split across the clock", id, stamps)
		}
		if n != streams {
			t.Errorf("run %d: %d rows over %d streams — a stream was logged twice", id, n, streams)
		}
	}
	if runs != 2 {
		t.Errorf("runs = %d, want 2", runs)
	}
}

// A DB written before import_log had import_id must still yield a baseline. If the
// read fell through to "no baseline", the next import would call itself the first
// one and notice nothing — the loss would ship in silence, which is the one
// outcome this file exists to prevent. Runs are recovered from structure (a stream
// repeats ⇒ a new run began), not from stamps that cannot be trusted.
func TestOldLedgerWithoutImportIDStillGivesABaseline(t *testing.T) {
	cfg, shealth := lossCfg(t)

	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}

	// Rewrite the ledger the way the old build left it: no import_id column, and a
	// single run smeared across two seconds.
	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE old_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			imported_at TEXT NOT NULL,
			source TEXT NOT NULL,
			table_name TEXT NOT NULL,
			rows_imported INTEGER,
			source_path TEXT
		);
		INSERT INTO old_log (imported_at, source, table_name, rows_imported, source_path)
			SELECT imported_at, source, table_name, rows_imported, source_path FROM import_log ORDER BY id;
		DROP TABLE import_log;
		ALTER TABLE old_log RENAME TO import_log;
		UPDATE import_log SET imported_at = '2026-07-14T11:55:52+09:00' WHERE table_name = 'sleep';
		UPDATE import_log SET imported_at = '2026-07-14T11:55:53+09:00' WHERE table_name = 'stress';
	`); err != nil {
		t.Fatal(err)
	}
	var before int
	db.QueryRow(`SELECT rows_imported FROM import_log WHERE table_name = 'stress'`).Scan(&before)
	db.Close()

	if before == 0 {
		t.Fatal("fixture has no stress rows")
	}

	truncateToHeader(t, filepath.Join(shealth, stressCSV))

	next, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != statusWarn {
		t.Fatalf("status = %q, want %q — the old ledger was dropped on the floor", next.Status, statusWarn)
	}
	if !strings.Contains(strings.Join(next.Warnings, "\n"), comma(before)) {
		t.Errorf("warnings = %q, want the lost count %s", next.Warnings, comma(before))
	}
	if next.ImportID < 2 {
		t.Errorf("import_id = %d, want >= 2 — the old run was not counted", next.ImportID)
	}
}

// A ledger that cannot be read is not a first import. Passing it off as one would
// disarm the loss check for that run and say "ok" while doing no checking at all —
// the same silence, now wearing the safety net's clothes.
func TestUnreadableLedgerIsNotAFreshStart(t *testing.T) {
	cfg, _ := lossCfg(t)

	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}

	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE import_log`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	next, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != statusWarn {
		t.Errorf("status = %q, want %q — the check silently stopped checking", next.Status, statusWarn)
	}
	if next.Note != "" {
		t.Errorf("note = %q, want empty — this is not a first import", next.Note)
	}
	if !strings.Contains(strings.Join(next.Warnings, "\n"), "ledger") {
		t.Errorf("warnings = %q, want the unreadable ledger named", next.Warnings)
	}
}
