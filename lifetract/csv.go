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

	// A row that will not parse is not a row that isn't there.
	//
	// This used to `continue`, and the read still returned success. So a corrupted
	// export lost rows quietly, and — because Samsung's export is cumulative — the
	// new rows in the next dump could cover the loss, leaving the count higher than
	// last time and the shrink guard with nothing to see. The whole file is refused
	// instead: import reports it and does not promote, and the live DB stands.
	//
	// Measured against the real export at the time this was written: 0 malformed
	// rows in 335,299. Refusing them costs nothing and buys the silence back.
	var records []map[string]string
	line := 2 // metadata + header are already consumed
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			return nil, nil, fmt.Errorf("malformed row at line %d of %s: %w", line, filepath.Base(path), err)
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

func parseSleepRecords(cfg *Config, w Window) ([]SleepRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.sleep.")
	if path == "" {
		return nil, fmt.Errorf("sleep CSV not found in %s", cfg.ShealthDir)
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

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
		if !w.contains(start) {
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

// stepDay is one day's step total, already keyed to the day it measures.
type stepDay struct {
	day      time.Time
	count    int
	distance float64
	calorie  float64
	update   time.Time // Samsung's update_time — the tiebreak between revisions of a day
}

// stepDayStats is what selectStepDays could not turn into a day, kept apart so each
// caller can apply its own policy: the importer counts and carries on, a query refuses
// to answer at all.
type stepDayStats struct {
	superseded int   // an earlier revision of a day that a later one replaced
	rejected   int   // a known Samsung placeholder date, intentionally refused
	invalid    int   // in the file and unreadable — the tool failing to read its source
	firstErr   error // the first such row, for the caller that wants to stop
}

func (st *stepDayStats) fail(err error) {
	st.invalid++
	if st.firstErr == nil {
		st.firstErr = err
	}
}

// selectStepDays reduces step_daily_trend to at most one record per day.
//
// Samsung ships the merged record (source_type=-2) more than once for a day when the
// phone re-syncs; this export has six such days. Five repeat the same count, so any
// choice agrees. On 2025-07-20 the two disagree — 909 and 463 — and 463 is a snapshot
// taken at 05:50 that a later revision replaced. So the newest update_time wins.
// Not create_time: on that very day it runs backwards against update_time and would
// enshrine the stale half-day as the truth.
//
// Both surfaces read the day through here, because they used to decide it separately
// and disagree: the DB path SUMmed the duplicates (7,685 became 15,370) while the CSV
// path let whichever row came last in the file win. One day, one number, one place.
func selectStepDays(records []map[string]string) (map[string]stepDay, stepDayStats) {
	days := make(map[string]stepDay)
	var st stepDayStats

	for _, rec := range records {
		// source_type=-2 is Samsung Health's merged record across devices. Other values
		// are per-device raw counts that would double-count if summed.
		if rec["source_type"] != "-2" {
			continue
		}

		countStr := rec["count"]
		if countStr == "" {
			st.fail(fmt.Errorf("count: missing"))
			continue
		}

		// The day a row is about, never the day it was written. See parseDayTime.
		t, err := parseDayTime(rec["day_time"])
		if err != nil {
			st.fail(err)
			continue
		}
		update, err := parseShealthTime(rec["update_time"])
		if err != nil {
			st.fail(fmt.Errorf("update_time: unreadable %q", rec["update_time"]))
			continue
		}

		var n numRow
		count := n.int("count", countStr)
		dist := n.float("distance", rec["distance"])
		cal := n.float("calorie", rec["calorie"])
		if n.bad() {
			st.fail(n.err)
			continue
		}
		if isSentinelTime(t) {
			st.rejected++
			continue
		}
		// A daily aggregate may name today, but not a day after today. Compare
		// normalized dates: epoch-millis exports can represent a day at 09:00 KST.
		if startOfDay(t).After(startOfDay(nowKST())) {
			st.fail(fmt.Errorf("day_time: future date %s", dateStr(t)))
			continue
		}
		if count <= 0 {
			continue // a policy filter: the watch logging no steps is not a broken row
		}

		cand := stepDay{day: t, count: count, distance: dist, calorie: cal, update: update}
		date := dateStr(t)
		if prev, seen := days[date]; seen {
			if prev.update.Equal(cand.update) &&
				(prev.count != cand.count || prev.distance != cand.distance || prev.calorie != cand.calorie) {
				st.fail(fmt.Errorf("update_time: conflicting revisions for %s at %s", date, update.Format("2006-01-02 15:04:05.000")))
				continue
			}
			st.superseded++
			if !cand.update.After(prev.update) {
				continue // the row already held is the later (or identical) revision
			}
		}
		days[date] = cand
	}

	return days, st
}

func parseStepRecords(cfg *Config, w Window) ([]StepRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.step_daily_trend.")
	if path == "" {
		return parseStepRecordsFromPedometer(cfg, w)
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

	days, st := selectStepDays(records)
	if st.firstErr != nil {
		return nil, fmt.Errorf("steps: %w", st.firstErr)
	}

	dailySteps := make(map[string]int)
	for date, d := range days {
		if !w.contains(d.day) {
			continue
		}
		dailySteps[date] = d.count
	}

	return stepsMapToSorted(dailySteps), nil
}

func parseStepRecordsFromPedometer(cfg *Config, w Window) ([]StepRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.tracker.pedometer_step_count.")
	if path == "" {
		return nil, fmt.Errorf("step count CSV not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

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
		if !w.contains(start) {
			continue
		}

		var n numRow
		count := n.int("count", countStr)
		if n.bad() {
			return nil, fmt.Errorf("steps: %w", n.err)
		}
		dailySteps[dateStr(start)] += count
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

func parseHeartRecords(cfg *Config, w Window) ([]HeartRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.tracker.heart_rate.")
	if path == "" {
		return nil, fmt.Errorf("heart rate CSV not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

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
		if !w.contains(start) {
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

func parseStressRecords(cfg *Config, w Window) ([]StressRecord, error) {
	path := cfg.shealthCSV("com.samsung.shealth.stress.")
	if path == "" {
		return nil, fmt.Errorf("stress CSV not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

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
		if !w.contains(start) {
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

func parseExerciseRecords(cfg *Config, w Window) ([]ExerciseRecord, error) {
	// It already had the newest export from shealthCSV, then threw it away for
	// matches[0] — the oldest one. Same bug as the importer had, same file.
	path := cfg.shealthCSV("com.samsung.shealth.exercise.")
	if path == "" {
		return nil, fmt.Errorf("exercise CSV not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return nil, err
	}

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
		if !w.contains(start) {
			continue
		}

		var n numRow
		durMs := n.float("duration", rec["com.samsung.health.exercise.duration"])
		if n.bad() {
			return nil, fmt.Errorf("exercise: %w", n.err)
		}
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

// parseTimeRecords cannot answer, and says so.
//
// aTimeLogger has no CSV export — it is a SQLite file, and the DB path is the only
// one that reads it. This used to return an empty list, so in CSV mode the time
// axis was not missing, it was ZERO: timeline, today and read each assembled a day
// with no tracked hours in it and exited 0.
//
// `time` already refused to do that ("this is not an empty result"). The three
// commands that fold the same axis into a larger answer quietly did the opposite —
// one tool with two voices about one fact. The collector downstream consumes depth
// 0 and would have written the silence into a public record as a day of no work.
func parseTimeRecords(cfg *Config, w Window) ([]TimeRecord, error) {
	_ = w
	return nil, fmt.Errorf("aTimeLogger has no CSV source, so the time axis cannot be read without the DB "+
		"(run 'lifetract import --exec'; expected at %s) — this is not an empty result", cfg.ATimeLoggerDB)
}
