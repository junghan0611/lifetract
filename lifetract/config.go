package main

import (
	"os"
	"path/filepath"
	"sort"
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
		matches, _ := filepath.Glob(filepath.Join(c.ShealthDir, pattern+"*.csv"))
		if len(matches) > 0 {
			return matches[0]
		}
	}
	// Fallback: search all shealth dirs (newest first)
	for i := len(c.ShealthDirs) - 1; i >= 0; i-- {
		matches, _ := filepath.Glob(filepath.Join(c.ShealthDirs[i], pattern+"*.csv"))
		if len(matches) > 0 {
			return matches[0]
		}
	}
	return ""
}
