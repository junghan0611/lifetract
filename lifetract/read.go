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

	// A source that could not be read is not a day without that measurement.
	sleepRecs, err := parseSleepRecords(cfg, allTime())
	if err != nil {
		return nil, fmt.Errorf("sleep: %w", err)
	}
	stepRecs, err := parseStepRecords(cfg, allTime())
	if err != nil {
		return nil, fmt.Errorf("steps: %w", err)
	}
	heartRecs, err := parseHeartRecords(cfg, allTime())
	if err != nil {
		return nil, fmt.Errorf("heart: %w", err)
	}
	stressRecs, err := parseStressRecords(cfg, allTime())
	if err != nil {
		return nil, fmt.Errorf("stress: %w", err)
	}
	exerciseRecs, err := parseExerciseRecords(cfg, allTime())
	if err != nil {
		return nil, fmt.Errorf("exercise: %w", err)
	}

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

	if isToday(dateS) {
		enrichTimelineEntryWithHA(cfg, entry)
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
	sleepRecs, err := parseSleepRecords(cfg, allTime())
	if err != nil {
		return nil, fmt.Errorf("sleep: %w", err)
	}
	for _, r := range sleepRecs {
		if r.ID == id {
			return r, nil
		}
	}

	exerciseRecs, err := parseExerciseRecords(cfg, allTime())
	if err != nil {
		return nil, fmt.Errorf("exercise: %w", err)
	}
	for _, r := range exerciseRecs {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, fmt.Errorf("no event found for ID %s", id)
}
