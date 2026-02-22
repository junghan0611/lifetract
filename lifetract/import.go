package main

import (
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

	shealthSources := []struct {
		name    string
		pattern string
	}{
		{"sleep", "com.samsung.shealth.sleep."},
		{"sleep_stage", "com.samsung.health.sleep_stage."},
		{"heart_rate", "com.samsung.shealth.tracker.heart_rate."},
		{"steps_daily", "com.samsung.shealth.step_daily_trend."},
		{"steps_pedometer", "com.samsung.shealth.tracker.pedometer_step_count."},
		{"stress", "com.samsung.shealth.stress."},
		{"exercise", "com.samsung.shealth.exercise.2"},
		{"weight", "com.samsung.health.weight."},
		{"hrv", "com.samsung.health.hrv."},
		{"activity_summary", "com.samsung.shealth.activity.day_summary."},
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
			Name:   s.name,
			Type:   "samsung_health_csv",
			Path:   path,
			Rows:   rows,
			SizeMB: float64(info.Size()) / (1024 * 1024),
		})
		manifest.TotalRows += rows
	}

	if info, err := os.Stat(cfg.ATimeLoggerDB); err == nil {
		manifest.Sources = append(manifest.Sources, ImportSource{
			Name:   "atimelogger",
			Type:   "sqlite",
			Path:   cfg.ATimeLoggerDB,
			SizeMB: float64(info.Size()) / (1024 * 1024),
			Note:   "13,094 intervals, 18 categories",
		})
	}

	manifest.CategoryPolicy = defaultCategoryPolicy()
	manifest.EstimatedDBSizeMB = 30

	return manifest, nil
}

type ImportManifest struct {
	CreatedAt         string          `json:"created_at"`
	Sources           []ImportSource  `json:"sources"`
	TotalRows         int             `json:"total_rows"`
	EstimatedDBSizeMB int             `json:"estimated_db_size_mb"`
	CategoryPolicy    *CategoryPolicy `json:"category_policy"`
}

type ImportSource struct {
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Path   string  `json:"path"`
	Rows   int     `json:"rows,omitempty"`
	SizeMB float64 `json:"size_mb"`
	Note   string  `json:"note,omitempty"`
}
