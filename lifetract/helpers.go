package main

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// --- Denote ID ---

// denoteID returns Denote identifier "YYYYMMDDTHHMMSS" from a time.
func denoteID(t time.Time) string {
	return t.Format("20060102T150405")
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
	return time.ParseInLocation("20060102T150405", id, time.Local)
}

// --- Time helpers ---

// parseShealthTime parses "2021-01-21 01:19:00.000" format.
func parseShealthTime(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05.000", s, time.Local)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	}
	return t, err
}

// dateStr returns "2006-01-02" from a time.
func dateStr(t time.Time) string {
	return t.Format("2006-01-02")
}

// timeStr returns "15:04" from a time.
func timeStr(t time.Time) string {
	return t.Format("15:04")
}

// cutoffTime returns time N days ago from now.
func cutoffTime(days int) time.Time {
	return time.Now().AddDate(0, 0, -days)
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
