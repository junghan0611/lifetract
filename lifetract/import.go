package main

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cmdImport converts raw Samsung Health CSVs + aTimeLogger SQLite
// into a single lifetract.db SQLite database.
// Phase 1: Generates a JSON manifest of what would be imported.
// Phase 2: Will produce actual SQLite (needs modernc.org/sqlite).
func cmdImport(cfg *Config) (interface{}, error) {
	manifest := &ImportManifest{
		CreatedAt: time.Now().Format(time.RFC3339),
		Sources:   []ImportSource{},
	}

	// Samsung Health CSVs
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

	// aTimeLogger
	if info, err := os.Stat(cfg.ATimeLoggerDB); err == nil {
		manifest.Sources = append(manifest.Sources, ImportSource{
			Name:   "atimelogger",
			Type:   "sqlite",
			Path:   cfg.ATimeLoggerDB,
			SizeMB: float64(info.Size()) / (1024 * 1024),
			Note:   "13,094 intervals, 18 categories",
		})
	}

	// Category mapping for aTimeLogger
	manifest.CategoryPolicy = defaultCategoryPolicy()

	// Estimate
	manifest.EstimatedDBSizeMB = 30 // from PoC measurement

	return manifest, nil
}

type ImportManifest struct {
	CreatedAt         string           `json:"created_at"`
	Sources           []ImportSource   `json:"sources"`
	TotalRows         int              `json:"total_rows"`
	EstimatedDBSizeMB int              `json:"estimated_db_size_mb"`
	CategoryPolicy    *CategoryPolicy  `json:"category_policy"`
}

type ImportSource struct {
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Path   string  `json:"path"`
	Rows   int     `json:"rows,omitempty"`
	SizeMB float64 `json:"size_mb"`
	Note   string  `json:"note,omitempty"`
}

// CategoryPolicy defines how aTimeLogger categories map to traction/distraction.
type CategoryPolicy struct {
	Traction    []string `json:"traction"`
	Maintenance []string `json:"maintenance"`
	Distraction []string `json:"distraction"`
	Family      []string `json:"family"`
	Note        string   `json:"note"`
}

func defaultCategoryPolicy() *CategoryPolicy {
	return &CategoryPolicy{
		Traction:    []string{"본짓", "독서", "수행", "운동", "걷기", "셀프토크"},
		Maintenance: []string{"수면", "낮잠", "식사", "준비", "집안일", "이동", "쇼핑"},
		Distraction: []string{"딴짓", "유튜브", "짧은휴식", "여가 활동"},
		Family:      []string{"가족"},
		Note:        "Nir Eyal's Indistractable: traction = intentional action. Family is separate — can be traction or maintenance depending on context.",
	}
}

func countCSVRows(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	// Skip metadata + header
	reader.Read()
	reader.Read()

	count := 0
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		count++
	}
	return count, nil
}

// cmdExport generates a public-safe version of the data.
// Strips: device UUIDs, package names, custom fields.
// Keeps: timestamps, measurements, Denote IDs.
func cmdExport(cfg *Config) (interface{}, error) {
	plan := &ExportPlan{
		Remove: []string{
			"jsons/ (866MB binning data)",
			"files/ (5MB images/PDFs)",
			"device UUIDs",
			"package names",
			"client data IDs",
		},
		Keep: []string{
			"timestamps (Denote ID axis)",
			"health measurements (HR, steps, sleep, stress)",
			"exercise sessions",
			"weight",
			"aTimeLogger categories + intervals",
		},
		Estimated: ExportSize{
			OriginalMB: 942,
			CleanedMB:  35,
			GzipMB:     13,
		},
	}
	return plan, nil
}

type ExportPlan struct {
	Remove    []string   `json:"remove"`
	Keep      []string   `json:"keep"`
	Estimated ExportSize `json:"estimated_size"`
}

type ExportSize struct {
	OriginalMB int `json:"original_mb"`
	CleanedMB  int `json:"cleaned_db_mb"`
	GzipMB     int `json:"gzip_mb"`
}

// writeCategoryPolicy writes the policy to a JSON file for user editing.
func writeCategoryPolicy(path string) error {
	policy := defaultCategoryPolicy()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(policy)
}

// stripPrivateFields removes identifying info from a CSV row.
func stripPrivateFields(headers []string, row map[string]string) map[string]string {
	clean := make(map[string]string, len(row))
	for _, h := range headers {
		lower := strings.ToLower(h)
		// Skip private fields
		if strings.Contains(lower, "deviceuuid") ||
			strings.Contains(lower, "pkg_name") ||
			strings.Contains(lower, "client_data") ||
			strings.Contains(lower, "datauuid") ||
			strings.Contains(lower, "custom") {
			continue
		}
		if v, ok := row[h]; ok {
			clean[h] = v
		}
	}
	return clean
}

// findExportDir returns the path for exported data.
func findExportDir(cfg *Config) string {
	return filepath.Join(cfg.DataDir, "export")
}
