package main

import (
	"os"
	"path/filepath"
)

// --- status ---

type StatusResult struct {
	SamsungHealth *ShealthStatus     `json:"samsung_health"`
	ATimeLogger   *ATimeLoggerStatus `json:"atimelogger"`
}

type ShealthStatus struct {
	Path      string   `json:"path"`
	Available bool     `json:"available"`
	CSVCount  int      `json:"csv_count"`
	AllDirs   []string `json:"all_dirs,omitempty"`
	DateRange []string `json:"date_range,omitempty"`
}

type ATimeLoggerStatus struct {
	Path      string  `json:"path"`
	Available bool    `json:"available"`
	SizeMB    float64 `json:"size_mb,omitempty"`
	Note      string  `json:"note,omitempty"`
}

func cmdStatus(cfg *Config) (interface{}, error) {
	sh := &ShealthStatus{Path: cfg.ShealthDir}
	if cfg.ShealthDir != "" {
		matches, _ := filepath.Glob(filepath.Join(cfg.ShealthDir, "*.csv"))
		sh.CSVCount = len(matches)
		sh.Available = sh.CSVCount > 0
	}
	if len(cfg.ShealthDirs) > 1 {
		sh.AllDirs = cfg.ShealthDirs
	}

	atl := getATimeLoggerStatus(cfg)

	return &StatusResult{
		SamsungHealth: sh,
		ATimeLogger:   &atl,
	}, nil
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
	}
}

// --- today ---

type TodayResult struct {
	Date           string         `json:"date"`
	Steps          int            `json:"steps"`
	SleepHours     float64        `json:"sleep_hours"`
	AvgHR          float64        `json:"avg_hr"`
	StressAvg      float64        `json:"stress_avg"`
	TimeCategories []TimeCategory `json:"time_categories,omitempty"`
}

func cmdToday(cfg *Config) (interface{}, error) {
	result := &TodayResult{
		Date: dateStr(cutoffTime(0).AddDate(0, 0, 1)),
	}

	if steps, err := parseStepRecords(cfg, 1); err == nil && len(steps) > 0 {
		result.Steps = steps[0].Steps
	}
	if sleeps, err := parseSleepRecords(cfg, 2); err == nil && len(sleeps) > 0 {
		result.SleepHours = sleeps[0].DurationHours
	}
	if hearts, err := parseHeartRecords(cfg, 1); err == nil && len(hearts) > 0 {
		result.AvgHR = hearts[0].AvgHR
	}
	if stresses, err := parseStressRecords(cfg, 1); err == nil && len(stresses) > 0 {
		result.StressAvg = stresses[0].AvgScore
	}
	if times, _ := parseTimeRecords(cfg, 1); len(times) > 0 {
		result.TimeCategories = times[0].Categories
	}

	return result, nil
}

// --- sleep ---

func cmdSleep(cfg *Config) (interface{}, error) {
	records, err := parseSleepRecords(cfg, cfg.Days)
	if err != nil {
		return nil, err
	}
	if cfg.Summary {
		return sleepSummary(records), nil
	}
	return records, nil
}

type SleepSummary struct {
	Days          int     `json:"days"`
	Sessions      int     `json:"sessions"`
	AvgDuration   float64 `json:"avg_duration_hours"`
	AvgScore      float64 `json:"avg_score,omitempty"`
	AvgEfficiency float64 `json:"avg_efficiency,omitempty"`
}

func sleepSummary(records []SleepRecord) *SleepSummary {
	if len(records) == 0 {
		return &SleepSummary{}
	}

	s := &SleepSummary{Sessions: len(records)}
	var totalDur, totalScore, totalEff float64
	var scoreCount, effCount int

	for _, r := range records {
		totalDur += r.DurationHours
		if r.SleepScore > 0 {
			totalScore += float64(r.SleepScore)
			scoreCount++
		}
		if r.Efficiency > 0 {
			totalEff += r.Efficiency
			effCount++
		}
	}

	s.AvgDuration = round1(totalDur / float64(len(records)))
	if scoreCount > 0 {
		s.AvgScore = round1(totalScore / float64(scoreCount))
	}
	if effCount > 0 {
		s.AvgEfficiency = round1(totalEff / float64(effCount))
	}

	dates := make(map[string]bool)
	for _, r := range records {
		dates[r.Date] = true
	}
	s.Days = len(dates)

	return s
}

// --- steps/heart/stress/exercise/time ---

func cmdSteps(cfg *Config) (interface{}, error) {
	return parseStepRecords(cfg, cfg.Days)
}

func cmdHeart(cfg *Config) (interface{}, error) {
	return parseHeartRecords(cfg, cfg.Days)
}

func cmdStress(cfg *Config) (interface{}, error) {
	return parseStressRecords(cfg, cfg.Days)
}

func cmdExercise(cfg *Config) (interface{}, error) {
	return parseExerciseRecords(cfg, cfg.Days)
}

func cmdTime(cfg *Config) (interface{}, error) {
	status := getATimeLoggerStatus(cfg)
	if !status.Available {
		return map[string]string{
			"error": "aTimeLogger database not found",
			"path":  cfg.ATimeLoggerDB,
		}, nil
	}

	records, err := parseTimeRecords(cfg, cfg.Days)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return map[string]string{
			"note": "aTimeLogger SQLite parser not yet implemented (Phase 2)",
			"db":   cfg.ATimeLoggerDB,
		}, nil
	}

	return records, nil
}
