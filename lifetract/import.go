package main

import (
	"database/sql"
	"os"
	"time"
)

// cmdImport converts raw CSV + aTimeLogger → lifetract.db.
// Without --exec: shows manifest (dry run).
// With --exec: performs actual import.
func cmdImport(cfg *Config) (interface{}, error) {
	if cfg.Exec {
		return execImport(cfg)
	}

	manifest := &ImportManifest{
		CreatedAt: time.Now().Format(time.RFC3339),
		Sources:   []ImportSource{},
	}

	// imported says whether --exec actually loads this source. steps_pedometer and
	// activity_summary are listed because they are in the export, not because they
	// land in the DB.
	shealthSources := []struct {
		name     string
		pattern  string
		imported bool
	}{
		{"sleep", "com.samsung.shealth.sleep.", true},
		{"sleep_stage", "com.samsung.health.sleep_stage.", true},
		{"heart_rate", "com.samsung.shealth.tracker.heart_rate.", true},
		{"steps_daily", "com.samsung.shealth.step_daily_trend.", true},
		{"steps_pedometer", "com.samsung.shealth.tracker.pedometer_step_count.", false},
		{"stress", "com.samsung.shealth.stress.", true},
		{"exercise", "com.samsung.shealth.exercise.2", true},
		{"weight", "com.samsung.health.weight.", true},
		{"hrv", "com.samsung.health.hrv.", true},
		{"activity_summary", "com.samsung.shealth.activity.day_summary.", false},
	}

	for _, s := range shealthSources {
		path := cfg.shealthCSV(s.pattern)
		if path == "" {
			continue
		}
		rows, err := countCSVRows(path)
		if err != nil {
			continue
		}
		info, _ := os.Stat(path)
		manifest.Sources = append(manifest.Sources, ImportSource{
			Name:     s.name,
			Type:     "samsung_health_csv",
			Path:     path,
			RawRows:  rows,
			Imported: s.imported,
			SizeMB:   float64(info.Size()) / (1024 * 1024),
		})
		manifest.RawSourceRows += rows
	}

	if info, err := os.Stat(cfg.ATimeLoggerDB); err == nil {
		rows := atlIntervalCount(cfg)
		manifest.Sources = append(manifest.Sources, ImportSource{
			Name:     "atimelogger",
			Type:     "sqlite",
			Path:     cfg.ATimeLoggerDB,
			RawRows:  rows,
			Imported: true,
			SizeMB:   float64(info.Size()) / (1024 * 1024),
		})
		manifest.RawSourceRows += rows
	}

	manifest.CategoryPolicy = defaultCategoryPolicy()
	manifest.EstimatedDBSizeMB = 30
	manifest.Note = "raw_source_rows counts rows in the sources, not rows that will be imported: " +
		"sources with imported=false are skipped, and --exec dedups and filters (run it for the real count)"

	return manifest, nil
}

// atlIntervalCount counts what the aTimeLogger DB actually holds. The manifest
// used to carry a hardcoded "13,094 intervals, 18 categories" — true once, then
// quietly false (14,617 today, and nobody noticed the note aging). A dry run
// exists to say what is there, so it has to look.
func atlIntervalCount(cfg *Config) int {
	db, err := sql.Open("sqlite", cfg.ATimeLoggerDB)
	if err != nil {
		return 0
	}
	defer db.Close()

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM time_interval2`).Scan(&n)
	return n
}

// ImportManifest is a dry run: what is in the sources, not what will land in the
// DB. The two differ by about 22k rows — the manifest counts CSV lines, while the
// import drops per-device duplicates (steps keeps only source_type=-2), skips two
// sources entirely, and dedups by UUID. The field is named raw_source_rows because
// it was previously named total_rows, sat next to the import's total_rows, and
// meant something else. Two numbers with one name is how a tool starts lying by
// accident.
type ImportManifest struct {
	CreatedAt         string          `json:"created_at"`
	Sources           []ImportSource  `json:"sources"`
	RawSourceRows     int             `json:"raw_source_rows"`
	Note              string          `json:"note"`
	EstimatedDBSizeMB int             `json:"estimated_db_size_mb"`
	CategoryPolicy    *CategoryPolicy `json:"category_policy"`
}

type ImportSource struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Path     string  `json:"path"`
	RawRows  int     `json:"raw_rows,omitempty"`
	Imported bool    `json:"imported"` // false = present in the export, not loaded by --exec
	SizeMB   float64 `json:"size_mb"`
	Note     string  `json:"note,omitempty"`
}
