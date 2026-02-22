package main

import (
	"os"
	"path/filepath"
)

// Config holds runtime configuration.
type Config struct {
	DataDir        string // self-tracking-data root
	ShealthDir     string // Samsung Health CSV export directory
	ATimeLoggerDB  string // aTimeLogger SQLite path
	Days           int
	Summary        bool
	Category       string
	ReadID         string // Denote ID for read command
}

func newConfig(flags map[string]string) *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, "repos", "gh", "self-tracking-data")
	if d, ok := flags["data-dir"]; ok {
		dataDir = d
	}

	// Find Samsung Health export directory (glob for samsunghealth_*)
	shealthDir := ""
	matches, _ := filepath.Glob(filepath.Join(dataDir, "samsunghealth_*"))
	if len(matches) > 0 {
		// Use the latest (last alphabetically)
		shealthDir = matches[len(matches)-1]
	}

	cfg := &Config{
		DataDir:        dataDir,
		ShealthDir:     shealthDir,
		ATimeLoggerDB:  filepath.Join(dataDir, "atimelogger", "database.db3"),
		Days:           flagDays(flags),
		Summary:        flags["summary"] == "true",
		Category:       flags["category"],
	}
	return cfg
}

// shealthCSV returns the full path to a Samsung Health CSV file.
// pattern is like "com.samsung.shealth.sleep" — it finds the matching CSV.
func (c *Config) shealthCSV(pattern string) string {
	if c.ShealthDir == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(c.ShealthDir, pattern+"*.csv"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}
