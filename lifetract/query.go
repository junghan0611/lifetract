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

	// FreshnessChecked says whether the streams were examined at all. Without it,
	// "warnings": [] means two different things — "examined, all current" and "not
	// examined, because there is no DB" — and the second one wearing the first
	// one's clothes is exactly the disease: a check that reports itself passing
	// when it never ran. Read this before you read Warnings.
	FreshnessChecked bool `json:"freshness_checked"`

	// Never omitempty, never nil. A missing key cannot be told apart from an old
	// binary that never checked. [] is a claim, and it is only true when
	// freshness_checked is true.
	Warnings []string `json:"warnings"`
}

// staleAfterDays is how far a stream may lag today before status calls it stale.
// Records land daily, so a multi-day gap means the import pipeline stopped, not
// that nothing happened.
const staleAfterDays = 3

// dbLastDate runs a query returning a single date string.
//
// The error is returned, not folded into "". A corrupt file, a dropped table and
// a stream that genuinely holds no rows all used to come back as the empty
// string, and the caller reported all three as "no records in DB" — the freshness
// check could fail and still look like a clean answer.
func dbLastDate(cfg *Config, query string) (string, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return "", err
	}
	defer db.Close()

	var date sql.NullString
	if err := db.QueryRow(query).Scan(&date); err != nil {
		return "", err
	}
	return date.String, nil
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
		// No DB, so nothing was examined. Saying so is the point: an empty warnings
		// list here used to read as a clean bill of health for streams no one looked
		// at.
		dbSt.Mode = "csv"
		dbSt.Warnings = append(dbSt.Warnings,
			"no DB — freshness was not checked; run 'lifetract import --exec'")
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
	streams := []struct {
		name, query, remedy string
		into                *string
	}{
		{"aTimeLogger",
			`SELECT DATE(MAX(start_time), 'unixepoch', '+9 hours') FROM atl_interval WHERE is_deleted = 0`,
			"copy a fresh database.db3 from the phone, then 'lifetract import --exec'",
			&st.LastTimeBlock},
		{"Samsung sleep",
			`SELECT DATE(MAX(start_time)) FROM sleep`,
			"re-export Samsung Health from the phone, then 'lifetract import --exec'",
			&st.LastSleep},
		{"Samsung steps",
			`SELECT MAX(date) FROM steps_daily`,
			"re-export Samsung Health from the phone, then 'lifetract import --exec'",
			&st.LastSteps},
	}

	today := startOfDay(nowKST())
	checked := true

	for _, s := range streams {
		last, err := dbLastDate(cfg, s.query)
		if err != nil {
			// The check itself failed. That is not "the stream is empty", and it is
			// not "the stream is current" — it is "I could not look", and it has to
			// be the loudest of the three.
			checked = false
			st.Warnings = append(st.Warnings,
				fmt.Sprintf("%s: freshness could not be read — %v", s.name, err))
			continue
		}
		*s.into = last

		if last == "" {
			st.Warnings = append(st.Warnings,
				fmt.Sprintf("%s: no records in DB — has 'lifetract import --exec' ever run?", s.name))
			continue
		}

		parsed, err := time.ParseInLocation("2006-01-02", last, KST)
		if err != nil {
			// A date the DB holds and we cannot read is a broken record, not a fresh
			// one. It used to `continue` in silence and leave the stream looking fine.
			checked = false
			st.Warnings = append(st.Warnings,
				fmt.Sprintf("%s: newest date %q is unreadable — the DB is malformed", s.name, last))
			continue
		}

		behind := int(today.Sub(parsed).Hours() / 24)
		if behind < 0 {
			// A record dated after today is a clock that is wrong somewhere, and a
			// negative "days behind" sailed through the freshness check as the
			// freshest thing in the DB.
			st.Warnings = append(st.Warnings, fmt.Sprintf(
				"%s: newest record is dated %s, in the future — the source clock is wrong", s.name, last))
			continue
		}
		if behind > st.StaleDays {
			st.StaleDays = behind
		}
		if behind >= staleAfterDays {
			st.Warnings = append(st.Warnings, fmt.Sprintf(
				"%s is %d days behind (newest %s) — the export has stalled; %s",
				s.name, behind, last, s.remedy))
		}
	}

	st.FreshnessChecked = checked
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

	// A source that fails to answer is not a source that answered zero. Every one of
	// these used to be `err == nil &&` — so a broken table left the field at its zero
	// value and today reported 0 steps, 0 hours of sleep, a resting heart rate of
	// nothing. The numbers were indistinguishable from a day spent lying still, and
	// they were going into a journal as fact.
	if dbExists(cfg) {
		result.Source = "db"

		steps, err := dbQuerySteps(cfg, daysWindow(1))
		if err != nil {
			return nil, fmt.Errorf("steps: %w", err)
		}
		if len(steps) > 0 {
			result.Steps = steps[0].Steps
		}

		sleeps, err := dbQuerySleep(cfg, daysWindow(2))
		if err != nil {
			return nil, fmt.Errorf("sleep: %w", err)
		}
		if len(sleeps) > 0 {
			result.SleepHours = sleeps[0].DurationHours
		}

		hearts, err := dbQueryHeart(cfg, daysWindow(1))
		if err != nil {
			return nil, fmt.Errorf("heart: %w", err)
		}
		if len(hearts) > 0 {
			result.AvgHR = hearts[0].AvgHR
		}

		stresses, err := dbQueryStress(cfg, daysWindow(1))
		if err != nil {
			return nil, fmt.Errorf("stress: %w", err)
		}
		if len(stresses) > 0 {
			result.StressAvg = stresses[0].AvgScore
		}

		times, err := dbQueryTime(cfg, daysWindow(1))
		if err != nil {
			return nil, fmt.Errorf("time: %w", err)
		}
		if len(times) > 0 {
			result.TimeCategories = times[0].Categories
		}
	} else {
		result.Source = "csv"

		steps, err := parseStepRecords(cfg, daysWindow(1))
		if err != nil {
			return nil, fmt.Errorf("steps: %w", err)
		}
		if len(steps) > 0 {
			result.Steps = steps[0].Steps
		}

		sleeps, err := parseSleepRecords(cfg, daysWindow(2))
		if err != nil {
			return nil, fmt.Errorf("sleep: %w", err)
		}
		if len(sleeps) > 0 {
			result.SleepHours = sleeps[0].DurationHours
		}

		hearts, err := parseHeartRecords(cfg, daysWindow(1))
		if err != nil {
			return nil, fmt.Errorf("heart: %w", err)
		}
		if len(hearts) > 0 {
			result.AvgHR = hearts[0].AvgHR
		}

		stresses, err := parseStressRecords(cfg, daysWindow(1))
		if err != nil {
			return nil, fmt.Errorf("stress: %w", err)
		}
		if len(stresses) > 0 {
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
		records, err = parseSleepRecords(cfg, cfg.queryWindow())
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
	return parseStepRecords(cfg, cfg.queryWindow())
}

func cmdHeart(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryHeart(cfg, cfg.queryWindow())
	}
	return parseHeartRecords(cfg, cfg.queryWindow())
}

func cmdStress(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryStress(cfg, cfg.queryWindow())
	}
	return parseStressRecords(cfg, cfg.queryWindow())
}

func cmdExercise(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryExercise(cfg, cfg.queryWindow())
	}
	return parseExerciseRecords(cfg, cfg.queryWindow())
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
		if last, err := dbLastDate(cfg, "SELECT DATE(MAX(start_time), 'unixepoch', '+9 hours') FROM atl_interval WHERE is_deleted = 0"); err == nil && last != "" {
			msg += fmt.Sprintf("; DB holds blocks only through %s — run 'lifetract import --exec' if the phone export is newer", last)
		}
		fmt.Fprintln(os.Stderr, "warning: "+msg)
	}
	return records, nil
}
