package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// flagRange builds the explicit window from --days/--from/--to. The interval is
// always half-open: --from 2026-07-01 --to 2026-07-14 covers July 1 through 13.
//
// Every accepted combination means exactly one thing:
//
//	--days N              [today−N, today+1)   (nil here; queryWindow's default)
//	--days N --to T       [T−N, T)             N days ending at T
//	--days N --from F     [F, F+N)             N days starting at F
//	--from F --to T       [F, T)
//	--from F              [F, today+1)
//	--to T                (1970, T)            lower bound open, by design
//	--days N --from F --to T   → error (overspecified)
//
// --days used to be dropped the moment either bound appeared, so
// `--days 3 --to 2026-07-01` answered with 1,701 days and still called itself
// three. That is worse than a wrong number: it is a *plausible* wrong number,
// and it goes into a journal as fact. A flag that is accepted and then ignored
// is a lie the tool tells quietly.
//
// The three-flag form is refused rather than resolved. It happens to give the
// right answer today — --from and --to agree with --days — but only by luck, and
// a contract that depends on the caller's arithmetic matching ours is not a
// contract. Say which two you mean.
//
// A bound that will not parse is an error, never a fallback. `--from garbage`
// used to quietly answer the last 7 days: the caller asked one question and was
// handed the answer to another, with nothing to mark the substitution.
func flagRange(flags map[string]string) (*Window, error) {
	fromS, hasFrom := flags["from"]
	toS, hasTo := flags["to"]
	daysS, hasDays := flags["days"]
	if !hasFrom && !hasTo {
		return nil, nil // plain --days (or nothing): queryWindow handles it
	}

	if hasDays && hasFrom && hasTo {
		return nil, fmt.Errorf("--days, --from and --to together are overspecified — drop one; --days N --to T means N days ending at T, --days N --from F means N days starting at F")
	}

	parseDay := func(name, s string) (time.Time, error) {
		t, err := time.ParseInLocation("2006-01-02", s, KST)
		if err != nil {
			return time.Time{}, fmt.Errorf("--%s %q is not a date (want YYYY-MM-DD)", name, s)
		}
		return t, nil
	}

	days := 0
	if hasDays {
		n, err := strconv.Atoi(daysS)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("--days %q is not a positive number of days", daysS)
		}
		days = n
	}

	var from, to time.Time
	switch {
	case hasFrom && hasTo:
		var err error
		if from, err = parseDay("from", fromS); err != nil {
			return nil, err
		}
		if to, err = parseDay("to", toS); err != nil {
			return nil, err
		}
	case hasFrom:
		var err error
		if from, err = parseDay("from", fromS); err != nil {
			return nil, err
		}
		if hasDays {
			to = from.AddDate(0, 0, days)
		} else {
			to = startOfDay(nowKST()).AddDate(0, 0, 1)
		}
	default: // hasTo
		var err error
		if to, err = parseDay("to", toS); err != nil {
			return nil, err
		}
		if hasDays {
			from = to.AddDate(0, 0, -days)
		} else {
			// No lower bound asked for, none imposed: everything before T.
			from = time.Date(1970, 1, 1, 0, 0, 0, 0, KST)
		}
	}

	if !to.After(from) {
		return nil, fmt.Errorf("empty window: --from %s is not before --to %s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
	return &Window{From: from, To: to}, nil
}

func newConfig(flags map[string]string) (*Config, error) {
	// A --days that will not parse is refused here too, not quietly rounded to
	// the default. Same rule as the bounds: the caller must never be answered a
	// question they did not ask.
	if s, ok := flags["days"]; ok {
		if n, err := strconv.Atoi(s); err != nil || n <= 0 {
			return nil, fmt.Errorf("--days %q is not a positive number of days", s)
		}
	}

	rng, err := flagRange(flags)
	if err != nil {
		return nil, err
	}

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
		Range:         rng,
		Summary:       flags["summary"] == "true",
		Category:      flags["category"],
	}
	return cfg, nil
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
