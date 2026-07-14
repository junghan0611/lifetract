package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// --- Time axis ---

// KST is the single time axis every lifetract record lives on. Korea has had no
// DST since 1988 and every record here is Korean, so a fixed offset is exact —
// and unlike LoadLocation it needs no tzdata, which a static build may not ship.
//
// Nothing in this package may use time.Local: the answer must not depend on the
// shell that happened to invoke us.
var KST = time.FixedZone("KST", 9*60*60)

// nowKST returns the current instant on the KST axis.
func nowKST() time.Time {
	return time.Now().In(KST)
}

// startOfDay returns KST midnight opening the day t falls in.
func startOfDay(t time.Time) time.Time {
	t = t.In(KST)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, KST)
}

// --- Query window ---

// Window is the half-open interval [From, To) a query covers, on the KST axis.
// Half-open so that adjacent windows tile exactly: no day is counted twice and
// none falls through the seam.
type Window struct {
	From time.Time
	To   time.Time
}

// daysWindow covers exactly N calendar days including today: [tomorrow-N, tomorrow).
// Thus --days 1 is today, and --days 7 is today plus the previous six days.
// This is the same width contract as --days N --to T: plain --days simply has
// tomorrow as its implicit exclusive upper bound.
//
// The boundaries are midnights, not "this instant N days ago". That distinction
// is load-bearing: a mid-day cutoff silently truncates the oldest day in the
// window, so widening the window would change what the CLI reports as fact
// about a day already past.
func daysWindow(days int) Window {
	to := startOfDay(nowKST()).AddDate(0, 0, 1)
	return Window{From: to.AddDate(0, 0, -days), To: to}
}

// dayWindow covers exactly the one day that t falls in.
func dayWindow(t time.Time) Window {
	start := startOfDay(t)
	return Window{From: start, To: start.AddDate(0, 0, 1)}
}

// allTime is every record the source holds. It is what a lookup by Denote ID
// needs: the row it wants may sit anywhere in the history.
func allTime() Window {
	return Window{
		From: time.Date(1970, 1, 1, 0, 0, 0, 0, KST),
		To:   startOfDay(nowKST()).AddDate(0, 0, 1),
	}
}

// contains reports whether t falls in the half-open window [From, To).
//
// The CSV readers used to apply the lower bound only — a cutoff N days back, with
// nothing above it. In DB mode `--from 2026-07-01 --to 2026-07-03` meant those
// three days; in CSV mode the same command silently answered with today's rows.
// One CLI, one contract: the window decides, whichever source is behind it.
func (w Window) contains(t time.Time) bool {
	return !t.Before(w.From) && t.Before(w.To)
}

// shealthBounds renders the window as Samsung Health wall-clock strings, the
// form those tables store start_time in.
func (w Window) shealthBounds() (string, string) {
	const layout = "2006-01-02 15:04:05.000"
	return w.From.Format(layout), w.To.Format(layout)
}

// dateBounds renders the window as plain dates, for tables keyed by date.
func (w Window) dateBounds() (string, string) {
	return w.From.Format("2006-01-02"), w.To.Format("2006-01-02")
}

// epochBounds renders the window as unix seconds, for tables keyed by epoch.
func (w Window) epochBounds() (int64, int64) {
	return w.From.Unix(), w.To.Unix()
}

// --- Denote ID ---

// denoteID returns Denote identifier "YYYYMMDDTHHMMSS" from a time.
func denoteID(t time.Time) string {
	return t.In(KST).Format("20060102T150405")
}

// denoteDayID returns Denote identifier for a date "YYYYMMDDT000000".
func denoteDayID(dateStr string) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr // fallback
	}
	return t.Format("20060102T000000")
}

// parseDenoteID parses "YYYYMMDDTHHMMSS" into time.Time.
func parseDenoteID(id string) (time.Time, error) {
	return time.ParseInLocation("20060102T150405", id, KST)
}

// --- Time helpers ---

// parseShealthTime parses "2021-01-21 01:19:00.000" format. Samsung stores bare
// wall clock with no zone; it is Korean wall clock, so it is read as KST.
func parseShealthTime(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05.000", s, KST)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02 15:04:05", s, KST)
	}
	return t, err
}

// parseDayTime reads Samsung's `day_time` column, the midnight that names the day
// a daily-aggregate row is *about*. Exports ship it in two shapes: epoch millis in
// older dumps, a bare wall-clock string ("2026-07-13 00:00:00.000") in current ones.
// Both are read here so no caller has to guess.
//
// Never substitute create_time when this fails. create_time is when the row was
// written, not the day it measures — in this export it often falls on the previous
// calendar day for live rows and jumps to the dump date for backfilled ones.
// Reading it as the day silently misdated every step row we had. An unreadable day_time is a hole,
// and a hole must be counted, not filled.
//
// The column also appears in activity/stand/floors/vitality/pedometer summaries.
// Any importer added for those reads the day through here.
func parseDayTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("day_time: empty")
	}
	if t, err := parseShealthTime(s); err == nil {
		return t, nil
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMilli(ms).In(KST), nil
	}
	return time.Time{}, fmt.Errorf("day_time: unreadable %q", s)
}

// dateStr returns "2006-01-02" from a time.
func dateStr(t time.Time) string {
	return t.In(KST).Format("2006-01-02")
}

// timeStr returns "15:04" from a time.
func timeStr(t time.Time) string {
	return t.In(KST).Format("15:04")
}

// cutoffTime returns the KST midnight that opens a window reaching N days back.
// It is a midnight, not "now minus N days" — see daysWindow.
func cutoffTime(days int) time.Time {
	return startOfDay(nowKST()).AddDate(0, 0, -days)
}

// --- String/number helpers ---

// stripBOM removes UTF-8 BOM from a string.
func stripBOM(s string) string {
	if len(s) >= 3 && s[0] == 0xEF && s[1] == 0xBB && s[2] == 0xBF {
		return s[3:]
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == 0xFEFF {
		return s[size:]
	}
	return s
}

// numRow reads the numbers of one row and remembers the first field that was there
// and would not parse.
//
// The old parseInt/parseFloat folded a parse error into 0, and 0 was already taken.
// `heart_rate <= 0` and `steps <= 0` are policy filters, written to drop the
// measurements the watch could not take — so `heart_rate="garbage"` came back as 0
// and left through a door built for a different reason, counted as neither imported
// nor invalid. A stress score did worse: it LANDED as a real 0 and pulled the day's
// average down. Nothing anywhere moved.
//
// An empty field is a value the export does not have: legitimate, common, still 0.
// A field that is present and unreadable means the file is no longer the file we
// think we are reading, and that is what invalid counts.
type numRow struct{ err error }

func (n *numRow) float(field, s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		if n.err == nil {
			n.err = fmt.Errorf("%s: %q is not a number", field, s)
		}
		return 0
	}
	return f
}

// int reads a whole number, tolerating the "85.0" the export writes for integers.
func (n *numRow) int(field, s string) int {
	return int(n.float(field, s))
}

// bad reports whether any field in this row failed to parse.
func (n *numRow) bad() bool { return n.err != nil }

// round1 rounds to 1 decimal place.
func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

// firstNonEmpty returns the first non-empty value from rec for given keys.
func firstNonEmpty(rec map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := rec[k]; ok && v != "" {
			return v
		}
	}
	return ""
}
