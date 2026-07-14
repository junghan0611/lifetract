package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Rejecting rows is never a licence to lose them.
//
// "A shrink no larger than what we rejected is fine" is what let the reject policy
// land at all — and it is a standing licence if it outlives that one import: the
// placeholders never leave the export, so `rejected` comes back at the same number
// every run, and that many real rows can vanish silently from then on.
//
//	run 1 (old build):  real rows + 1 placeholder    → ledger counts them all
//	run 2 (this build): placeholder rejected         → shrink of 1, explained ✓
//	run 3:              ONE REAL ROW VANISHES        → shrink of 1 ≤ 1, "explained" ✗
//
// Run 3 is the whole test. It came from a reviewer, and it was worth more than any
// of the tests already here: everything was green while a real row was being lost.
//
// The one-shot gate grants the allowance only against a baseline predating refusal
// accounting (rows_rejected IS NULL), never against a normal baseline. A later
// refusal policy gets a separately verified baseline transition; it does not turn
// refusals into a permanent budget that real loss can spend.
func TestRejectAllowanceDoesNotRenew(t *testing.T) {
	cfg, shealth := lossCfg(t)
	appendHeartRow(t, shealth, "1970-01-01 00:00:00.000", "hr-epoch", "79.0")

	var err error
	withOldBinary(t, func() { _, err = execImport(cfg) })
	if err != nil {
		t.Fatal(err)
	}
	legacyLedger(t, cfg) // the old build had no rows_rejected column at all

	second, err := execImport(cfg) // the migration: rejects explain the shrink
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != statusOK {
		t.Fatalf("migration = %q, want ok: %v", second.Status, second.Warnings)
	}
	migrated := stream(t, second, "heart_rate").Rows

	// The export comes back with the same placeholder — and one fewer real row.
	dropLastRealHeartRow(t, shealth)

	third, err := execImport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	hr := stream(t, third, "heart_rate")
	if hr.Rows != migrated-1 || hr.Rejected != 1 {
		t.Fatalf("fixture: rows=%d (want %d), rejected=%d (want 1)", hr.Rows, migrated-1, hr.Rejected)
	}

	if third.Status != statusWarn {
		t.Errorf("status = %q, want warning — a real row vanished and the rejects paid for it", third.Status)
	}
	if third.CandidatePath == "" {
		t.Error("the losing run was promoted — the reject count became a standing allowance")
	}
}

// New refused rows cannot pay for accepted rows that vanished. Conserving only
// rows+refused would miss this: 100 accepted + 14 refused and 90 accepted + 24
// refused both total 114, even though ten real rows disappeared.
func TestNewRefusalsCannotHideAcceptedLoss(t *testing.T) {
	b := &importBaseline{
		Prev:      map[string]int{"steps_daily": 100},
		PrePolicy: map[string]bool{"steps_daily": false},
	}
	status, _ := b.classify("steps_daily", 90, 24, nil)
	if status != statusShrunk {
		t.Fatalf("status = %q, want shrunk — new refusals paid for ten lost accepted rows", status)
	}
}

// dropLastRealHeartRow removes the final real row while leaving the appended
// placeholder in place.
func dropLastRealHeartRow(t *testing.T, shealth string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(shealth, "com.samsung.shealth.tracker.heart_rate.*.csv"))
	if len(matches) != 1 {
		t.Fatalf("heart_rate fixture: %v", matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("fixture too short: %d lines", len(lines))
	}
	kept := append(lines[:len(lines)-2:len(lines)-2], lines[len(lines)-1])
	if err := os.WriteFile(matches[0], []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
