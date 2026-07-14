package main

import (
	"fmt"
	"os"
	"sort"
)

// TimelineEntry represents a single day's unified life data.
type TimelineEntry struct {
	ID        string          `json:"id"`
	Date      string          `json:"date"`
	Health    *HealthMetrics  `json:"health,omitempty"`
	Time      *TimeMetrics    `json:"time,omitempty"`
	Exercise  []ExerciseBrief `json:"exercise,omitempty"`
	HASources []string        `json:"ha_sources,omitempty"` // today-only, fields filled from HA
}

type HealthMetrics struct {
	Steps      int     `json:"steps,omitempty"`
	SleepHours float64 `json:"sleep_hours,omitempty"`
	SleepScore int     `json:"sleep_score,omitempty"`
	AvgHR      float64 `json:"avg_hr,omitempty"`
	MinHR      int     `json:"min_hr,omitempty"`
	MaxHR      int     `json:"max_hr,omitempty"`
	StressAvg  float64 `json:"stress_avg,omitempty"`
}

type TimeMetrics struct {
	Categories []TimeCategory `json:"categories,omitempty"`
	TotalMin   float64        `json:"total_min,omitempty"`
}

type ExerciseBrief struct {
	Type     string  `json:"type"`
	Minutes  float64 `json:"minutes"`
	Calories float64 `json:"calories,omitempty"`
}

func cmdTimeline(cfg *Config) (interface{}, error) {
	if dbExists(cfg) {
		return dbQueryTimeline(cfg, cfg.queryWindow())
	}
	return csvTimeline(cfg)
}

// csvTimeline builds the timeline from the CSV exports (fallback).
//
// Every source error used to go into `_`. A timeline missing three of its six
// feeds came back looking exactly like a timeline of three quiet days — the tool
// could not see, and reported that there was nothing to see. The rows still go to
// stdout (a caller must always get a list), but a source that did not answer is
// named on stderr instead of being folded into the silence.
func csvTimeline(cfg *Config) (interface{}, error) {
	w := cfg.queryWindow()

	sleepRecs, errSleep := parseSleepRecords(cfg, w)
	stepRecs, errSteps := parseStepRecords(cfg, w)
	heartRecs, errHeart := parseHeartRecords(cfg, w)
	stressRecs, errStress := parseStressRecords(cfg, w)
	exerciseRecs, errExercise := parseExerciseRecords(cfg, w)
	timeRecs, errTime := parseTimeRecords(cfg, w)

	for _, src := range []struct {
		name string
		err  error
	}{
		{"sleep", errSleep}, {"steps", errSteps}, {"heart", errHeart},
		{"stress", errStress}, {"exercise", errExercise}, {"time", errTime},
	} {
		if src.err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s missing from this timeline — %v\n", src.name, src.err)
		}
	}

	return buildTimeline(stepRecs, sleepRecs, heartRecs, stressRecs, exerciseRecs, timeRecs), nil
}

// buildTimeline assembles timeline entries from parsed records.
func buildTimeline(stepRecs []StepRecord, sleepRecs []SleepRecord,
	heartRecs []HeartRecord, stressRecs []StressRecord,
	exerciseRecs []ExerciseRecord, timeRecs []TimeRecord) []TimelineEntry {

	entries := make(map[string]*TimelineEntry)
	ensureEntry := func(date string) *TimelineEntry {
		if e, ok := entries[date]; ok {
			return e
		}
		e := &TimelineEntry{ID: denoteDayID(date), Date: date}
		entries[date] = e
		return e
	}
	ensureHealth := func(date string) *HealthMetrics {
		e := ensureEntry(date)
		if e.Health == nil {
			e.Health = &HealthMetrics{}
		}
		return e.Health
	}

	for _, r := range stepRecs {
		ensureHealth(r.Date).Steps = r.Steps
	}

	sleepByDate := make(map[string]SleepRecord)
	for _, r := range sleepRecs {
		if _, exists := sleepByDate[r.Date]; !exists {
			sleepByDate[r.Date] = r
		}
	}
	for date, r := range sleepByDate {
		h := ensureHealth(date)
		h.SleepHours = r.DurationHours
		h.SleepScore = r.SleepScore
	}

	for _, r := range heartRecs {
		h := ensureHealth(r.Date)
		h.AvgHR = r.AvgHR
		h.MinHR = r.MinHR
		h.MaxHR = r.MaxHR
	}

	for _, r := range stressRecs {
		ensureHealth(r.Date).StressAvg = r.AvgScore
	}

	for _, r := range exerciseRecs {
		e := ensureEntry(r.Date)
		e.Exercise = append(e.Exercise, ExerciseBrief{
			Type:     r.Type,
			Minutes:  r.DurationMinutes,
			Calories: r.Calories,
		})
	}

	for _, r := range timeRecs {
		e := ensureEntry(r.Date)
		totalMin := 0.0
		for _, c := range r.Categories {
			totalMin += c.Minutes
		}
		e.Time = &TimeMetrics{
			Categories: r.Categories,
			TotalMin:   round1(totalMin),
		}
	}

	var result []TimelineEntry
	for _, e := range entries {
		result = append(result, *e)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date > result[j].Date
	})
	return result
}
