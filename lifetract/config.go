package main

import (
	"os"
	"path/filepath"
	"sort"
)

// Config holds runtime configuration.
type Config struct {
	DataDir        string   // self-tracking-data root
	ShealthDir     string   // Primary Samsung Health CSV export directory (latest)
	ShealthDirs    []string // All Samsung Health export directories (for merge mode)
	ATimeLoggerDB  string   // aTimeLogger SQLite path
	Days           int
	Summary        bool
	Category       string
	ReadID         string   // Denote ID for read command
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
		DataDir:        dataDir,
		ShealthDir:     shealthDir,
		ShealthDirs:    shealthDirs,
		ATimeLoggerDB:  filepath.Join(dataDir, "atimelogger", "database.db3"),
		Days:           flagDays(flags),
		Summary:        flags["summary"] == "true",
		Category:       flags["category"],
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
