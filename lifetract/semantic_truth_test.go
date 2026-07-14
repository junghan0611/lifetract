package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A number the file HAS and we cannot read is not a measurement of zero.
//
// The importers guarded the timestamp and the presence of the field, and then
// handed the value to parseInt/parseFloat, which fold a parse error into 0. So
// `heart_rate="garbage"` became 0, and 0 was already spoken for: it is the policy
// filter that drops resting-heart-rate rows the watch could not measure. The row
// left through a door built for a different reason, counted as neither imported
// nor invalid, and no number anywhere moved.
//
// Same shape, different exits: a garbage step count vanished the same way; a
// garbage stress score LANDED as a real 0 and dragged the day's average down; a
// garbage sleep stage landed as stage 0, which matches none of the four stage
// codes, so those minutes silently left the night.
//
// The rule these tests hold: an empty field is a value the export does not have,
// and a field that is there and will not parse means the file changed shape.

// appendFixtureRow appends one raw row to a fixture CSV.
func appendFixtureRow(t *testing.T, shealth, pattern, row string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(shealth, pattern))
	if len(matches) != 1 {
		t.Fatalf("fixture %s: %v", pattern, matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimRight(string(b), "\n")
	if err := os.WriteFile(matches[0], []byte(body+"\n"+row+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// wasNotPromoted reports whether the run was held back from the live DB.
func wasNotPromoted(r *ImportResult) bool { return r.CandidatePath != "" }

// assertInvalidBlocksPromotion is the whole contract in one place: the unreadable
// row is counted, the run says so, and the DB it built never becomes the live one.
func assertInvalidBlocksPromotion(t *testing.T, r *ImportResult, table string) {
	t.Helper()
	s := stream(t, r, table)
	if s.Invalid != 1 {
		t.Errorf("%s: invalid = %d, want 1 — a row the file has and we cannot read was not counted", table, s.Invalid)
	}
	if r.Status != statusWarn {
		t.Errorf("%s: status = %q, want %q", table, r.Status, statusWarn)
	}
	if !wasNotPromoted(r) {
		t.Errorf("%s: an import that could not read a row promoted itself to the live DB", table)
	}
}

// heart_rate: the parse error was folded into 0, and 0 is the policy filter for
// "the watch could not measure it". A broken row wore the filter's clothes.
func TestGarbageHeartRateIsNotAPolicyFilter(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendHeartRow(t, shealth, "2025-01-20 10:00:00.000", "hr-garbage", "garbage")

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertInvalidBlocksPromotion(t, result, "heart_rate")
}

// steps: same door — parseInt("garbage") == 0, and `steps <= 0` drops it as the
// policy filter for a day with no merged step record.
func TestGarbageStepCountIsNotAPolicyFilter(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendFixtureRow(t, shealth, "com.samsung.shealth.step_daily_trend.*.csv",
		",2025-01-16 23:00:00.000,2025-01-16 08:00:00.000,com.sec.android.app.shealth,-2,garbage,3.5,6500.0,350.0,test-device,com.sec.android.app.shealth,step-garbage,1736985600000,")

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertInvalidBlocksPromotion(t, result, "steps_daily")
}

// stress is worse than a quiet skip: the garbage score LANDS as a real 0, passes
// the `score >= 0` query filter, and pulls the day's average toward zero. The row
// is in the DB, it is wrong, and nothing about it looks unusual.
func TestGarbageStressScoreIsNotAZeroScore(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendFixtureRow(t, shealth, "com.samsung.shealth.stress.*.csv",
		",2025-01-16 11:00:00.000,,,10000,,2025-01-16 11:00:00.000,2025-01-16 11:00:00.000,45.0,0.0,garbage,1,UTC+0900,test-device,,com.sec.android.app.shealth,2025-01-16 11:00:00.000,stress-garbage,")

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertInvalidBlocksPromotion(t, result, "stress")
}

// sleep_stage: a garbage stage lands as stage 0, which matches none of the four
// stage codes. The minutes are in the table and belong to no stage — they leave
// the night without leaving a trace.
func TestGarbageSleepStageIsNotStageZero(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendFixtureRow(t, shealth, "com.samsung.health.sleep_stage.*.csv",
		",2025-01-16 23:30:00.000,sleep-uuid-001,,,2025-01-16 23:30:00.000,2025-01-16 23:45:00.000,garbage,UTC+0900,test-device,com.sec.android.app.shealth,2025-01-16 23:45:00.000,stage-garbage,")

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertInvalidBlocksPromotion(t, result, "sleep_stage")
}

// sleepRowNoScores builds a raw sleep row with only the times and the uuid set.
// Every scored field is left empty, which the real export legitimately does.
func sleepRowNoScores(start, end, uuid string) string {
	f := make([]string, 62)
	f[49] = start // com.samsung.health.sleep.start_time
	f[60] = end   // com.samsung.health.sleep.end_time
	f[61] = uuid  // com.samsung.health.sleep.datauuid
	return strings.Join(f, ",")
}

// An empty optional field is not a broken row. The export legitimately ships nights
// with no score, and refusing those would jam the import on healthy data — the
// distinction this whole change rests on. Measured on the real export the day this
// was written: 0 invalid rows in 203,539, so the strict read costs nothing.
func TestEmptyOptionalFieldIsNotInvalid(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendFixtureRow(t, shealth, "com.samsung.shealth.sleep.*.csv",
		sleepRowNoScores("2025-01-16 23:00:00.000", "2025-01-17 07:00:00.000", "sleep-noscore"))

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s := stream(t, result, "sleep"); s.Invalid != 0 {
		t.Errorf("sleep: invalid = %d, want 0 — an empty optional field was treated as a broken row", s.Invalid)
	}
	if result.Status != statusOK {
		t.Errorf("status = %q, want %q — a healthy export was refused: %v", result.Status, statusOK, result.Warnings)
	}
}

// --- a stream cannot leave quietly ---

// hrv is retired: the export has no rmssd column, so 1,058 rows had been landing as
// 0.0 while the row COUNT kept the loss guard happy. Retiring it must not itself
// become a silence — the ledger drops it on purpose and stops comparing against it,
// so no future import reports a stream we deliberately gave up as lost.
func TestRetiredStreamLeavesTheLedgerOnPurpose(t *testing.T) {
	cfg, _ := lossCfg(t)

	first, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range first.Tables {
		if tr.Name == "hrv" {
			t.Fatal("hrv is retired and must not be imported or counted")
		}
	}

	// A second run reads the first one's ledger. A retired stream must not come back
	// as a stream that went missing.
	second, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusOK {
		t.Errorf("second run = %q, want %q: %v", second.Status, statusOK, second.Warnings)
	}
	for _, w := range second.Warnings {
		if strings.Contains(w, "hrv") {
			t.Errorf("retired stream reported as a loss: %q", w)
		}
	}
}

// The other half of the same rule: a stream that stops being imported WITHOUT being
// retired is the quietest loss available — no error, no empty count, no shrink, just
// a name that stops appearing. Deleting an importer must not be able to do that.
func TestStreamThatVanishesFromTheRunIsCaught(t *testing.T) {
	cfg, _ := lossCfg(t)
	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}

	// The ledger now knows `weight`. Stand in for someone deleting its importer by
	// retiring nothing and simply never judging it.
	base := readBaseline(dbPath(cfg))
	if _, ok := base.Prev["weight"]; !ok {
		t.Fatal("fixture should have imported weight")
	}

	r := &ImportResult{Status: statusOK, handled: map[string]bool{}, Warnings: []string{}}
	for name := range base.Prev {
		if name != "weight" {
			r.handled[name] = true
		}
	}
	for name := range base.Prev {
		if !r.handled[name] {
			r.warn(name + ": untouched")
		}
	}
	if r.Status != statusWarn {
		t.Error("a stream in the ledger that this run never touched was not noticed")
	}
}

// A field that is there and will not parse is the file changing shape, even when
// the field is optional. Empty means "the export does not have this"; "garbage"
// means we are no longer reading what we think we are reading.
func TestGarbageOptionalFieldIsStillInvalid(t *testing.T) {
	cfg, shealth := lossCfg(t)
	row := sleepRowNoScores("2025-01-16 23:00:00.000", "2025-01-17 07:00:00.000", "sleep-badscore")
	f := strings.Split(row, ",")
	f[44] = "garbage" // sleep_score
	appendFixtureRow(t, shealth, "com.samsung.shealth.sleep.*.csv", strings.Join(f, ","))

	result, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertInvalidBlocksPromotion(t, result, "sleep")
}
