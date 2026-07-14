package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The time axis is a contract, not an implementation detail. Three properties
// hold it up, and each one broke in production before it was tested here:
//
//  1. The answer does not depend on the caller's $TZ.
//  2. Widening the window does not change what a past day contains.
//  3. Interval comments never leave the process.
//
// These tests exist so the next person cannot quietly reintroduce any of them.

// atlFixture builds a DB with aTimeLogger blocks at known KST wall times.
// The sleep block deliberately crosses midnight (21:14 → 05:48 KST) — that is
// the shape 12% of real blocks have, and the shape every TZ bug reveals itself on.
func atlFixture(t *testing.T) *Config {
	t.Helper()
	cfg := &Config{DataDir: t.TempDir(), Days: 7}

	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := initSchema(db); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(
		`INSERT INTO atl_category (id, name) VALUES (1, '수면'), (2, '독서')`); err != nil {
		t.Fatal(err)
	}

	kst := func(s string) int64 {
		tm, err := time.ParseInLocation("2006-01-02 15:04", s, KST)
		if err != nil {
			t.Fatal(err)
		}
		return tm.Unix()
	}

	blocks := []struct {
		id         int
		start, end string
		category   int
		comment    string
	}{
		// Crosses midnight: belongs entirely to 07-11, the day it starts on.
		{1, "2026-07-11 21:14", "2026-07-12 05:48", 1, "김가족 이름 평문"},
		// Early morning on 07-12: a UTC-based day boundary would drag this to 07-11.
		{2, "2026-07-12 07:00", "2026-07-12 08:00", 2, ""},
		// Late evening on 07-12: a UTC+14 boundary would push this to 07-13.
		{3, "2026-07-12 22:00", "2026-07-12 23:00", 2, ""},
	}
	for _, b := range blocks {
		if _, err := db.Exec(
			`INSERT INTO atl_interval (id, start_time, end_time, category_id, comment, is_deleted)
			 VALUES (?, ?, ?, ?, ?, 0)`,
			b.id, kst(b.start), kst(b.end), b.category, b.comment); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

// fixtureWindow spans the fixture days without depending on today's date, so the
// test does not rot as the calendar moves.
func fixtureWindow() Window {
	return Window{
		From: time.Date(2026, 7, 11, 0, 0, 0, 0, KST),
		To:   time.Date(2026, 7, 13, 0, 0, 0, 0, KST),
	}
}

// TestCLIAnswerIsTZIndependent pins property 1: the same DB and the same command
// must give the same answer no matter what $TZ the caller's shell is in. Before
// the fix, `lifetract time` under TZ=UTC dropped 220 minutes from a day, and
// under TZ=Pacific/Kiritimati dragged the previous night's sleep into it —
// SQLite's 'localtime' modifier was bucketing each block by the invoking shell.
//
// This runs the real binary as a subprocess, once per zone, because that is the
// only thing that reproduces the bug. SQLite resolves 'localtime' from the
// process environment at startup and caches it: setting time.Local or os.Setenv
// inside the test process does not retarget it, so an in-process test of this
// property passes whether or not the bug is present. It has to be a subprocess.
func TestCLIAnswerIsTZIndependent(t *testing.T) {
	cfg := atlFixture(t)

	bin := filepath.Join(t.TempDir(), "lifetract")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// Every command that reports a date, so a regression anywhere is caught here.
	for _, args := range [][]string{
		{"time", "--from", "2026-07-11", "--to", "2026-07-13"},
		{"time", "--days", "7"},
		{"timeline", "--from", "2026-07-11", "--to", "2026-07-13"},
		{"status"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var baseline, baseZone string
			for _, tz := range []string{
				"Asia/Seoul",
				"UTC",
				"Pacific/Kiritimati", // +14, the far edge
				"Pacific/Midway",     // -11, and the other one
				"America/New_York",   // and one with DST, to be sure
			} {
				cmd := exec.Command(bin, append(args, "--data-dir", cfg.DataDir)...)
				cmd.Env = append(os.Environ(), "TZ="+tz)
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("TZ=%s: %v", tz, err)
				}

				if baseline == "" {
					baseline, baseZone = string(out), tz
					continue
				}
				if string(out) != baseline {
					t.Errorf("$TZ changed the answer — the shell must not be able to.\n"+
						"TZ=%s:\n%s\nTZ=%s:\n%s", baseZone, baseline, tz, out)
				}
			}
		})
	}
}

// TestMidnightCrossingBelongsToStartDay pins the attribution contract the
// timeline observatory builds its event mapping on: a block belongs to the day
// it starts on. Flipping this moves every downstream event, so it is spelled out.
func TestMidnightCrossingBelongsToStartDay(t *testing.T) {
	cfg := atlFixture(t)

	records, err := dbQueryTime(cfg, fixtureWindow())
	if err != nil {
		t.Fatal(err)
	}

	byDate := map[string]map[string]float64{}
	for _, r := range records {
		byDate[r.Date] = map[string]float64{}
		for _, c := range r.Categories {
			byDate[r.Date][c.Name] = c.Minutes
		}
	}

	// 21:14 → 05:48 is 514 minutes, all of it on 07-11.
	if got := byDate["2026-07-11"]["수면"]; got != 514 {
		t.Errorf("sleep on 2026-07-11: got %v, want 514 (whole block on its start day)", got)
	}
	if _, leaked := byDate["2026-07-12"]["수면"]; leaked {
		t.Error("sleep leaked into 2026-07-12: the block must not be split across days")
	}
	// Both reading blocks start on 07-12 and stay there.
	if got := byDate["2026-07-12"]["독서"]; got != 120 {
		t.Errorf("reading on 2026-07-12: got %v, want 120", got)
	}
}

// TestWindowSizeDoesNotChangeThePast pins property 2 — the quietest of the three
// bugs. cutoffTime used to be "this instant N days ago", a mid-day boundary, so
// the oldest day in a window came back truncated: --days 3 and --days 5 reported
// different totals for the same past day, with no error and no warning. Consumers
// (punchout, day-query) wrote the smaller number down as fact.
//
// A window is a lens, not an edit. Widening it must not rewrite the past.
func TestWindowSizeDoesNotChangeThePast(t *testing.T) {
	cfg := atlFixture(t)

	// Anchor "today" at 2026-07-13 so the fixture sits a fixed distance back;
	// daysWindow is relative to now, which is exactly the thing under test.
	day := func(n int) Window {
		today := time.Date(2026, 7, 13, 0, 0, 0, 0, KST)
		return Window{From: today.AddDate(0, 0, -n), To: today.AddDate(0, 0, 1)}
	}

	readOn11 := func(w Window) float64 {
		records, err := dbQueryTime(cfg, w)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range records {
			if r.Date != "2026-07-11" {
				continue
			}
			for _, c := range r.Categories {
				if c.Name == "수면" {
					return c.Minutes
				}
			}
		}
		return 0
	}

	// The sleep block starts 21:14 on 07-11. A mid-day cutoff two days back would
	// land at midday on 07-11 and clip it; a midnight cutoff keeps it whole.
	want := readOn11(day(5))
	if want != 514 {
		t.Fatalf("baseline sleep on 2026-07-11: got %v, want 514", want)
	}
	for _, n := range []int{2, 3, 4, 5, 10} {
		if got := readOn11(day(n)); got != want {
			t.Errorf("--days %d reported %v for 2026-07-11, but --days 5 reported %v; "+
				"window width must not change a past day", n, got, want)
		}
	}
}

// TestDaysWindowSnapsToMidnight pins the root of the truncation bug directly.
// cutoffTime used to return time.Now().AddDate(0,0,-days) — the current clock
// time, N days back. Any window opening at 10:19 discards everything that day
// before 10:19, so the oldest day came back short and no one was told.
//
// The boundary must be midnight, on the KST axis, regardless of when we ask.
func TestDaysWindowSnapsToMidnight(t *testing.T) {
	for _, days := range []int{0, 1, 2, 7, 365} {
		w := daysWindow(days)

		for _, b := range []struct {
			name string
			at   time.Time
		}{{"From", w.From}, {"To", w.To}} {
			at := b.at.In(KST)
			h, m, s := at.Clock()
			if h != 0 || m != 0 || s != 0 || at.Nanosecond() != 0 {
				t.Errorf("daysWindow(%d).%s = %s — must be KST midnight, not the wall clock we happened to run at",
					days, b.name, at.Format("2006-01-02 15:04:05.000 -0700"))
			}
			if _, off := at.Zone(); off != 9*3600 {
				t.Errorf("daysWindow(%d).%s is at offset %ds, want KST (+32400s)", days, b.name, off)
			}
		}

		// The window must reach back exactly `days` days and cover all of today.
		today := startOfDay(nowKST())
		if !w.From.Equal(today.AddDate(0, 0, -days)) {
			t.Errorf("daysWindow(%d).From = %s, want %s", days, w.From, today.AddDate(0, 0, -days))
		}
		if !w.To.Equal(today.AddDate(0, 0, 1)) {
			t.Errorf("daysWindow(%d).To = %s, want tomorrow's midnight %s", days, w.To, today.AddDate(0, 0, 1))
		}
	}
}

// TestHalfOpenWindowTiles pins the [from, to) contract: --to is exclusive, so
// back-to-back windows cover every block exactly once — no double count, no gap.
func TestHalfOpenWindowTiles(t *testing.T) {
	cfg := atlFixture(t)

	total := func(w Window) float64 {
		records, err := dbQueryTime(cfg, w)
		if err != nil {
			t.Fatal(err)
		}
		sum := 0.0
		for _, r := range records {
			for _, c := range r.Categories {
				sum += c.Minutes
			}
		}
		return sum
	}

	d := func(day int) time.Time { return time.Date(2026, 7, day, 0, 0, 0, 0, KST) }

	whole := total(Window{From: d(11), To: d(13)})
	first := total(Window{From: d(11), To: d(12)})  // 07-11 only
	second := total(Window{From: d(12), To: d(13)}) // 07-12 only

	if first+second != whole {
		t.Errorf("adjacent windows do not tile: [11,12)=%v + [12,13)=%v = %v, but [11,13)=%v",
			first, second, first+second, whole)
	}
	if first != 514 {
		t.Errorf("[07-11, 07-12) = %v, want 514: --to must be exclusive", first)
	}
}

// TestCommentNeverEscapes pins property 3. atl_interval.comment holds family
// names in plain text. The DB keeps them; this CLI is the only door out of it,
// so the door is where the contract has to live — not in the repo being private,
// which is a setting, not a guarantee.
//
// This asserts on the marshalled output of every query path, so adding the column
// to any SELECT fails here rather than in a published artifact.
// --days is not decoration. It used to be dropped the instant either bound
// appeared, so `--days 3 --to 2026-07-01` answered with 1,701 days and still
// called itself three. A *plausible* wrong number is worse than an obviously
// wrong one: nobody double-checks it, and it lands in a journal as fact.
func TestDaysCombinesWithBounds(t *testing.T) {
	day := func(s string) time.Time {
		t.Helper()
		d, err := time.ParseInLocation("2006-01-02", s, KST)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	tomorrow := startOfDay(nowKST()).AddDate(0, 0, 1)

	tests := []struct {
		name     string
		flags    map[string]string
		from, to time.Time
	}{
		{"days+to = N days ending at T",
			map[string]string{"days": "3", "to": "2026-07-01"},
			day("2026-06-28"), day("2026-07-01")},
		{"days+from = N days starting at F",
			map[string]string{"days": "3", "from": "2026-06-28"},
			day("2026-06-28"), day("2026-07-01")},
		{"from+to = exactly that window",
			map[string]string{"from": "2026-06-28", "to": "2026-07-01"},
			day("2026-06-28"), day("2026-07-01")},
		{"from alone runs through today",
			map[string]string{"from": "2026-06-28"},
			day("2026-06-28"), tomorrow},
		{"to alone leaves the floor open, by design",
			map[string]string{"to": "2026-07-01"},
			day("1970-01-01"), day("2026-07-01")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := flagRange(tt.flags)
			if err != nil {
				t.Fatal(err)
			}
			if !w.From.Equal(tt.from) || !w.To.Equal(tt.to) {
				t.Errorf("window = [%s, %s), want [%s, %s)",
					w.From.Format("2006-01-02"), w.To.Format("2006-01-02"),
					tt.from.Format("2006-01-02"), tt.to.Format("2006-01-02"))
			}
		})
	}
}

// A flag that is accepted and then ignored is a lie the tool tells quietly.
// Each of these used to be swallowed: the caller asked one question and was
// handed the answer to another, with nothing marking the substitution.
func TestIgnoredFlagsAreRefused(t *testing.T) {
	tests := []struct {
		name  string
		flags map[string]string
	}{
		// Right today only because the caller's arithmetic happens to match
		// ours. A contract that depends on that luck is not a contract.
		{"overspecified", map[string]string{"days": "3", "from": "2026-06-28", "to": "2026-07-01"}},
		{"unparsable from", map[string]string{"from": "garbage"}},
		{"unparsable to", map[string]string{"to": "2026-13-99"}},
		{"days is not a number", map[string]string{"days": "three", "to": "2026-07-01"}},
		{"backwards window", map[string]string{"from": "2026-07-01", "to": "2026-06-28"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := flagRange(tt.flags); err == nil {
				t.Errorf("flagRange(%v) = nil error — silently ignored", tt.flags)
			}
		})
	}

	// --days alone carries no bounds, so its check lives one level up.
	if _, err := newConfig(map[string]string{"days": "three"}); err == nil {
		t.Error("newConfig(--days three) = nil error — it used to quietly answer 7 days")
	}
}

func TestCommentNeverEscapes(t *testing.T) {
	cfg := atlFixture(t)
	const secret = "김가족 이름 평문" // seeded into the fixture's sleep block

	w := fixtureWindow()
	day := time.Date(2026, 7, 11, 0, 0, 0, 0, KST)

	paths := map[string]func() (interface{}, error){
		"time":     func() (interface{}, error) { return dbQueryTime(cfg, w) },
		"timeline": func() (interface{}, error) { return dbQueryTimeline(cfg, w) },
		"day":      func() (interface{}, error) { return dbQueryDay(cfg, day) },
		"status":   func() (interface{}, error) { return cmdStatus(cfg) },
	}

	for name, query := range paths {
		result, err := query()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(string(out), secret) {
			t.Errorf("%s leaked an interval comment — comments must never cross the CLI boundary:\n%s",
				name, out)
		}
	}
}

// freshnessFixture builds a DB whose newest records sit a given number of days
// behind today, on every stream. Staleness is measured against now, so the
// fixture is anchored to now too — otherwise the test would drift with the
// calendar. atlBehind and samsungBehind are separate because the two feeds
// arrive by different routes and stall independently.
func freshnessFixture(t *testing.T, atlBehind, samsungBehind int) *Config {
	t.Helper()
	cfg := &Config{DataDir: t.TempDir(), Days: 7}

	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := initSchema(db); err != nil {
		t.Fatal(err)
	}

	dayBack := func(n int) time.Time {
		return startOfDay(nowKST()).AddDate(0, 0, -n).Add(10 * time.Hour)
	}

	atl := dayBack(atlBehind)
	if _, err := db.Exec(`INSERT INTO atl_category (id, name) VALUES (1, '본짓')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO atl_interval (id, start_time, end_time, category_id, is_deleted)
		 VALUES (1, ?, ?, 1, 0)`,
		atl.Unix(), atl.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}

	sam := dayBack(samsungBehind)
	const layout = "2006-01-02 15:04:05.000"
	if _, err := db.Exec(
		`INSERT INTO sleep (id, uuid, start_time, end_time, duration_min)
		 VALUES (?, 'u1', ?, ?, 420)`,
		denoteID(sam), sam.Format(layout), sam.Add(7*time.Hour).Format(layout)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO steps_daily (id, date, count) VALUES (?, ?, 8000)`,
		denoteDayID(dateStr(sam)), dateStr(sam)); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func statusOf(t *testing.T, cfg *Config) *DBStatus {
	t.Helper()
	result, err := cmdStatus(cfg)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := result.(*StatusResult)
	if !ok {
		t.Fatalf("unexpected status type %T", result)
	}
	return st.Database
}

// TestStalenessAnnouncesItself pins the third failure mode: the aTimeLogger depth
// once sat two months behind because the phone export had stalled, and nothing
// said so — queries just quietly returned less. Staleness has to report itself.
func TestStalenessAnnouncesItself(t *testing.T) {
	const behind = 60
	db := statusOf(t, freshnessFixture(t, behind, behind))

	want := startOfDay(nowKST()).AddDate(0, 0, -behind).Format("2006-01-02")
	if db.LastTimeBlock != want {
		t.Errorf("last_time_block: got %q, want %q", db.LastTimeBlock, want)
	}
	if db.StaleDays != behind {
		t.Errorf("stale_days: got %d, want %d", db.StaleDays, behind)
	}
	if len(db.Warnings) == 0 {
		t.Fatal("a stale DB must warn: silence is how two months went unnoticed")
	}
}

// TestOneStaleStreamStillWarns is the case the first version of this check would
// have missed, and the case that was live in the real DB when it was written:
// aTimeLogger current to yesterday, Samsung Health two months behind. A freshness
// check that only watches the feed that broke last time is not a freshness check.
func TestOneStaleStreamStillWarns(t *testing.T) {
	db := statusOf(t, freshnessFixture(t, 1, 60)) // atl fresh, Samsung stalled

	if db.StaleDays != 60 {
		t.Errorf("stale_days: got %d, want 60 (the worst stream, not the healthiest)", db.StaleDays)
	}

	joined := strings.Join(db.Warnings, "\n")
	if !strings.Contains(joined, "Samsung") {
		t.Errorf("a stalled Samsung export must warn even while aTimeLogger flows, got: %q", joined)
	}
	if strings.Contains(joined, "aTimeLogger") {
		t.Errorf("aTimeLogger is current and must not warn, got: %q", joined)
	}
}

// TestFreshDBStaysQuiet is the other half of the contract — a warning that fires
// on healthy data is a warning people learn to ignore.
func TestFreshDBStaysQuiet(t *testing.T) {
	db := statusOf(t, freshnessFixture(t, 0, 0))

	if db.StaleDays != 0 {
		t.Errorf("stale_days: got %d, want 0 for records from today", db.StaleDays)
	}
	if len(db.Warnings) != 0 {
		t.Errorf("a current DB must not warn, got: %q", db.Warnings)
	}
}

// TestNewestExportWins pins the trap that appears the moment all Samsung exports
// share one folder (2026-07-14). Samsung stamps the export time into every
// filename, so a directory can hold two generations of the same CSV:
//
//	com.samsung.shealth.sleep.20260518102827.csv   ← stale, stops at 05-17
//	com.samsung.shealth.sleep.20260714110176.csv   ← current, runs to 07-13
//
// filepath.Glob returns them in lexical order, so the OLD one comes first. The
// code used to take matches[0] and would have read two months of nothing while
// reporting success — the same silent wrongness as every other bug found today.
func TestNewestExportWins(t *testing.T) {
	dir := t.TempDir()
	const kind = "com.samsung.shealth.sleep."

	for _, stamp := range []string{"20260714110176", "20260518102827"} { // written newest-first on purpose
		if err := os.WriteFile(filepath.Join(dir, kind+stamp+".csv"), []byte(stamp), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &Config{ShealthDir: dir, ShealthDirs: []string{dir}}
	got := cfg.shealthCSV(kind)

	if !strings.Contains(got, "20260714110176") {
		t.Errorf("shealthCSV picked %q — the newest export must win, not the first one glob happens to list", got)
	}
}

// TestSubKindIsNotMistakenForItsParent pins the other half, which the first
// version of newestCSV got wrong and which cost a whole table.
//
// The pattern is a prefix, so "com.samsung.shealth.stress." also matches
// stress.histogram and stress.base_histogram. The real stress export is 7 MB; the
// histograms are ~1 KB. Digits sort before letters, so taking the FIRST match had
// been picking the right file by luck — and switching to the LAST match to fix
// the stale-generation bug quietly started reading a 1 KB histogram as the stress
// log. Import reported "ok" and 0 rows.
//
// Only the file whose remainder is purely the timestamp is the kind we asked for.
func TestSubKindIsNotMistakenForItsParent(t *testing.T) {
	dir := t.TempDir()
	const kind = "com.samsung.shealth.stress."

	// Exactly what the 2026-07-14 export ships.
	files := map[string]string{
		"com.samsung.shealth.stress.20260714110176.csv":                "the real stress log",
		"com.samsung.shealth.stress.histogram.20260714110176.csv":      "a histogram",
		"com.samsung.shealth.stress.base_histogram.20260714110176.csv": "another histogram",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &Config{ShealthDir: dir, ShealthDirs: []string{dir}}
	got := filepath.Base(cfg.shealthCSV(kind))

	if got != "com.samsung.shealth.stress.20260714110176.csv" {
		t.Errorf("shealthCSV picked %q — a sub-kind is not its parent; only <kind>.<timestamp>.csv counts", got)
	}
}
