package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
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

	// Freshness. Staleness must announce itself: the aTimeLogger depth once sat
	// two months behind because the phone export had stalled, and nothing here
	// said so — every query just quietly returned less.
	//
	// Every stream is checked, not just the one that failed last time. The two
	// feeds arrive by different routes (aTimeLogger via database.db3, Samsung via
	// the CSV export) and stall independently.
	LastTimeBlock string `json:"last_time_block,omitempty"`
	LastSleep     string `json:"last_sleep,omitempty"`
	LastSteps     string `json:"last_steps,omitempty"`
	StaleDays     int    `json:"stale_days,omitempty"` // worst stream

	// Never omitempty, never nil. A missing key cannot be told apart from an old
	// binary that never checked; "warnings": [] is the positive claim that the
	// streams were examined and found current. A check that can vanish when it
	// passes is not a check.
	Warnings []string `json:"warnings"`
}

// staleAfterDays is how far a stream may lag today before status calls it stale.
// Records land daily, so a multi-day gap means the import pipeline stopped, not
// that nothing happened.
const staleAfterDays = 3

// dbLastDate runs a query returning a single date string, or "" if unavailable.
func dbLastDate(cfg *Config, query string) string {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return ""
	}
	defer db.Close()

	var date sql.NullString
	if err := db.QueryRow(query).Scan(&date); err != nil {
		return ""
	}
	return date.String
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

	dbSt := &DBStatus{Path: dbPath(cfg), Warnings: []string{}}
	if info, err := os.Stat(dbPath(cfg)); err == nil {
		dbSt.Available = true
		dbSt.SizeMB = float64(info.Size()) / (1024 * 1024)
		dbSt.Mode = "db"
		addDBFreshness(cfg, dbSt)
	} else {
		dbSt.Mode = "csv"
	}

	return &StatusResult{
		SamsungHealth: sh,
		ATimeLogger:   &atl,
		Database:      dbSt,
	}, nil
}

// addDBFreshness reports how current each stream is and warns about any that has
// fallen behind. A stream is checked even if the others are healthy — that is the
// whole point: the Samsung export can stall while aTimeLogger keeps flowing, and
// a check that only watched the feed that broke last time would miss it.
func addDBFreshness(cfg *Config, st *DBStatus) {
	st.LastTimeBlock = dbLastDate(cfg,
		`SELECT DATE(MAX(start_time), 'unixepoch', '+9 hours') FROM atl_interval WHERE is_deleted = 0`)
	st.LastSleep = dbLastDate(cfg, `SELECT DATE(MAX(start_time)) FROM sleep`)
	st.LastSteps = dbLastDate(cfg, `SELECT MAX(date) FROM steps_daily`)

	streams := []struct {
		name, last, remedy string
	}{
		{"aTimeLogger", st.LastTimeBlock, "copy a fresh database.db3 from the phone, then 'lifetract import --exec'"},
		{"Samsung sleep", st.LastSleep, "re-export Samsung Health from the phone, then 'lifetract import --exec'"},
		{"Samsung steps", st.LastSteps, "re-export Samsung Health from the phone, then 'lifetract import --exec'"},
	}

	today := startOfDay(nowKST())
	for _, s := range streams {
		if s.last == "" {
			st.Warnings = append(st.Warnings,
				fmt.Sprintf("%s: no records in DB — has 'lifetract import --exec' ever run?", s.name))
			continue
		}
		last, err := time.ParseInLocation("2006-01-02", s.last, KST)
		if err != nil {
			continue
		}
		behind := int(today.Sub(last).Hours() / 24)
		if behind > st.StaleDays {
			st.StaleDays = behind
		}
		if behind >= staleAfterDays {
			st.Warnings = append(st.Warnings, fmt.Sprintf(
				"%s is %d days behind (newest %s) — the export has stalled; %s",
				s.name, behind, s.last, s.remedy))
		}
	}
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
	Source         string         `json:"source"`               // "db" | "csv" | "db+ha" | "csv+ha"
	HASources      []string       `json:"ha_sources,omitempty"` // fields HA filled in
}

func cmdToday(cfg *Config) (interface{}, error) {
	result := &TodayResult{
		Date: dateStr(cutoffTime(0)),
	}

	if dbExists(cfg) {
		result.Source = "db"
		if steps, err := dbQuerySteps(cfg, daysWindow(1)); err == nil && len(steps) > 0 {
			result.Steps = steps[0].Steps
		}
		if sleeps, err := dbQuerySleep(cfg, daysWindow(2)); err == nil && len(sleeps) > 0 {
			result.SleepHours = sleeps[0].DurationHours
		}
		if hearts, err := dbQueryHeart(cfg, daysWindow(1)); err == nil && len(hearts) > 0 {
			result.AvgHR = hearts[0].AvgHR
		}
		if stresses, err := dbQueryStress(cfg, daysWindow(1)); err == nil && len(stresses) > 0 {
			result.StressAvg = stresses[0].AvgScore
		}
		if times, err := dbQueryTime(cfg, daysWindow(1)); err == nil && len(times) > 0 {
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

	enrichTodayWithHA(cfg, result)

	return result, nil
}

// --- sleep ---

func cmdSleep(cfg *Config) (interface{}, error) {
	var records []SleepRecord
	var err error

	if dbExists(cfg) {
		records, err = dbQuerySleep(cfg, cfg.queryWindow())
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
		return dbQuerySteps(cfg, cfg.queryWindow())
	}
	return parseStepRecords(cfg, cfg.Days)
}

func cmdHeart(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryHeart(cfg, cfg.queryWindow())
	}
	return parseHeartRecords(cfg, cfg.Days)
}

func cmdStress(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryStress(cfg, cfg.queryWindow())
	}
	return parseStressRecords(cfg, cfg.Days)
}

func cmdExercise(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryExercise(cfg, cfg.queryWindow())
	}
	return parseExerciseRecords(cfg, cfg.Days)
}

func cmdTime(cfg *Config) (interface{}, error) {
	// No DB is not an empty window: it means the question could not be asked. An
	// empty list here would report "you tracked nothing" when the truth is "I
	// could not look" — so this fails loudly instead of answering quietly.
	if !dbExists(cfg) {
		if status := getATimeLoggerStatus(cfg); !status.Available {
			return nil, fmt.Errorf("aTimeLogger database not found at %s — cannot answer, this is not an empty result", cfg.ATimeLoggerDB)
		}
		return nil, fmt.Errorf("aTimeLogger data not imported yet — run 'lifetract import --exec'; cannot answer, this is not an empty result")
	}

	records, err := dbQueryTime(cfg, cfg.queryWindow())
	if err != nil {
		return nil, err
	}

	// An empty window is not the same as a stale import. stdout stays a list so a
	// caller can always loop over it; the distinction is spoken on stderr, where
	// it cannot be mistaken for a record.
	if len(records) == 0 {
		msg := "no aTimeLogger blocks in the requested window"
		if last := dbLastDate(cfg, "SELECT DATE(MAX(start_time), 'unixepoch', '+9 hours') FROM atl_interval WHERE is_deleted = 0"); last != "" {
			msg += fmt.Sprintf("; DB holds blocks only through %s — run 'lifetract import --exec' if the phone export is newer", last)
		}
		fmt.Fprintln(os.Stderr, "warning: "+msg)
	}
	return records, nil
}
