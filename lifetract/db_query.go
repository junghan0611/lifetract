package main

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"
)

// dbQuerySleep returns sleep records from DB.
func dbQuerySleep(cfg *Config, days int) ([]SleepRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	cutoff := cutoffTime(days).Format("2006-01-02 15:04:05.000")
	rows, err := db.Query(`
		SELECT id, uuid, start_time, end_time, duration_min,
		       sleep_score, efficiency, total_light_min, total_rem_min
		FROM sleep WHERE start_time >= ? ORDER BY start_time DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Pre-load stages grouped by sleep UUID
	stageMap := dbLoadSleepStages(db)

	var results []SleepRecord
	for rows.Next() {
		var id, uuid, startStr, endStr string
		var durMin, eff, lightMin, remMin sql.NullFloat64
		var score sql.NullInt64
		rows.Scan(&id, &uuid, &startStr, &endStr, &durMin, &score, &eff, &lightMin, &remMin)

		start, _ := parseShealthTime(startStr)
		end, _ := parseShealthTime(endStr)
		durH := end.Sub(start).Hours()
		if durH <= 0 || durH > 24 {
			continue
		}

		sr := SleepRecord{
			ID:            denoteID(start),
			Date:          dateStr(start),
			Start:         timeStr(start),
			End:           timeStr(end),
			DurationHours: math.Round(durH*10) / 10,
		}
		if score.Valid {
			sr.SleepScore = int(score.Int64)
		}
		if eff.Valid {
			sr.Efficiency = math.Round(eff.Float64*10) / 10
		}
		if stages, ok := stageMap[uuid]; ok {
			sr.Stages = stages
		} else if lightMin.Valid || remMin.Valid {
			sr.Stages = &SleepStages{
				LightMin: lightMin.Float64,
				RemMin:   remMin.Float64,
			}
		}
		results = append(results, sr)
	}
	return results, nil
}

func dbLoadSleepStages(db *sql.DB) map[string]*SleepStages {
	rows, err := db.Query(`SELECT sleep_uuid, stage, start_time, end_time FROM sleep_stage`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string]*SleepStages)
	for rows.Next() {
		var uuid, startStr, endStr string
		var stage int
		rows.Scan(&uuid, &stage, &startStr, &endStr)

		start, _ := parseShealthTime(startStr)
		end, _ := parseShealthTime(endStr)
		dur := end.Sub(start).Minutes()

		s, ok := result[uuid]
		if !ok {
			s = &SleepStages{}
			result[uuid] = s
		}
		switch stage {
		case 40001:
			s.AwakeMin += dur
		case 40002:
			s.LightMin += dur
		case 40003:
			s.DeepMin += dur
		case 40004:
			s.RemMin += dur
		}
	}
	for _, s := range result {
		s.DeepMin = math.Round(s.DeepMin*10) / 10
		s.LightMin = math.Round(s.LightMin*10) / 10
		s.RemMin = math.Round(s.RemMin*10) / 10
		s.AwakeMin = math.Round(s.AwakeMin*10) / 10
	}
	return result
}

// dbQuerySteps returns daily step records from DB.
func dbQuerySteps(cfg *Config, days int) ([]StepRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	cutoff := cutoffTime(days).Format("2006-01-02")
	rows, err := db.Query(`
		SELECT date, SUM(count) as total
		FROM steps_daily WHERE date >= ?
		GROUP BY date ORDER BY date DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []StepRecord
	for rows.Next() {
		var date string
		var total int
		rows.Scan(&date, &total)
		if total > 0 {
			results = append(results, StepRecord{
				ID:    denoteDayID(date),
				Date:  date,
				Steps: total,
			})
		}
	}
	return results, nil
}

// dbQueryHeart returns daily heart rate records from DB.
func dbQueryHeart(cfg *Config, days int) ([]HeartRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	cutoff := cutoffTime(days).Format("2006-01-02 15:04:05.000")
	rows, err := db.Query(`
		SELECT DATE(start_time) as date,
		       ROUND(AVG(heart_rate), 1),
		       CAST(MIN(heart_rate) AS INTEGER),
		       CAST(MAX(heart_rate) AS INTEGER),
		       COUNT(*)
		FROM heart_rate
		WHERE start_time >= ? AND heart_rate > 0
		GROUP BY date ORDER BY date DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []HeartRecord
	for rows.Next() {
		var r HeartRecord
		rows.Scan(&r.Date, &r.AvgHR, &r.MinHR, &r.MaxHR, &r.Samples)
		r.ID = denoteDayID(r.Date)
		results = append(results, r)
	}
	return results, nil
}

// dbQueryStress returns daily stress records from DB.
func dbQueryStress(cfg *Config, days int) ([]StressRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	cutoff := cutoffTime(days).Format("2006-01-02 15:04:05.000")
	rows, err := db.Query(`
		SELECT DATE(start_time) as date,
		       ROUND(AVG(score), 1),
		       CAST(MIN(score) AS INTEGER),
		       CAST(MAX(score) AS INTEGER),
		       COUNT(*)
		FROM stress
		WHERE start_time >= ? AND score >= 0
		GROUP BY date ORDER BY date DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []StressRecord
	for rows.Next() {
		var r StressRecord
		rows.Scan(&r.Date, &r.AvgScore, &r.MinScore, &r.MaxScore, &r.Samples)
		r.ID = denoteDayID(r.Date)
		results = append(results, r)
	}
	return results, nil
}

// dbQueryExercise returns exercise records from DB.
func dbQueryExercise(cfg *Config, days int) ([]ExerciseRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	cutoff := cutoffTime(days).Format("2006-01-02 15:04:05.000")
	rows, err := db.Query(`
		SELECT id, start_time, exercise_type, duration_ms, calorie, mean_hr, max_hr
		FROM exercise
		WHERE start_time >= ?
		ORDER BY start_time DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ExerciseRecord
	for rows.Next() {
		var id, startStr string
		var exType sql.NullInt64
		var durMs sql.NullInt64
		var cal, meanHR, maxHR sql.NullFloat64
		rows.Scan(&id, &startStr, &exType, &durMs, &cal, &meanHR, &maxHR)

		start, _ := parseShealthTime(startStr)
		durMin := float64(durMs.Int64) / 60000.0
		if durMin <= 0 {
			continue
		}

		typeCode := ""
		if exType.Valid {
			typeCode = fmt.Sprintf("%d", exType.Int64)
		}
		typeName := exerciseTypes[typeCode]
		if typeName == "" {
			typeName = "Type_" + typeCode
		}

		r := ExerciseRecord{
			ID:              denoteID(start),
			Date:            dateStr(start),
			Type:            typeName,
			DurationMinutes: math.Round(durMin*10) / 10,
		}
		if cal.Valid {
			r.Calories = math.Round(cal.Float64*10) / 10
		}
		if meanHR.Valid {
			r.AvgHR = math.Round(meanHR.Float64*10) / 10
		}
		if maxHR.Valid {
			r.MaxHR = math.Round(maxHR.Float64*10) / 10
		}
		results = append(results, r)
	}
	return results, nil
}

// dbQueryTime returns time tracking records from DB (aTimeLogger).
func dbQueryTime(cfg *Config, days int) ([]TimeRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	cutoff := cutoffTime(days)
	cutoffEpoch := cutoff.Unix()

	rows, err := db.Query(`
		SELECT c.name,
		       DATE(i.start_time, 'unixepoch', 'localtime') as date,
		       SUM(i.end_time - i.start_time) / 60.0 as minutes
		FROM atl_interval i
		JOIN atl_category c ON i.category_id = c.id
		WHERE i.is_deleted = 0 AND i.start_time >= ?
		GROUP BY date, c.name
		ORDER BY date DESC, minutes DESC`, cutoffEpoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dateMap := make(map[string][]TimeCategory)
	for rows.Next() {
		var name, date string
		var minutes float64
		rows.Scan(&name, &date, &minutes)

		// Optional category filter
		if cfg.Category != "" && name != cfg.Category {
			continue
		}

		dateMap[date] = append(dateMap[date], TimeCategory{
			Name:    name,
			Minutes: math.Round(minutes*10) / 10,
		})
	}

	var results []TimeRecord
	for date, cats := range dateMap {
		results = append(results, TimeRecord{Date: date, Categories: cats})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Date > results[j].Date
	})
	return results, nil
}

// dbQueryTimeline returns timeline entries from DB.
func dbQueryTimeline(cfg *Config, days int) ([]TimelineEntry, error) {
	steps, _ := dbQuerySteps(cfg, days)
	sleeps, _ := dbQuerySleep(cfg, days)
	hearts, _ := dbQueryHeart(cfg, days)
	stresses, _ := dbQueryStress(cfg, days)
	exercises, _ := dbQueryExercise(cfg, days)
	times, _ := dbQueryTime(cfg, days)

	return buildTimeline(steps, sleeps, hearts, stresses, exercises, times), nil
}

// dbQueryDay returns a single day's detail from DB.
func dbQueryDay(cfg *Config, day time.Time) (interface{}, error) {
	dateS := dateStr(day)
	dayID := denoteDayID(dateS)

	// Query everything for ±1 day
	origDays := cfg.Days
	cfg.Days = int(time.Since(day).Hours()/24) + 2

	steps, _ := dbQuerySteps(cfg, cfg.Days)
	sleeps, _ := dbQuerySleep(cfg, cfg.Days)
	hearts, _ := dbQueryHeart(cfg, cfg.Days)
	stresses, _ := dbQueryStress(cfg, cfg.Days)
	exercises, _ := dbQueryExercise(cfg, cfg.Days)
	times, _ := dbQueryTime(cfg, cfg.Days)

	cfg.Days = origDays

	entry := &TimelineEntry{ID: dayID, Date: dateS}

	for _, r := range steps {
		if r.Date == dateS {
			ensureTimelineHealth(entry).Steps = r.Steps
			break
		}
	}

	var sleepSessions []SleepRecord
	for _, r := range sleeps {
		if r.Date == dateS {
			sleepSessions = append(sleepSessions, r)
		}
	}
	if len(sleepSessions) > 0 {
		h := ensureTimelineHealth(entry)
		h.SleepHours = sleepSessions[0].DurationHours
		h.SleepScore = sleepSessions[0].SleepScore
	}

	for _, r := range hearts {
		if r.Date == dateS {
			h := ensureTimelineHealth(entry)
			h.AvgHR = r.AvgHR
			h.MinHR = r.MinHR
			h.MaxHR = r.MaxHR
			break
		}
	}

	for _, r := range stresses {
		if r.Date == dateS {
			ensureTimelineHealth(entry).StressAvg = r.AvgScore
			break
		}
	}

	for _, r := range exercises {
		if r.Date == dateS {
			entry.Exercise = append(entry.Exercise, ExerciseBrief{
				Type:     r.Type,
				Minutes:  r.DurationMinutes,
				Calories: r.Calories,
			})
		}
	}

	for _, r := range times {
		if r.Date == dateS {
			totalMin := 0.0
			for _, c := range r.Categories {
				totalMin += c.Minutes
			}
			entry.Time = &TimeMetrics{
				Categories: r.Categories,
				TotalMin:   round1(totalMin),
			}
			break
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

// dbQueryEvent finds a specific event by Denote ID from DB.
func dbQueryEvent(cfg *Config, t time.Time, id string) (interface{}, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Try sleep
	var sid, uuid, startStr, endStr string
	var durMin, eff, lightMin, remMin sql.NullFloat64
	var score sql.NullInt64
	err = db.QueryRow(`SELECT id, uuid, start_time, end_time, duration_min,
		sleep_score, efficiency, total_light_min, total_rem_min
		FROM sleep WHERE id = ?`, id).Scan(
		&sid, &uuid, &startStr, &endStr, &durMin, &score, &eff, &lightMin, &remMin)
	if err == nil {
		start, _ := parseShealthTime(startStr)
		end, _ := parseShealthTime(endStr)
		sr := SleepRecord{
			ID:            denoteID(start),
			Date:          dateStr(start),
			Start:         timeStr(start),
			End:           timeStr(end),
			DurationHours: math.Round(end.Sub(start).Hours()*10) / 10,
		}
		if score.Valid {
			sr.SleepScore = int(score.Int64)
		}
		if eff.Valid {
			sr.Efficiency = math.Round(eff.Float64*10) / 10
		}
		stageMap := dbLoadSleepStages(db)
		if stages, ok := stageMap[uuid]; ok {
			sr.Stages = stages
		}
		return sr, nil
	}

	// Try exercise
	var eid, eStartStr string
	var exType, eDurMs sql.NullInt64
	var cal, meanHR, maxHR sql.NullFloat64
	err = db.QueryRow(`SELECT id, start_time, exercise_type, duration_ms, calorie, mean_hr, max_hr
		FROM exercise WHERE id = ?`, id).Scan(
		&eid, &eStartStr, &exType, &eDurMs, &cal, &meanHR, &maxHR)
	if err == nil {
		start, _ := parseShealthTime(eStartStr)
		durMin := float64(eDurMs.Int64) / 60000.0
		typeCode := ""
		if exType.Valid {
			typeCode = fmt.Sprintf("%d", exType.Int64)
		}
		typeName := exerciseTypes[typeCode]
		if typeName == "" {
			typeName = "Type_" + typeCode
		}
		r := ExerciseRecord{
			ID:              denoteID(start),
			Date:            dateStr(start),
			Type:            typeName,
			DurationMinutes: math.Round(durMin*10) / 10,
		}
		if cal.Valid {
			r.Calories = math.Round(cal.Float64*10) / 10
		}
		if meanHR.Valid {
			r.AvgHR = math.Round(meanHR.Float64*10) / 10
		}
		if maxHR.Valid {
			r.MaxHR = math.Round(maxHR.Float64*10) / 10
		}
		return r, nil
	}

	return nil, fmt.Errorf("no event found for ID %s", id)
}

// --- helpers ---

func ensureTimelineHealth(e *TimelineEntry) *HealthMetrics {
	if e.Health == nil {
		e.Health = &HealthMetrics{}
	}
	return e.Health
}
