package main

import (
	"testing"
	"time"
)

// A day is the day the export says the steps are for, and nothing else.
//
// day_time is that day. Samsung writes it as epoch millis in older exports and as a
// wall-clock string in current ones, and the importer read only the millis: every
// string failed ParseInt, and the code then took create_time — when the row was
// WRITTEN — as the day. The rows still landed, the count still matched, the loss
// guard still said ok. What it built was nine years of steps stamped with the wrong
// day: 3,019 days crushed onto the export's dump date, and every live day shifted
// back by one, because create_time runs a day ahead of the day it describes.
//
// The fixture is why no test saw it. Its day_time was always epoch millis — the one
// shape the code could read — so the tests only ever asked the question the code
// already answered. These rows carry the shape the real export ships.
//
// stepRow builds a step_daily_trend row. Column order:
// binning,update_time,create_time,pkg,source_type,count,speed,distance,calorie,device,pkg,datauuid,day_time
func stepRow(update, create, count, uuid, dayTime string) string {
	return "," + update + "," + create + ",com.sec.android.app.shealth,-2," + count +
		",3.5,6500.0,350.0,test-device,com.sec.android.app.shealth," + uuid + "," + dayTime + ","
}

// stepsOn returns the count the live DB holds for a date, and whether it holds one.
func stepsOn(t *testing.T, cfg *Config, date string) (int, bool) {
	t.Helper()
	records, err := dbQuerySteps(cfg, daysWindow(9999))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if r.Date == date {
			return r.Steps, true
		}
	}
	return 0, false
}

// The real export's shape: day_time is a string, and create_time is the evening
// BEFORE it. Reading create_time as the day is what shifted every recent day back
// by one — the tool reported 3,187 steps for the 13th when 3,187 was the 14th's
// morning and the 13th's real total was 17,715.
func TestStringDayTimeIsTheDayNotCreateTime(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow("2025-02-11 06:00:00.000", "2025-02-10 19:13:02.228", "17715", "step-str", "2025-02-11 00:00:00.000"))

	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}

	if got, ok := stepsOn(t, cfg, "2025-02-11"); !ok || got != 17715 {
		t.Errorf("2025-02-11 = %d (present=%v), want 17715 — the day day_time names", got, ok)
	}
	if got, ok := stepsOn(t, cfg, "2025-02-10"); ok {
		t.Errorf("2025-02-10 = %d — create_time's day, which measured nothing", got)
	}
}

// The older export shape still reads. Both forms are one day_time, and the fixture's
// own rows are epoch millis, so this guards the reader against a fix that trades one
// shape for the other.
func TestEpochDayTimeStillReads(t *testing.T) {
	cfg, _ := lossCfg(t)
	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}
	// 1736899200000 → 2025-01-15 09:00 KST, the fixture's first row.
	if got, ok := stepsOn(t, cfg, "2025-01-15"); !ok || got != 8432 {
		t.Errorf("2025-01-15 = %d (present=%v), want 8432", got, ok)
	}
}

// A day_time that will not parse is a day we do not have. It is not create_time's
// day, and it is not any other day: substituting one fact for another is how a
// misdated row passes for a measured one. Count it and refuse to promote.
func TestUnreadableDayTimeIsNotCreateTimesDay(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow("2025-03-02 06:00:00.000", "2025-03-01 19:00:00.000", "9000", "step-bad", "not-a-day"))

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertInvalidBlocksPromotion(t, result, "steps_daily")
}

// A future aggregate is not today's measurement. Accepting it would poison status
// and every relative window until the calendar caught up, so it is an invalid source
// row and the candidate must not be promoted.
func TestFutureDayTimeBlocksPromotion(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow("2099-01-01 06:00:00.000", "2098-12-31 19:00:00.000", "9000", "step-future", "2099-01-01 00:00:00.000"))

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertInvalidBlocksPromotion(t, result, "steps_daily")
}

// If two conflicting revisions claim the exact same update_time, neither is
// demonstrably newer. File order is not evidence, so the ambiguity is invalid
// rather than an arbitrary winner.
func TestEqualUpdateTimeCannotChooseConflictingRevision(t *testing.T) {
	cfg, shealth := lossCfg(t)
	day := "2025-04-04 00:00:00.000"
	update := "2025-04-04 12:00:00.000"
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow(update, "2025-04-03 18:00:00.000", "1000", "step-tie-a", day))
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow(update, "2025-04-03 18:01:00.000", "2000", "step-tie-b", day))

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertInvalidBlocksPromotion(t, result, "steps_daily")
}

// The phone re-syncs a day and the export carries the merged record twice. Both rows
// are the same day, so the day is one number — the DB used to SUM them and hand back
// double (7,685 read out as 15,370).
func TestResyncedDayIsNotDoubled(t *testing.T) {
	cfg, shealth := lossCfg(t)
	day := "2025-04-05 00:00:00.000"
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow("2025-04-05 18:06:06.665", "2025-04-04 18:06:06.665", "7685", "step-dup-a", day))
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow("2025-04-05 18:16:50.911", "2025-04-04 18:16:50.911", "7685", "step-dup-b", day))

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if got, ok := stepsOn(t, cfg, "2025-04-05"); !ok || got != 7685 {
		t.Errorf("2025-04-05 = %d (present=%v), want 7685 — one day, not two rows summed", got, ok)
	}
	if s := stream(t, result, "steps_daily"); s.Rejected != 1 {
		t.Errorf("rejected = %d, want 1 — the superseded revision was dropped without a word", s.Rejected)
	}
}

// When the two revisions disagree, the later one is the day. This is 2025-07-20 in
// the real export: 463 steps written at 05:50 and 909 written at 12:26 — a half-day
// snapshot and the day that superseded it.
//
// The tiebreak must be update_time. create_time runs BACKWARDS against it on that
// very day (the 909 row was created earlier and updated later), so picking by
// create_time enshrines the stale 463 as the day's total.
func TestRevisedDayTakesNewestUpdateTime(t *testing.T) {
	cfg, shealth := lossCfg(t)
	day := "2025-05-09 00:00:00.000"
	// stale: updated at 05:50, but created LAST
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow("2025-05-09 05:50:05.879", "2025-05-08 18:44:09.921", "463", "step-rev-stale", day))
	// current: updated at 12:26, but created FIRST
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow("2025-05-09 12:26:43.143", "2025-05-08 17:41:03.852", "909", "step-rev-current", day))

	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}

	got, ok := stepsOn(t, cfg, "2025-05-09")
	if !ok {
		t.Fatal("2025-05-09 missing")
	}
	if got == 463 {
		t.Fatal("2025-05-09 = 463 — the 05:50 snapshot won; the tiebreak read create_time")
	}
	if got != 909 {
		t.Fatalf("2025-05-09 = %d, want 909 (the newest update_time)", got)
	}
}

// The database carries the same one-day/one-row invariant as the importer. A future
// refactor that puts UUIDs back into id must still fail loudly instead of storing two
// revisions under one date.
func TestStepsDateIsUniqueInSchema(t *testing.T) {
	cfg, _ := lossCfg(t)
	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}

	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO steps_daily (id, date, count) VALUES ('different-id', '2025-01-15', 1)`); err == nil {
		t.Fatal("duplicate steps date landed — one day/one-row is not enforced by the schema")
	}
}

// The two surfaces answer the same day with the same number. They did not: the DB
// summed a re-synced day while the CSV path let whichever row came last in the file
// win, and a day_time the CSV path could not parse was dropped from the window
// entirely — steps came back [] no matter how wide the window opened.
func TestDBAndCSVAgreeOnTheSameDays(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		stepRow("2025-06-02 06:00:00.000", "2025-06-01 19:13:02.228", "12345", "step-agree", "2025-06-02 00:00:00.000"))

	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}

	w := Window{From: time.Date(2025, 1, 1, 0, 0, 0, 0, KST), To: time.Date(2026, 1, 1, 0, 0, 0, 0, KST)}
	fromDB, err := dbQuerySteps(cfg, w)
	if err != nil {
		t.Fatal(err)
	}
	fromCSV, err := parseStepRecords(cfg, w)
	if err != nil {
		t.Fatal(err)
	}

	if len(fromCSV) == 0 {
		t.Fatal("CSV path returned no days — the fallback dropped every row and called it an empty window")
	}
	if len(fromDB) != len(fromCSV) {
		t.Fatalf("DB has %d days, CSV has %d — the two surfaces disagree on what the window holds", len(fromDB), len(fromCSV))
	}
	for i := range fromDB {
		if fromDB[i].Date != fromCSV[i].Date || fromDB[i].Steps != fromCSV[i].Steps {
			t.Errorf("day %d: DB says %s=%d, CSV says %s=%d",
				i, fromDB[i].Date, fromDB[i].Steps, fromCSV[i].Date, fromCSV[i].Steps)
		}
	}
}
