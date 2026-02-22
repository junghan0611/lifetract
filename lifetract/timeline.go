package main

import (
	"sort"
)

// TimelineEntry represents a single day's unified life data.
// Date format matches denotecli journal: "YYYY-MM-DD"
// This enables cross-querying: denotecli shows what you wrote,
// lifetract shows what you did/measured on the same day.
type TimelineEntry struct {
	Date     string          `json:"date"`
	Health   *HealthMetrics  `json:"health,omitempty"`
	Time     *TimeMetrics    `json:"time,omitempty"`
	Exercise []ExerciseBrief `json:"exercise,omitempty"`
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
	days := cfg.Days

	// Collect all data sources in parallel-ish
	sleepRecs, _ := parseSleepRecords(cfg, days)
	stepRecs, _ := parseStepRecords(cfg, days)
	heartRecs, _ := parseHeartRecords(cfg, days)
	stressRecs, _ := parseStressRecords(cfg, days)
	exerciseRecs, _ := parseExerciseRecords(cfg, days)
	timeRecs, _ := parseTimeRecords(cfg, days)

	// Build date-indexed maps
	entries := make(map[string]*TimelineEntry)
	ensureEntry := func(date string) *TimelineEntry {
		if e, ok := entries[date]; ok {
			return e
		}
		e := &TimelineEntry{Date: date}
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

	// Steps
	for _, r := range stepRecs {
		h := ensureHealth(r.Date)
		h.Steps = r.Steps
	}

	// Sleep — take first (most recent) session per date
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

	// Heart rate
	for _, r := range heartRecs {
		h := ensureHealth(r.Date)
		h.AvgHR = r.AvgHR
		h.MinHR = r.MinHR
		h.MaxHR = r.MaxHR
	}

	// Stress
	for _, r := range stressRecs {
		h := ensureHealth(r.Date)
		h.StressAvg = r.AvgScore
	}

	// Exercise
	for _, r := range exerciseRecs {
		e := ensureEntry(r.Date)
		e.Exercise = append(e.Exercise, ExerciseBrief{
			Type:     r.Type,
			Minutes:  r.DurationMinutes,
			Calories: r.Calories,
		})
	}

	// Time tracking (aTimeLogger)
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

	// Convert to sorted slice
	var result []TimelineEntry
	for _, e := range entries {
		result = append(result, *e)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date > result[j].Date
	})

	return result, nil
}
