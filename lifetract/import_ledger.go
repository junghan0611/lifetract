package main

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The import ledger: what every stream was worth the last time we looked.
//
// On 2026-07-14 a prefix glob started matching stress.histogram (1 KB) instead of
// the real stress export (7 MB). 27,598 rows became 0 and `import` still said
// "ok". The tests were green. What caught it was a human noticing the total had
// fallen — 203,539 → 175,941. The tool itself never said a word, and if that one
// number hadn't been on screen the stress axis would have shipped dead.
//
// The material to catch it was already here. import_log records what each stream
// imported; it just wasn't readable across runs. execImport deletes the DB before
// rebuilding it, so the ledger died with the file every time, and logImport only
// wrote a row when there were rows to write — so a stream that came back empty
// left no trace at all. Both halves conspired to forget exactly the fact we
// needed.
//
// So: read the ledger before the DB is replaced, carry it into the new one, and
// record every stream including the empty ones. A count of zero is a fact worth
// keeping — it is the whole fact, in fact.

// Status words. "ok" said two things at once — we read the source, and something
// was in it — and callers only ever checked the first.
const (
	statusOK     = "ok"
	statusEmpty  = "empty"  // read cleanly, and there was nothing there
	statusShrunk = "shrunk" // fewer rows than the previous import
	statusWarn   = "warning"
)

// retiredStreams are streams this tool deliberately no longer imports, with the
// reason it stopped.
//
// hrv is the first: the Samsung export's HRV file has no rmssd column at all — the
// measurement lives in a binning_data JSON this tool does not read. Every row of it
// landed as hrv_rmssd = 0.0, 1,058 of them in the live DB, and because the COUNT was
// right the loss guard was satisfied the whole time. A stream can be entirely
// present and hold none of what it claims to. We never had HRV; we had timestamps.
//
// Retiring a stream HERE, rather than only deleting its importer, is what keeps the
// removal from becoming one more silence. The baseline drops these on purpose and
// the run says which — instead of a name simply ceasing to appear in the ledger,
// which is indistinguishable from a stream that vanished by accident.
var retiredStreams = map[string]string{
	"hrv": "the export carries no rmssd column — every row imported as 0.0, so the stream was never real",
}

// nowStamp is the clock the ledger reads, and a run must read it exactly once.
// It is a variable so a test can make it tick on every read: the defect it guards
// against — a stamp taken per row — is invisible to a clock that never moves, and
// a three-row fixture imports well inside one second. Production is where the
// second boundary falls in the middle of a run, and production is where it fell.
var nowStamp = func() string { return time.Now().Format(time.RFC3339) }

type ledgerRow struct {
	ImportID   int
	ImportedAt string
	Source     string
	Table      string
	Rows       int
	// Rejected is NULL for any run written before the reject policy existed. Not
	// zero — NULL. "This build refused nothing" and "this build could not refuse
	// anything" are different facts, and collapsing them into 0 is what would let
	// the migration allowance renew itself on every future import.
	Rejected   sql.NullInt64
	SourcePath string
}

// importBaseline is what the previous imports know, and this import must answer to.
type importBaseline struct {
	Exists   bool
	ReadErr  string         // the ledger was there and could not be read
	At       string         // when the previous import ran
	MaxRunID int            // highest import_id on record
	Prev     map[string]int // rows at the previous import (zeros included)
	// PrePolicy marks a stream whose previous count was taken by a build with no
	// reject filter — so Prev still includes the placeholder rows. Only such a
	// stream may explain a shrink with this run's rejects, and only once: after
	// that import the ledger carries a reject count and the allowance is gone.
	PrePolicy  map[string]bool
	LastGood   map[string]int    // most recent NON-zero count on record
	LastGoodAt map[string]string // when that count was recorded
	History    []ledgerRow
}

// nextRunID is the id this import will write under.
func (b *importBaseline) nextRunID() int { return b.MaxRunID + 1 }

// numberRuns fills in run ids for rows written before import_log had them.
//
// The timestamps cannot group a run — that is the entire reason import_id exists:
// logImport used to stamp time.Now() per row, so one import landed under two or
// three different seconds. The structure can, though. A run writes each stream at
// most once, so a stream seen twice means a new run began.
func (b *importBaseline) numberRuns() {
	run := 0
	seen := map[string]bool{}

	for i := range b.History {
		r := &b.History[i]
		if r.ImportID > 0 { // already numbered by a build that knew how
			run = r.ImportID
			seen = map[string]bool{}
			continue
		}
		if run == 0 || seen[r.Table] {
			run++
			seen = map[string]bool{}
		}
		seen[r.Table] = true
		r.ImportID = run
	}
	b.MaxRunID = run
}

// readBaseline loads the ledger from the DB that is about to be replaced.
// Anything missing — no DB, no import_log (an older build), an unreadable file —
// means no baseline, which is a fact we can state rather than an error.
func readBaseline(path string) *importBaseline {
	b := &importBaseline{
		Prev:       map[string]int{},
		PrePolicy:  map[string]bool{},
		LastGood:   map[string]int{},
		LastGoodAt: map[string]string{},
	}

	// No DB at all is the one honest "first import". Every other failure below is a
	// baseline we were supposed to have and don't — and a check that quietly stops
	// checking is the same disease it was built to cure, wearing the safety net's
	// clothes. So those say so out loud (ReadErr) instead of passing for new.
	if _, err := os.Stat(path); err != nil {
		return b
	}
	db, err := openDB(path)
	if err != nil {
		b.ReadErr = err.Error()
		return b
	}
	defer db.Close()

	// Older ledgers are missing columns, one generation at a time. Each fallback
	// selects a literal in place of the column it lacks — NULL for rows_rejected,
	// which is precisely the claim "this run predates the reject policy". Falling
	// through to "no baseline" instead would be the worst outcome available: the
	// import would call itself the first one and notice nothing.
	rows, err := db.Query(`SELECT import_id, imported_at, source, table_name, rows_imported, rows_rejected, source_path
		FROM import_log ORDER BY id`)
	if err != nil {
		rows, err = db.Query(`SELECT import_id, imported_at, source, table_name, rows_imported, NULL, source_path
			FROM import_log ORDER BY id`)
	}
	if err != nil {
		rows, err = db.Query(`SELECT 0, imported_at, source, table_name, rows_imported, NULL, source_path
			FROM import_log ORDER BY id`)
		if err != nil {
			b.ReadErr = err.Error()
			return b
		}
	}
	defer rows.Close()

	for rows.Next() {
		var r ledgerRow
		var n sql.NullInt64
		var sp sql.NullString
		if err := rows.Scan(&r.ImportID, &r.ImportedAt, &r.Source, &r.Table, &n, &r.Rejected, &sp); err != nil {
			// Skipping the row would leave a baseline missing whichever stream
			// failed to scan — and a stream missing from the baseline is a stream
			// whose disappearance nobody notices. Half a ledger is worse than none,
			// because none announces itself.
			b.ReadErr = err.Error()
			return b
		}
		r.Rows = int(n.Int64)
		r.SourcePath = sp.String

		// A retired stream leaves the ledger for good: not carried into the new DB,
		// not compared against, not counted in the total. Otherwise its last count
		// would sit in the baseline forever, and every future import would report a
		// stream it no longer has as lost.
		if _, retired := retiredStreams[r.Table]; retired {
			continue
		}
		b.History = append(b.History, r)

		// Rows arrive in id order, so the last write per stream is the newest.
		b.Prev[r.Table] = r.Rows
		b.PrePolicy[r.Table] = !r.Rejected.Valid
		if r.Rows > 0 {
			b.LastGood[r.Table] = r.Rows
			b.LastGoodAt[r.Table] = r.ImportedAt
		}
		if r.ImportedAt > b.At {
			b.At = r.ImportedAt
		}
	}
	// An iteration that ended in an error read fewer rows than the ledger holds,
	// and says so nowhere unless asked. Ask.
	if err := rows.Err(); err != nil {
		b.ReadErr = err.Error()
		b.History = nil
		return b
	}

	b.Exists = len(b.History) > 0
	b.numberRuns()
	return b
}

// carryForward re-inserts the old ledger into the freshly built DB. Without this
// every import is the first import, and the first import can never notice a loss.
//
// The recorded timestamps are copied as they were found, including the ones that
// disagree within a run. They are what the clock actually said; rewriting them to
// look tidy would be inventing a record. The run they belong to is carried in
// import_id instead, which is a fact we can establish rather than a fact we wish
// had been written down.
//
// Nothing is pruned. Trimming old runs would eventually drop the last non-zero
// count of a stream that died long ago — and that count is exactly what makes the
// warning keep firing. A ledger that forgets is the silence we are here to end.
// At ~9 rows an import, it can afford to remember.
func carryForward(db *sql.DB, b *importBaseline) error {
	if len(b.History) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO import_log
		(import_id, imported_at, source, table_name, rows_imported, rows_rejected, source_path)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, r := range b.History {
		// r.Rejected is carried as it was found — NULL stays NULL. Writing 0 here
		// would forge a policy the old run never had, and the forgery would look
		// exactly like a clean migration on every future import.
		if _, err := stmt.Exec(r.ImportID, r.ImportedAt, r.Source, r.Table, r.Rows, r.Rejected, r.SourcePath); err != nil {
			tx.Rollback()
			return fmt.Errorf("carry %s (run %d): %w", r.Table, r.ImportID, err)
		}
	}
	return tx.Commit()
}

// prevTotal is what the last import added up to — the number a human would have
// had to notice by eye.
func (b *importBaseline) prevTotal() int {
	total := 0
	for _, n := range b.Prev {
		total += n
	}
	return total
}

// classify says what a stream's import is worth reporting, and warns when the
// count went somewhere it shouldn't have.
//
// err is the importer's own complaint (a missing CSV). It does not excuse us from
// noticing that the stream used to have rows: "csv not found" on a stream that
// carried 27,598 rows yesterday is the loudest case there is, not a quiet one.
//
// The threshold is deliberately not invented here. Rows-before-and-none-now is
// not a judgment call, so that is what warns. A partial drop is reported as a
// number (prev_rows/delta on every stream) and warned about only against the
// immediately previous import, where it is an event rather than an opinion.
// classify judges one stream. rejected is how many rows this run refused on
// purpose (placeholder timestamps).
//
// The reject filter sets a trap for itself. Refusing rows makes the stream shrink
// against a baseline that counted them, and a shrink is what the loss guard
// refuses to promote — so the very first import after the filter lands would jam
// the door the guard was built to watch, permanently, since the count can never
// climb back.
//
// The obvious escape — "a shrink no larger than what we rejected is fine" — is
// worse than the jam. The placeholders never leave the export, so `rejected` comes
// back at 14 on every future run, and 14 rows of real, silent loss are waved
// through every import from then on. A reviewer found it in three runs:
//
//	run 1 (old build):  real 100 + 14 placeholders   → ledger says 114
//	run 2 (this build): 100 kept, 14 rejected        → shrink of 14, explained ✓
//	run 3:              10 REAL rows vanish          → shrink of 10 ≤ 14, "explained" ✗
//
// So the allowance is spent once, on the migration, and never renewed. A ledger
// entry written before the policy existed (rows_rejected IS NULL) still counts the
// placeholders, and only against such an entry may this run's rejects account for
// the drop. Once a run records its rejects, the baseline is accepted-rows-only and
// any shrink in accepted rows is a loss again — including a shrink that hides
// under a sudden flood of rejects, because that flood costs accepted rows too.
func (b *importBaseline) classify(name string, rows, rejected int, err error) (status, warning string) {
	prev, hadPrev := b.Prev[name]
	good, hadGood := b.LastGood[name]

	// rows + rejected is what the source still offers. Against a pre-policy
	// baseline — which counted the placeholders as imported — that sum has to
	// reach the old number, or something real went missing on top of the cleanup.
	explained := hadPrev && b.PrePolicy[name] && rows < prev && rows+rejected >= prev

	switch {
	case err != nil:
		status = err.Error()
	case rows == 0:
		status = statusEmpty
	case hadPrev && rows < prev && !explained:
		status = statusShrunk
	default:
		status = statusOK
	}

	// A stream that had rows and now has none. No baseline, no claim about loss —
	// the first import has nothing to compare against and says so instead of
	// crying wolf.
	if rows == 0 && hadGood {
		return status, fmt.Sprintf("%s: %s rows (%s) → 0 — stream lost [%s]",
			name, comma(good), shortStamp(b.LastGoodAt[name]), status)
	}

	// Not being able to READ a source is a different fact from having nothing to
	// compare it against, and it is true on the first import too. "No baseline" is
	// a reason not to claim a loss; it was never a reason to call an unread source
	// fine.
	if err != nil {
		return status, fmt.Sprintf("%s: source unreadable — %v", name, err)
	}
	if status == statusShrunk {
		msg := fmt.Sprintf("%s: %s → %s rows (%s) — fewer than the last import",
			name, comma(prev), comma(rows), signed(rows-prev))
		if rejected > 0 {
			msg += fmt.Sprintf("; only %s of those are the placeholders this run rejected — the rest is unaccounted for",
				comma(rejected))
		}
		return status, msg
	}
	return status, ""
}

// comma renders 27598 as 27,598 — the loss should be legible at a glance, since
// being legible at a glance is the only reason the original one got caught.
func comma(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + comma(-n)
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func signed(n int) string {
	if n > 0 {
		return "+" + comma(n)
	}
	return comma(n)
}

// shortStamp trims an RFC3339 timestamp to the day and minute.
func shortStamp(s string) string {
	if len(s) >= 16 {
		return strings.Replace(s[:16], "T", " ", 1)
	}
	return s
}
