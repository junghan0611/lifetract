package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Config holds runtime configuration.
type Config struct {
	DataDir       string   // self-tracking-data root
	ShealthDir    string   // Primary Samsung Health CSV export directory (latest)
	ShealthDirs   []string // All Samsung Health export directories (for merge mode)
	ATimeLoggerDB string   // aTimeLogger SQLite path
	Days          int
	Range         *Window // explicit --from/--to; overrides Days when set
	Summary       bool
	Category      string
	ReadID        string // Denote ID for read command
	Exec          bool   // Execute mode (for import)
}

// queryWindow is the window range commands cover: an explicit --from/--to when
// given, otherwise the --days N window. Explicit ranges are what make a query
// reproducible — a relative window answers a different question every day.
func (c *Config) queryWindow() Window {
	if c.Range != nil {
		return *c.Range
	}
	return daysWindow(c.Days)
}

// flagRange builds the explicit window from --from/--to. The interval is
// half-open: --from 2026-07-01 --to 2026-07-14 covers July 1 through 13.
// Either bound may be omitted; the missing side is left open-ended.
func flagRange(flags map[string]string) *Window {
	fromS, hasFrom := flags["from"]
	toS, hasTo := flags["to"]
	if !hasFrom && !hasTo {
		return nil
	}

	w := Window{
		From: time.Date(1970, 1, 1, 0, 0, 0, 0, KST),
		To:   startOfDay(nowKST()).AddDate(0, 0, 1),
	}
	if hasFrom {
		t, err := time.ParseInLocation("2006-01-02", fromS, KST)
		if err != nil {
			return nil
		}
		w.From = t
	}
	if hasTo {
		t, err := time.ParseInLocation("2006-01-02", toS, KST)
		if err != nil {
			return nil
		}
		w.To = t
	}
	return &w
}

func newConfig(flags map[string]string) *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, "repos", "gh", "self-tracking-data")
	if d, ok := flags["data-dir"]; ok {
		dataDir = d
	}

	// Find all Samsung Health export directories (sorted → latest last)
	var shealthDirs []string
	matches, _ := filepath.Glob(filepath.Join(dataDir, "samsunghealth_*"))
	sort.Strings(matches) // alphabetical = chronological (timestamp in name)
	shealthDirs = matches

	// Primary = latest, or explicit --shealth-dir
	shealthDir := ""
	if d, ok := flags["shealth-dir"]; ok {
		shealthDir = d
	} else if len(shealthDirs) > 0 {
		shealthDir = shealthDirs[len(shealthDirs)-1]
	}

	cfg := &Config{
		DataDir:       dataDir,
		ShealthDir:    shealthDir,
		ShealthDirs:   shealthDirs,
		ATimeLoggerDB: filepath.Join(dataDir, "atimelogger", "database.db3"),
		Days:          flagDays(flags),
		Range:         flagRange(flags),
		Summary:       flags["summary"] == "true",
		Category:      flags["category"],
	}
	return cfg
}

// shealthCSV returns the full path to a Samsung Health CSV file.
// pattern is like "com.samsung.shealth.sleep" — it finds the matching CSV.
// Searches primary ShealthDir first, then falls back to other dirs.
func (c *Config) shealthCSV(pattern string) string {
	if c.ShealthDir != "" {
		if m := newestCSV(c.ShealthDir, pattern); m != "" {
			return m
		}
	}
	// Fallback: search all shealth dirs (newest first)
	for i := len(c.ShealthDirs) - 1; i >= 0; i-- {
		if m := newestCSV(c.ShealthDirs[i], pattern); m != "" {
			return m
		}
	}
	return ""
}

// newestCSV returns the most recent export of exactly one CSV kind in dir.
//
// Samsung names every file <kind>.<export-timestamp>.csv, and two things bite:
//
//  1. The pattern is a prefix, so it also catches OTHER kinds that extend it:
//     "com.samsung.shealth.stress." matches the real 7 MB stress export and also
//     stress.histogram (1 KB) and stress.base_histogram. Only the file whose
//     remainder is purely the timestamp is the kind we asked for.
//
//  2. Among the survivors, glob order is lexical, so the OLDEST export comes
//     first. Taking the first match would silently read a stale generation
//     whenever two land in one directory — which is what happens now that all
//     exports share a folder.
//
// So: keep only <pattern><digits>.csv, then take the newest. Getting either half
// wrong is silent — one reads a 1 KB histogram as if it were the stress log and
// reports zero rows; the other reads two-month-old data and reports success.
func newestCSV(dir, pattern string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern+"*.csv"))

	var exact []string
	for _, m := range matches {
		stamp := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), pattern), ".csv")
		if stamp != "" && strings.IndexFunc(stamp, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
			exact = append(exact, m)
		}
	}
	if len(exact) == 0 {
		return ""
	}
	sort.Strings(exact) // pure-digit stamps: lexical order == chronological
	return exact[len(exact)-1]
}
