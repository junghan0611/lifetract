package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The import guards its timestamps. The QUERIES did not.
//
// Every read of a stored time was `start, _ := parseShealthTime(...)`, so a
// timestamp the DB holds and we cannot parse became the zero time — year 1 — and
// what happened next depended only on which stream it was:
//
//   - sleep:     end - start <= 0, so `continue`. The night left the timeline.
//   - exercise:  the duration comes from another column, so the record SURVIVED,
//                dated 0001-01-01. A fabricated date, reported as fact.
//   - stages:    the minutes were computed against year 1 and folded into the night.
//
// None of it reached the caller as an error. A hole is not a zero, and a hole is
// not a date we made up either.

// importedDB runs a healthy import and hands back the live DB for tampering.
func importedDB(t *testing.T) (*Config, string) {
	t.Helper()
	cfg, _ := lossCfg(t)
	r, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != statusOK {
		t.Fatalf("fixture import was not sound: %v", r.Warnings)
	}
	return cfg, dbPath(cfg)
}

// corrupt writes a value straight into the live DB, standing in for a stored
// timestamp that no longer parses — a schema drift, a bad migration, a truncated
// write.
//
// The value has to stay inside the query's string range or the WHERE clause filters
// the row out before any of this code sees it. "2025-01-15 99:99:99.000" sorts
// exactly where a January row sorts and parses as nothing at all — which is also
// the realistic shape of the bug: a format that drifts, not a value that screams.
func corrupt(t *testing.T, path, stmt string) {
	t.Helper()
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatal(err)
	}
}

func TestUnparsableSleepTimeIsNotAQuietNight(t *testing.T) {
	cfg, path := importedDB(t)
	corrupt(t, path, `UPDATE sleep SET start_time = '2025-01-15 99:99:99.000'`)

	if _, err := dbQuerySleep(cfg, allTime()); err == nil {
		t.Error("sleep returned without the nights it could not read — a night that will not parse is not a night that did not happen")
	}
	if _, err := dbQueryTimeline(cfg, allTime()); err == nil {
		t.Error("timeline came back whole with sleep silently missing from it")
	}
}

// The one that does not skip. exercise takes its duration from duration_ms, so the
// row passes every guard and lands in the answer stamped 0001-01-01 — the tool
// asserting a date it never read.
func TestUnparsableExerciseTimeIsNeverAFabricatedDate(t *testing.T) {
	cfg, path := importedDB(t)
	corrupt(t, path, `UPDATE exercise SET start_time = '2025-01-15 99:99:99.000'`)

	recs, err := dbQueryExercise(cfg, allTime())
	if err == nil {
		for _, r := range recs {
			if strings.HasPrefix(r.Date, "0001-") {
				t.Fatalf("exercise reported date %q, id %q — a date the tool invented from a timestamp it could not read", r.Date, r.ID)
			}
		}
		t.Error("exercise answered for a timestamp it could not parse")
	}
}

func TestUnparsableStageTimeIsNotZeroMinutes(t *testing.T) {
	cfg, path := importedDB(t)
	corrupt(t, path, `UPDATE sleep_stage SET end_time = '2025-01-15 99:99:99.000'`)

	if _, err := dbQuerySleep(cfg, allTime()); err == nil {
		t.Error("the night came back with its stages computed against a time we could not read")
	}
	_ = cfg
}

// read <event-id> takes the same path and used to invent the same date.
func TestUnparsableEventTimeFailsInsteadOfInventingOne(t *testing.T) {
	cfg, path := importedDB(t)

	var id string
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT id FROM exercise LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	db.Close()

	corrupt(t, path, `UPDATE exercise SET start_time = '2025-01-15 99:99:99.000'`)

	tm, err := parseDenoteID("20250115T073000")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dbQueryEvent(cfg, tm, id); err == nil {
		t.Error("read returned an event whose timestamp it could not parse")
	}
}

// --- the newest export wins, in every stream ---

// newestCSV exists because glob order is lexical: the OLDEST export sorts first,
// and two generations in one folder mean the first match is stale data. Every
// stream goes through it — except exercise, which globs for itself and takes
// matches[0]. It reads the old generation, reports success, and the count looks
// like an ordinary smaller number. This is the stress-histogram bug with a
// different filename.
func TestExerciseReadsTheNewestExport(t *testing.T) {
	cfg, shealth := lossCfg(t)

	old, err := filepath.Glob(filepath.Join(shealth, "com.samsung.shealth.exercise.*.csv"))
	if err != nil || len(old) != 1 {
		t.Fatalf("exercise fixture: %v", old)
	}
	b, err := os.ReadFile(old[0])
	if err != nil {
		t.Fatal(err)
	}

	// A newer generation lands beside the old one, carrying one session the old
	// export never had.
	newer := filepath.Join(shealth, "com.samsung.shealth.exercise.20260714110176.csv")
	body := strings.TrimRight(string(b), "\n") +
		"\n,300.0,3600000,2026-07-13 06:00:00.000,1002,140.0,170.0,2026-07-13 07:00:00.000,UTC+0900,test-device,com.sec.android.app.shealth,ex-uuid-new\n"
	if err := os.WriteFile(newer, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}

	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM exercise WHERE id = 'ex-uuid-new'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Error("exercise imported the older export — the newest generation's session is missing, and the import called it a success")
	}
}

// --- the CSV fallback must not answer for a stream it cannot see ---

// `time` says it plainly: no DB means the question could not be asked, and it
// fails rather than answer []. But timeline, today and read assemble the SAME time
// axis from the same absent source — and simply left it out. One tool, two voices
// about one fact.
//
// aTimeLogger has no CSV source at all, so in CSV mode those hours are not zero;
// they are unreadable. The collector downstream consumes depth 0 and would write
// the silence into a public record as a day with no tracked time.
func TestCSVModeNeverZeroesTheTimeAxis(t *testing.T) {
	t.Setenv("LIFETRACT_NO_HA", "1")
	cfg, _ := lossCfg(t)

	if dbExists(cfg) {
		t.Fatal("this test needs the CSV path: no DB should exist yet")
	}

	if _, err := cmdTimeline(cfg); err == nil {
		t.Error("timeline answered without the time axis — those hours are a hole, not a zero")
	}
	if _, err := cmdToday(cfg); err == nil {
		t.Error("today answered without the time axis")
	}

	cfg.ReadID = "2025-01-15"
	if _, err := cmdRead(cfg); err == nil {
		t.Error("read answered without the time axis")
	}
}

// The health streams are still answerable from CSV on their own: asking for sleep
// is not asking for the time axis. Closing the hole above must not close these.
func TestCSVModeStillAnswersSingleHealthStreams(t *testing.T) {
	cfg, _ := lossCfg(t)

	if _, err := cmdSleep(cfg); err != nil {
		t.Errorf("sleep from CSV: %v", err)
	}
	if _, err := cmdHeart(cfg); err != nil {
		t.Errorf("heart from CSV: %v", err)
	}
	if _, err := cmdSteps(cfg); err != nil {
		t.Errorf("steps from CSV: %v", err)
	}
}

var _ = sql.ErrNoRows
