package main

import (
	"fmt"
	"strings"
	"time"
)

// cmdRead looks up data by Denote ID.
// Accepts:
//   - Day ID "YYYYMMDDT000000" → returns that day's timeline entry
//   - Event ID "YYYYMMDDTHHMMSS" → returns matching sleep/exercise event
//   - Short date "YYYY-MM-DD" → converts to day ID
func cmdRead(cfg *Config) (interface{}, error) {
	id := cfg.ReadID
	if id == "" {
		return nil, fmt.Errorf("usage: lifetract read <denote-id>")
	}

	// Normalize: "2025-10-04" → "20251004T000000"
	if len(id) == 10 && id[4] == '-' {
		id = denoteDayID(id)
	}

	t, err := parseDenoteID(id)
	if err != nil {
		return nil, fmt.Errorf("invalid denote ID %q: %w", id, err)
	}

	isDayID := strings.HasSuffix(id, "T000000")

	if dbExists(cfg) {
		if isDayID {
			return dbQueryDay(cfg, t)
		}
		return dbQueryEvent(cfg, t, id)
	}

	// CSV fallback
	if isDayID {
		return csvReadDay(cfg, t)
	}
	return csvReadEvent(cfg, t, id)
}

// csvReadDay returns the full timeline entry for a specific day (CSV mode).
func csvReadDay(cfg *Config, day time.Time) (interface{}, error) {
	dateS := dateStr(day)
	dayID := denoteDayID(dateS)

	origDays := cfg.Days
	cfg.Days = 9999

	sleepRecs, _ := parseSleepRecords(cfg, cfg.Days)
	stepRecs, _ := parseStepRecords(cfg, cfg.Days)
	heartRecs, _ := parseHeartRecords(cfg, cfg.Days)
	stressRecs, _ := parseStressRecords(cfg, cfg.Days)
	exerciseRecs, _ := parseExerciseRecords(cfg, cfg.Days)

	cfg.Days = origDays

	entry := &TimelineEntry{ID: dayID, Date: dateS}

	for _, r := range stepRecs {
		if r.Date == dateS {
			ensureTimelineHealth(entry).Steps = r.Steps
			break
		}
	}

	var sleepSessions []SleepRecord
	for _, r := range sleepRecs {
		if r.Date == dateS {
			sleepSessions = append(sleepSessions, r)
		}
	}
	if len(sleepSessions) > 0 {
		h := ensureTimelineHealth(entry)
		h.SleepHours = sleepSessions[0].DurationHours
		h.SleepScore = sleepSessions[0].SleepScore
	}

	for _, r := range heartRecs {
		if r.Date == dateS {
			h := ensureTimelineHealth(entry)
			h.AvgHR = r.AvgHR
			h.MinHR = r.MinHR
			h.MaxHR = r.MaxHR
			break
		}
	}

	for _, r := range stressRecs {
		if r.Date == dateS {
			ensureTimelineHealth(entry).StressAvg = r.AvgScore
			break
		}
	}

	for _, r := range exerciseRecs {
		if r.Date == dateS {
			entry.Exercise = append(entry.Exercise, ExerciseBrief{
				Type:     r.Type,
				Minutes:  r.DurationMinutes,
				Calories: r.Calories,
			})
		}
	}

	type DayDetail struct {
		*TimelineEntry
		SleepSessions []SleepRecord `json:"sleep_sessions,omitempty"`
	}
	if len(sleepSessions) > 0 {
		return &DayDetail{TimelineEntry: entry, SleepSessions: sleepSessions}, nil
	}
	return entry, nil
}

// csvReadEvent finds a specific event by exact Denote ID (CSV mode).
func csvReadEvent(cfg *Config, t time.Time, id string) (interface{}, error) {
	origDays := cfg.Days
	cfg.Days = 9999

	sleepRecs, _ := parseSleepRecords(cfg, cfg.Days)
	for _, r := range sleepRecs {
		if r.ID == id {
			cfg.Days = origDays
			return r, nil
		}
	}

	exerciseRecs, _ := parseExerciseRecords(cfg, cfg.Days)
	for _, r := range exerciseRecs {
		if r.ID == id {
			cfg.Days = origDays
			return r, nil
		}
	}

	cfg.Days = origDays
	return nil, fmt.Errorf("no event found for ID %s", id)
}
