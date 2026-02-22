package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// --- CSV reader ---

// shealthReadCSV reads a Samsung Health CSV file and returns header + records.
// Samsung Health CSVs have: line 1 = metadata, line 2 = headers, line 3+ = data.
func shealthReadCSV(path string) ([]string, []map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	// Line 1: metadata (skip)
	if _, err := reader.Read(); err != nil {
		return nil, nil, fmt.Errorf("reading metadata line: %w", err)
	}

	// Line 2: headers
	headers, err := reader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("reading headers: %w", err)
	}
	if len(headers) > 0 {
		headers[0] = stripBOM(headers[0])
	}

	// Read all records
	var records []map[string]string
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		rec := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(row) {
				rec[h] = strings.TrimSpace(row[i])
			}
		}
		records = append(records, rec)
	}

	return headers, records, nil
}

// countCSVRows counts data rows in a Samsung Health CSV (skips metadata + header).
func countCSVRows(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	reader.Read() // skip metadata
	reader.Read() // skip header

	count := 0
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		count++
	}
	return count, nil
}

// --- Record types ---

type SleepRecord struct {
	ID            string       `json:"id"`
	Date          string       `json:"date"`
	Start         string       `json:"start"`
	End           string       `json:"end"`
	DurationHours float64      `json:"duration_hours"`
	SleepScore    int          `json:"sleep_score,omitempty"`
	Efficiency    float64      `json:"efficiency,omitempty"`
	Stages        *SleepStages `json:"stages,omitempty"`
}

type SleepStages struct {
	DeepMin  float64 `json:"deep_min"`
	LightMin float64 `json:"light_min"`
	RemMin   float64 `json:"rem_min"`
	AwakeMin float64 `json:"awake_min"`
}

type StepRecord struct {
	ID    string `json:"id"`
	Date  string `json:"date"`
	Steps int    `json:"steps"`
}

type HeartRecord struct {
	ID      string  `json:"id"`
	Date    string  `json:"date"`
	AvgHR   float64 `json:"avg_hr"`
	MinHR   int     `json:"min_hr"`
	MaxHR   int     `json:"max_hr"`
	Samples int     `json:"samples"`
}

type StressRecord struct {
	ID       string  `json:"id"`
	Date     string  `json:"date"`
	AvgScore float64 `json:"avg_score"`
	MinScore int     `json:"min_score"`
	MaxScore int     `json:"max_score"`
	Samples  int     `json:"samples"`
}

type ExerciseRecord struct {
	ID              string  `json:"id"`
	Date            string  `json:"date"`
	Type            string  `json:"type"`
	DurationMinutes float64 `json:"duration_minutes"`
	Calories        float64 `json:"calories,omitempty"`
	AvgHR           float64 `json:"avg_hr,omitempty"`
	MaxHR           float64 `json:"max_hr,omitempty"`
}

type TimeRecord struct {
	Date       string         `json:"date"`
	Categories []TimeCategory `json:"categories"`
}

type TimeCategory struct {
	Name    string  `json:"name"`
	Minutes float64 `json:"minutes"`
}

// Samsung Health exercise type codes
var exerciseTypes = map[string]string{
	"0":     "Other",
	"1001":  "Walking",
	"1002":  "Running",
	"1003":  "Cycling",
	"1004":  "Hiking",
	"1005":  "Swimming",
	"1006":  "Elliptical",
	"1007":  "Rowing",
	"10001": "Strength Training",
	"11007": "Walking (Legacy)",
	"11008": "Running (Legacy)",
	"11009": "Cycling (Legacy)",
	"11010": "Hiking (Legacy)",
	"11014": "Swimming (Legacy)",
	"13001": "Stretching",
	"15001": "Yoga",
	"15003": "Pilates",
	"15005": "Meditation",
}

// --- CSV parsers ---

func parseSleepRecords(cfg *Config, days int) ([]SleepRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.sleep.")
	if path == "" {
		return nil, fmt.Errorf("sleep CSV not found in %s", cfg.ShealthDir)
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

	cutoff := cutoffTime(days)
	var results []SleepRecord
	stageMap := loadSleepStages(cfg)

	for _, rec := range records {
		startStr := rec["com.samsung.health.sleep.start_time"]
		endStr := rec["com.samsung.health.sleep.end_time"]
		if startStr == "" || endStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}
		if start.Before(cutoff) {
			continue
		}

		end, err := parseShealthTime(endStr)
		if err != nil {
			continue
		}

		duration := end.Sub(start).Hours()
		if duration <= 0 || duration > 24 {
			continue
		}

		sr := SleepRecord{
			ID:            denoteID(start),
			Date:          dateStr(start),
			Start:         timeStr(start),
			End:           timeStr(end),
			DurationHours: math.Round(duration*10) / 10,
		}

		if s := rec["sleep_score"]; s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				sr.SleepScore = n
			}
		}
		if s := rec["efficiency"]; s != "" {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				sr.Efficiency = math.Round(f*10) / 10
			}
		}

		uuid := rec["com.samsung.health.sleep.datauuid"]
		if stages, ok := stageMap[uuid]; ok {
			sr.Stages = stages
		}

		if sr.Stages == nil {
			stages := &SleepStages{}
			hasStages := false
			if s := rec["total_light_duration"]; s != "" {
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					stages.LightMin = f
					hasStages = true
				}
			}
			if s := rec["total_rem_duration"]; s != "" {
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					stages.RemMin = f
					hasStages = true
				}
			}
			if hasStages {
				sr.Stages = stages
			}
		}

		results = append(results, sr)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Date > results[j].Date
	})
	return results, nil
}

func loadSleepStages(cfg *Config) map[string]*SleepStages {
	path := cfg.shealthCSV("com.samsung.health.sleep_stage.")
	if path == "" {
		return nil
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil
	}

	type stageEntry struct {
		stage    int
		duration float64
	}
	groups := make(map[string][]stageEntry)

	for _, rec := range records {
		sleepID := rec["sleep_id"]
		if sleepID == "" {
			continue
		}
		startStr := rec["start_time"]
		endStr := rec["end_time"]
		stageStr := rec["stage"]
		if startStr == "" || endStr == "" || stageStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}
		end, err := parseShealthTime(endStr)
		if err != nil {
			continue
		}
		stage, err := strconv.Atoi(stageStr)
		if err != nil {
			continue
		}

		dur := end.Sub(start).Minutes()
		groups[sleepID] = append(groups[sleepID], stageEntry{stage: stage, duration: dur})
	}

	result := make(map[string]*SleepStages, len(groups))
	for id, entries := range groups {
		s := &SleepStages{}
		for _, e := range entries {
			switch e.stage {
			case 40001:
				s.AwakeMin += e.duration
			case 40002:
				s.LightMin += e.duration
			case 40003:
				s.DeepMin += e.duration
			case 40004:
				s.RemMin += e.duration
			}
		}
		s.DeepMin = math.Round(s.DeepMin*10) / 10
		s.LightMin = math.Round(s.LightMin*10) / 10
		s.RemMin = math.Round(s.RemMin*10) / 10
		s.AwakeMin = math.Round(s.AwakeMin*10) / 10
		result[id] = s
	}
	return result
}

func parseStepRecords(cfg *Config, days int) ([]StepRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.step_daily_trend.")
	if path == "" {
		return parseStepRecordsFromPedometer(cfg, days)
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

	cutoff := cutoffTime(days)
	dailySteps := make(map[string]int)

	for _, rec := range records {
		countStr := rec["count"]
		if countStr == "" {
			continue
		}

		dayTimeStr := rec["day_time"]
		if dayTimeStr == "" {
			ctStr := rec["create_time"]
			if ctStr == "" {
				continue
			}
			ct, err := parseShealthTime(ctStr)
			if err != nil {
				continue
			}
			if ct.Before(cutoff) {
				continue
			}
			date := dateStr(ct)
			count, _ := strconv.Atoi(countStr)
			dailySteps[date] += count
			continue
		}

		ms, err := strconv.ParseInt(dayTimeStr, 10, 64)
		if err != nil {
			continue
		}
		t := time.Unix(ms/1000, 0)
		if t.Before(cutoff) {
			continue
		}

		date := dateStr(t)
		count, _ := strconv.Atoi(countStr)
		dailySteps[date] += count
	}

	return stepsMapToSorted(dailySteps), nil
}

func parseStepRecordsFromPedometer(cfg *Config, days int) ([]StepRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.tracker.pedometer_step_count.")
	if path == "" {
		return nil, fmt.Errorf("step count CSV not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

	cutoff := cutoffTime(days)
	dailySteps := make(map[string]int)

	for _, rec := range records {
		startStr := rec["com.samsung.health.step_count.start_time"]
		countStr := rec["com.samsung.health.step_count.count"]
		if startStr == "" || countStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}
		if start.Before(cutoff) {
			continue
		}

		date := dateStr(start)
		count, _ := strconv.Atoi(countStr)
		dailySteps[date] += count
	}

	return stepsMapToSorted(dailySteps), nil
}

func stepsMapToSorted(m map[string]int) []StepRecord {
	var results []StepRecord
	for date, steps := range m {
		results = append(results, StepRecord{ID: denoteDayID(date), Date: date, Steps: steps})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Date > results[j].Date
	})
	return results
}

func parseHeartRecords(cfg *Config, days int) ([]HeartRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.tracker.heart_rate.")
	if path == "" {
		return nil, fmt.Errorf("heart rate CSV not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

	cutoff := cutoffTime(days)
	type dailyHR struct {
		sum     float64
		min     int
		max     int
		samples int
	}
	daily := make(map[string]*dailyHR)

	for _, rec := range records {
		startStr := rec["com.samsung.health.heart_rate.start_time"]
		hrStr := rec["com.samsung.health.heart_rate.heart_rate"]
		if startStr == "" || hrStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}
		if start.Before(cutoff) {
			continue
		}

		hr, err := strconv.ParseFloat(hrStr, 64)
		if err != nil || hr <= 0 {
			continue
		}
		hrInt := int(hr)

		date := dateStr(start)
		d, ok := daily[date]
		if !ok {
			d = &dailyHR{min: hrInt, max: hrInt}
			daily[date] = d
		}
		d.sum += hr
		d.samples++
		if hrInt < d.min {
			d.min = hrInt
		}
		if hrInt > d.max {
			d.max = hrInt
		}
	}

	var results []HeartRecord
	for date, d := range daily {
		results = append(results, HeartRecord{
			ID:      denoteDayID(date),
			Date:    date,
			AvgHR:   math.Round(d.sum/float64(d.samples)*10) / 10,
			MinHR:   d.min,
			MaxHR:   d.max,
			Samples: d.samples,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Date > results[j].Date
	})
	return results, nil
}

func parseStressRecords(cfg *Config, days int) ([]StressRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.stress.")
	if path == "" {
		return nil, fmt.Errorf("stress CSV not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

	cutoff := cutoffTime(days)
	type dailyStress struct {
		sum     float64
		min     int
		max     int
		samples int
	}
	daily := make(map[string]*dailyStress)

	for _, rec := range records {
		startStr := rec["start_time"]
		scoreStr := rec["score"]
		if startStr == "" || scoreStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}
		if start.Before(cutoff) {
			continue
		}

		score, err := strconv.ParseFloat(scoreStr, 64)
		if err != nil || score < 0 {
			continue
		}
		scoreInt := int(score)

		date := dateStr(start)
		d, ok := daily[date]
		if !ok {
			d = &dailyStress{min: scoreInt, max: scoreInt}
			daily[date] = d
		}
		d.sum += score
		d.samples++
		if scoreInt < d.min {
			d.min = scoreInt
		}
		if scoreInt > d.max {
			d.max = scoreInt
		}
	}

	var results []StressRecord
	for date, d := range daily {
		results = append(results, StressRecord{
			ID:       denoteDayID(date),
			Date:     date,
			AvgScore: math.Round(d.sum/float64(d.samples)*10) / 10,
			MinScore: d.min,
			MaxScore: d.max,
			Samples:  d.samples,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Date > results[j].Date
	})
	return results, nil
}

func parseExerciseRecords(cfg *Config, days int) ([]ExerciseRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.exercise.")
	if path == "" {
		return nil, fmt.Errorf("exercise CSV not found")
	}

	matches, _ := filepath.Glob(filepath.Join(cfg.ShealthDir, "com.samsung.shealth.exercise.2*.csv"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("exercise CSV not found")
	}
	path = matches[0]

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

	cutoff := cutoffTime(days)
	var results []ExerciseRecord

	for _, rec := range records {
		startStr := rec["com.samsung.health.exercise.start_time"]
		if startStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}
		if start.Before(cutoff) {
			continue
		}

		durStr := rec["com.samsung.health.exercise.duration"]
		durMs, _ := strconv.ParseFloat(durStr, 64)
		durMin := durMs / 60000.0
		if durMin <= 0 {
			continue
		}

		typeCode := rec["com.samsung.health.exercise.exercise_type"]
		if typeCode == "" {
			typeCode = rec["activity_type"]
		}
		typeName := exerciseTypes[typeCode]
		if typeName == "" {
			typeName = "Type_" + typeCode
		}

		er := ExerciseRecord{
			ID:              denoteID(start),
			Date:            dateStr(start),
			Type:            typeName,
			DurationMinutes: math.Round(durMin*10) / 10,
		}

		if s := rec["total_calorie"]; s != "" {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				er.Calories = math.Round(f*10) / 10
			}
		}
		if s := rec["com.samsung.health.exercise.mean_heart_rate"]; s != "" {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				er.AvgHR = math.Round(f*10) / 10
			}
		}
		if s := rec["com.samsung.health.exercise.max_heart_rate"]; s != "" {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				er.MaxHR = math.Round(f*10) / 10
			}
		}

		results = append(results, er)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Date > results[j].Date
	})
	return results, nil
}

func parseTimeRecords(cfg *Config, days int) ([]TimeRecord, error) {
	_ = days
	var results []TimeRecord
	sort.Slice(results, func(i, j int) bool {
		return results[i].Date > results[j].Date
	})
	return results, nil
}
