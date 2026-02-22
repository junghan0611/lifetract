package main

import (
	"os"
	"path/filepath"
)

// --- status ---

type StatusResult struct {
	SamsungHealth *ShealthStatus     `json:"samsung_health"`
	ATimeLogger   *ATimeLoggerStatus `json:"atimelogger"`
	Database      *DBStatus          `json:"database,omitempty"`
}

type ShealthStatus struct {
	Path      string   `json:"path"`
	Available bool     `json:"available"`
	CSVCount  int      `json:"csv_count"`
	AllDirs   []string `json:"all_dirs,omitempty"`
}

type ATimeLoggerStatus struct {
	Path      string  `json:"path"`
	Available bool    `json:"available"`
	SizeMB    float64 `json:"size_mb,omitempty"`
	Note      string  `json:"note,omitempty"`
}

type DBStatus struct {
	Path      string  `json:"path"`
	Available bool    `json:"available"`
	SizeMB    float64 `json:"size_mb,omitempty"`
	Mode      string  `json:"mode"` // "db" or "csv"
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

	dbSt := &DBStatus{Path: dbPath(cfg)}
	if info, err := os.Stat(dbPath(cfg)); err == nil {
		dbSt.Available = true
		dbSt.SizeMB = float64(info.Size()) / (1024 * 1024)
		dbSt.Mode = "db"
	} else {
		dbSt.Mode = "csv"
	}

	return &StatusResult{
		SamsungHealth: sh,
		ATimeLogger:   &atl,
		Database:      dbSt,
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
	Source         string         `json:"source"` // "db" or "csv"
}

func cmdToday(cfg *Config) (interface{}, error) {
	result := &TodayResult{
		Date: dateStr(cutoffTime(0).AddDate(0, 0, 1)),
	}

	if dbExists(cfg) {
		result.Source = "db"
		if steps, err := dbQuerySteps(cfg, 1); err == nil && len(steps) > 0 {
			result.Steps = steps[0].Steps
		}
		if sleeps, err := dbQuerySleep(cfg, 2); err == nil && len(sleeps) > 0 {
			result.SleepHours = sleeps[0].DurationHours
		}
		if hearts, err := dbQueryHeart(cfg, 1); err == nil && len(hearts) > 0 {
			result.AvgHR = hearts[0].AvgHR
		}
		if stresses, err := dbQueryStress(cfg, 1); err == nil && len(stresses) > 0 {
			result.StressAvg = stresses[0].AvgScore
		}
		if times, err := dbQueryTime(cfg, 1); err == nil && len(times) > 0 {
			result.TimeCategories = times[0].Categories
		}
	} else {
		result.Source = "csv"
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
	}

	return result, nil
}

// --- sleep ---

func cmdSleep(cfg *Config) (interface{}, error) {
	var records []SleepRecord
	var err error

	if dbExists(cfg) {
		records, err = dbQuerySleep(cfg, cfg.Days)
	} else {
		records, err = parseSleepRecords(cfg, cfg.Days)
	}
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
	if dbExists(cfg) {
		return dbQuerySteps(cfg, cfg.Days)
	}
	return parseStepRecords(cfg, cfg.Days)
}

func cmdHeart(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryHeart(cfg, cfg.Days)
	}
	return parseHeartRecords(cfg, cfg.Days)
}

func cmdStress(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryStress(cfg, cfg.Days)
	}
	return parseStressRecords(cfg, cfg.Days)
}

func cmdExercise(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryExercise(cfg, cfg.Days)
	}
	return parseExerciseRecords(cfg, cfg.Days)
}

func cmdTime(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		records, err := dbQueryTime(cfg, cfg.Days)
		if err != nil {
			return nil, err
		}
		if len(records) == 0 {
			return map[string]string{"note": "no aTimeLogger data in DB for the given period"}, nil
		}
		return records, nil
	}

	status := getATimeLoggerStatus(cfg)
	if !status.Available {
		return map[string]string{
			"error": "aTimeLogger database not found",
			"path":  cfg.ATimeLoggerDB,
		}, nil
	}

	return map[string]string{
		"note": "run 'lifetract import --exec' first to enable DB queries",
	}, nil
}
