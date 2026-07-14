package main

import (
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

// daysWindow covers the last N days plus today: [midnight N days ago, tomorrow).
//
// The boundaries are midnights, not "this instant N days ago". That distinction
// is load-bearing: a mid-day cutoff silently truncates the oldest day in the
// window, so widening the window would change what the CLI reports as fact
// about a day already past.
func daysWindow(days int) Window {
	today := startOfDay(nowKST())
	return Window{From: today.AddDate(0, 0, -days), To: today.AddDate(0, 0, 1)}
}

// dayWindow covers exactly the one day that t falls in.
func dayWindow(t time.Time) Window {
	start := startOfDay(t)
	return Window{From: start, To: start.AddDate(0, 0, 1)}
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

// parseInt parses a string to int, handling floats like "85.0".
func parseInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.Contains(s, ".") {
		f, _ := strconv.ParseFloat(s, 64)
		return int(f)
	}
	n, _ := strconv.Atoi(s)
	return n
}

// parseFloat parses a string to float64.
func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

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
