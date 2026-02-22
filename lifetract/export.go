package main

import (
	"encoding/json"
	"os"
	"strings"
)

// cmdExport generates a public-safe version plan.
func cmdExport(cfg *Config) (interface{}, error) {
	return &ExportPlan{
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
	}, nil
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

// --- Category Policy ---

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
