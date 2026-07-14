package main

import (
	"fmt"
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
// could not see, and reported there was nothing to see.
//
// A warning on stderr is not enough: the caller of this CLI is an agent, it reads
// stdout, and a JSON list that parses cleanly is a JSON list it will believe. So a
// source that cannot be read fails the command. An answer nobody can trust is
// worth less than no answer.
func csvTimeline(cfg *Config) (interface{}, error) {
	w := cfg.queryWindow()

	sleepRecs, err := parseSleepRecords(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("sleep: %w", err)
	}
	stepRecs, err := parseStepRecords(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("steps: %w", err)
	}
	heartRecs, err := parseHeartRecords(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("heart: %w", err)
	}
	stressRecs, err := parseStressRecords(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("stress: %w", err)
	}
	exerciseRecs, err := parseExerciseRecords(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("exercise: %w", err)
	}
	timeRecs, err := parseTimeRecords(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("time: %w", err)
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
