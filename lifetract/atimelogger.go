package main

// aTimeLogger data parsing.
// The database.db3 is SQLite but we avoid CGO dependency for now.
// Phase 1: Stub that reports availability.
// Phase 2: Will use modernc.org/sqlite or shell out to sqlite3.

import (
	"os"
	"sort"
)

type TimeRecord struct {
	Date       string         `json:"date"`
	Categories []TimeCategory `json:"categories"`
}

type TimeCategory struct {
	Name    string  `json:"name"`
	Minutes float64 `json:"minutes"`
}

type ATimeLoggerStatus struct {
	Path      string `json:"path"`
	Available bool   `json:"available"`
	SizeMB    float64 `json:"size_mb,omitempty"`
	Note      string `json:"note,omitempty"`
}

func getATimeLoggerStatus(cfg *Config) ATimeLoggerStatus {
	info, err := os.Stat(cfg.ATimeLoggerDB)
	if err != nil {
		return ATimeLoggerStatus{
			Path:      cfg.ATimeLoggerDB,
			Available: false,
			Note:      "database.db3 not found",
		}
	}

	return ATimeLoggerStatus{
		Path:      cfg.ATimeLoggerDB,
		Available: true,
		SizeMB:    float64(info.Size()) / (1024 * 1024),
		Note:      "SQLite parser not yet implemented (Phase 2)",
	}
}

func parseTimeRecords(cfg *Config, days int) ([]TimeRecord, error) {
	// Phase 2: implement SQLite parsing
	// For now return empty with note
	_ = days
	var results []TimeRecord
	sort.Slice(results, func(i, j int) bool {
		return results[i].Date > results[j].Date
	})
	return results, nil
}
