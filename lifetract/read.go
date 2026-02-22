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

	// Parse the ID to get a date for filtering
	t, err := parseDenoteID(id)
	if err != nil {
		return nil, fmt.Errorf("invalid denote ID %q: %w", id, err)
	}

	isDayID := strings.HasSuffix(id, "T000000")

	if isDayID {
		return readDay(cfg, t)
	}
	return readEvent(cfg, t, id)
}

// parseDenoteID → helpers.go

// readDay returns the full timeline entry for a specific day.
func readDay(cfg *Config, day time.Time) (interface{}, error) {
	dateS := dateStr(day)
	dayID := denoteDayID(dateS)

	// Load all data for a wide window to ensure we catch the day
	origDays := cfg.Days
	cfg.Days = 9999 // load all

	sleepRecs, _ := parseSleepRecords(cfg, cfg.Days)
	stepRecs, _ := parseStepRecords(cfg, cfg.Days)
	heartRecs, _ := parseHeartRecords(cfg, cfg.Days)
	stressRecs, _ := parseStressRecords(cfg, cfg.Days)
	exerciseRecs, _ := parseExerciseRecords(cfg, cfg.Days)

	cfg.Days = origDays

	entry := &TimelineEntry{ID: dayID, Date: dateS}

	// Steps
	for _, r := range stepRecs {
		if r.Date == dateS {
			if entry.Health == nil {
				entry.Health = &HealthMetrics{}
			}
			entry.Health.Steps = r.Steps
			break
		}
	}

	// Sleep — collect all sessions for the day
	var sleepSessions []SleepRecord
	for _, r := range sleepRecs {
		if r.Date == dateS {
			sleepSessions = append(sleepSessions, r)
		}
	}
	if len(sleepSessions) > 0 {
		if entry.Health == nil {
			entry.Health = &HealthMetrics{}
		}
		entry.Health.SleepHours = sleepSessions[0].DurationHours
		entry.Health.SleepScore = sleepSessions[0].SleepScore
	}

	// Heart rate
	for _, r := range heartRecs {
		if r.Date == dateS {
			if entry.Health == nil {
				entry.Health = &HealthMetrics{}
			}
			entry.Health.AvgHR = r.AvgHR
			entry.Health.MinHR = r.MinHR
			entry.Health.MaxHR = r.MaxHR
			break
		}
	}

	// Stress
	for _, r := range stressRecs {
		if r.Date == dateS {
			if entry.Health == nil {
				entry.Health = &HealthMetrics{}
			}
			entry.Health.StressAvg = r.AvgScore
			break
		}
	}

	// Exercise
	for _, r := range exerciseRecs {
		if r.Date == dateS {
			entry.Exercise = append(entry.Exercise, ExerciseBrief{
				Type:     r.Type,
				Minutes:  r.DurationMinutes,
				Calories: r.Calories,
			})
		}
	}

	// Also include sleep sessions as linked events
	type DayDetail struct {
		*TimelineEntry
		SleepSessions []SleepRecord `json:"sleep_sessions,omitempty"`
	}

	if len(sleepSessions) > 0 {
		return &DayDetail{
			TimelineEntry: entry,
			SleepSessions: sleepSessions,
		}, nil
	}

	return entry, nil
}

// readEvent finds a specific event (sleep/exercise) by exact Denote ID.
func readEvent(cfg *Config, t time.Time, id string) (interface{}, error) {
	origDays := cfg.Days
	cfg.Days = 9999

	// Try sleep
	sleepRecs, _ := parseSleepRecords(cfg, cfg.Days)
	for _, r := range sleepRecs {
		if r.ID == id {
			cfg.Days = origDays
			return r, nil
		}
	}

	// Try exercise
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
