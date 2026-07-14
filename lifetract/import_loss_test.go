package main

import (
	"database/sql"
	"encoding/json"
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

// fakeATL writes a minimal aTimeLogger database. The fixtures used to point at a
// path that did not exist, which quietly made "every source present" untestable:
// an unreadable source is a warning now, on the first import as much as the tenth,
// so a fixture that is missing a source can no longer stand in for a healthy run.
func fakeATL(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE activity_type (id INTEGER PRIMARY KEY, name TEXT, color INTEGER, is_group INTEGER, parent_id INTEGER);
		CREATE TABLE time_interval2 (id INTEGER PRIMARY KEY, guid TEXT, start INTEGER, finish INTEGER,
			comment TEXT, activity_type_id INTEGER, is_deleted INTEGER);
		INSERT INTO activity_type VALUES (1, '본짓', 255, 0, 0), (2, '몸짓', 128, 0, 0);
		INSERT INTO time_interval2 VALUES
			(1, 'g1', 1767225600, 1767229200, '', 1, 0),
			(2, 'g2', 1767232800, 1767236400, '', 2, 0),
			(3, 'g3', 1767240000, 1767243600, '', 1, 0);
	`); err != nil {
		t.Fatal(err)
	}
	return path
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
		ATimeLoggerDB: fakeATL(t, filepath.Join(dir, "atimelogger", "database.db3")),
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

// The stream stays lost until it comes back — and the baseline it is measured
// against does not rot in the meantime. Two things hold that line: a losing run is
// never promoted, so the live ledger keeps the last sound counts; and the ledger
// remembers the last NON-zero count regardless, so even a promoted zero cannot
// become the new normal. A warning that fires once and then goes quiet is just the
// silence again, one import later.
func TestLossKeepsWarningOnTheNextImport(t *testing.T) {
	cfg, shealth := lossCfg(t)

	first, _ := execImport(cfg)
	before := stream(t, first, "stress").Rows

	truncateToHeader(t, filepath.Join(shealth, stressCSV))
	execImport(cfg) // the import that loses it — warns, and is held back

	third, err := execImport(cfg) // still empty, still wrong
	if err != nil {
		t.Fatal(err)
	}
	if third.Status != statusWarn {
		t.Errorf("status = %q, want %q — the stream is still lost", third.Status, statusWarn)
	}
	st := stream(t, third, "stress")
	if st.PrevRows == nil || *st.PrevRows != before {
		t.Errorf("prev_rows = %v, want %d — the rejected run poisoned the baseline", st.PrevRows, before)
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
// crying wolf. Every source is present and readable here — "no baseline" excuses
// no claim about loss, and excuses nothing else.
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

// A clean run still has to show the warnings key. If it vanishes when empty, a
// caller cannot tell "checked, nothing lost" from a binary too old to check —
// and that is the same disease as a check that reports itself passing.
func TestCleanImportStillCarriesWarnings(t *testing.T) {
	cfg, _ := lossCfg(t)

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	b, _ := json.Marshal(result)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}

	raw, ok := m["warnings"]
	if !ok {
		t.Fatalf("a clean import dropped the warnings key: %s", b)
	}
	if string(raw) == "null" {
		t.Errorf("warnings = null, want [] — absence must not pass for a clean run")
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

	// And every stream is on the record, zero or not. A zero that goes unrecorded
	// is a loss that goes unnoticed next time.
	// 10, not 9: atl_category is a stream of its own now. It used to be imported
	// without being counted, so a category table that emptied out took the whole
	// time axis with it (dbQueryTime joins through it) while the ledger watched the
	// interval count sit there, unchanged and reassuring.
	var streams int
	db.QueryRow(`SELECT COUNT(DISTINCT table_name) FROM import_log`).Scan(&streams)
	if streams != 10 {
		t.Errorf("ledger covers %d streams, want 10 — a stream left no trace", streams)
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

// Saying "I lost a stream" while handing over the DB that lost it is not much of a
// warning. The live database — the one every later query reads — must still be the
// one that has the data. The old code deleted it before building, so by the time
// the warning printed there was nothing left to protect.
func TestALosingRunIsNotPromoted(t *testing.T) {
	cfg, shealth := lossCfg(t)

	first, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != statusOK {
		t.Fatalf("first import: status = %q (%v)", first.Status, first.Warnings)
	}
	good := stream(t, first, "stress").Rows

	truncateToHeader(t, filepath.Join(shealth, stressCSV))

	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusWarn {
		t.Fatalf("status = %q, want %q", second.Status, statusWarn)
	}
	if second.CandidatePath == "" {
		t.Error("candidate_path is empty — the rejected DB was not kept for inspection")
	}

	// The live DB still holds the stream the run lost.
	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var live int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stress`).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != good {
		t.Errorf("live stress rows = %d, want %d — the broken import was promoted over the good DB", live, good)
	}

	// And the candidate, which is the broken one, is on disk to be looked at.
	cdb, err := openDB(second.CandidatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer cdb.Close()

	var rejected int
	cdb.QueryRow(`SELECT COUNT(*) FROM stress`).Scan(&rejected)
	if rejected != 0 {
		t.Errorf("candidate stress rows = %d, want 0 — wrong file was kept", rejected)
	}
}

// "Nothing to compare against" excuses no claim about loss. It never excused
// calling a source we could not read fine — and on a first import, that was
// exactly what happened: no baseline, so no warning, so ok, so a missing export
// shipped as a healthy database.
func TestUnreadableSourceIsNotOKEvenOnTheFirstImport(t *testing.T) {
	cfg, shealth := lossCfg(t)

	if err := os.Remove(filepath.Join(shealth, stressCSV)); err != nil {
		t.Fatal(err)
	}

	first, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != statusWarn {
		t.Errorf("status = %q, want %q — an unread source is not a healthy import", first.Status, statusWarn)
	}
	joined := strings.Join(first.Warnings, "\n")
	if !strings.Contains(joined, "stress") || !strings.Contains(joined, "unreadable") {
		t.Errorf("warnings = %q, want stress named unreadable", first.Warnings)
	}

	// And it does not land. This test used to assert the opposite — "some data beats
	// none" — and that was the hole: eight streams import, one cannot be read, and
	// the partial DB installs itself. From then on the missing stream answers [] and
	// the tool has no error path left to tell the truth with, because now there IS a
	// database. A bootstrap that quietly drops a stream is where "I could not look"
	// becomes "there is nothing", permanently.
	if dbExists(cfg) {
		t.Error("a DB with a stream missing was promoted — from now on that stream is just 'empty'")
	}
	if first.CandidatePath == "" {
		t.Error("no candidate kept — there is nothing left to inspect")
	}
}

// A ledger that cannot be written is a baseline the next run will not have. The
// run says so and is held back: promoting a DB whose record failed to write is how
// a future import comes to believe it is the first one.
func TestLedgerWriteFailureBlocksPromotion(t *testing.T) {
	cfg, _ := lossCfg(t)

	first, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	good := stream(t, first, "stress").Rows

	// Make the ledger unwritable in the candidate: the schema is created fresh each
	// run, so a trigger is the honest way to make INSERTs fail the way a real
	// failure would (disk full, permissions, corruption).
	orig := initSchema
	t.Cleanup(func() { initSchema = orig })
	initSchema = func(db *sql.DB) error {
		if err := orig(db); err != nil {
			return err
		}
		_, err := db.Exec(`CREATE TRIGGER no_ledger BEFORE INSERT ON import_log
			BEGIN SELECT RAISE(ABORT, 'ledger is unwritable'); END`)
		return err
	}

	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusWarn {
		t.Errorf("status = %q, want %q — the ledger write failed silently", second.Status, statusWarn)
	}
	if !strings.Contains(strings.Join(second.Warnings, "\n"), "ledger") {
		t.Errorf("warnings = %q, want the ledger failure named", second.Warnings)
	}

	// The live DB is untouched, so the baseline survives.
	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var live int
	db.QueryRow(`SELECT COUNT(*) FROM stress`).Scan(&live)
	if live != good {
		t.Errorf("live stress rows = %d, want %d", live, good)
	}
	var gens int
	db.QueryRow(`SELECT COUNT(*) FROM import_log WHERE table_name = 'stress'`).Scan(&gens)
	if gens != 1 {
		t.Errorf("ledger generations = %d, want 1 — a run with no record was promoted", gens)
	}
}
