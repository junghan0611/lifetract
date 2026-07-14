package main

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"
)

// dbQuerySleep returns sleep records from DB.
func dbQuerySleep(cfg *Config, w Window) ([]SleepRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	from, to := w.shealthBounds()
	rows, err := db.Query(`
		SELECT id, uuid, start_time, end_time, duration_min,
		       sleep_score, efficiency, total_light_min, total_rem_min
		FROM sleep WHERE start_time >= ? AND start_time < ?
		ORDER BY start_time DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Pre-load stages grouped by sleep UUID
	stageMap, err := dbLoadSleepStages(db)
	if err != nil {
		return nil, err
	}

	var results []SleepRecord
	for rows.Next() {
		var id, uuid, startStr, endStr string
		var durMin, eff, lightMin, remMin sql.NullFloat64
		var score sql.NullInt64
		if err := rows.Scan(&id, &uuid, &startStr, &endStr, &durMin, &score, &eff, &lightMin, &remMin); err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

		// A stored time we cannot read is not a night that did not happen. Folded to
		// the zero time it made end-start negative, and the `continue` below — meant
		// for genuinely impossible durations — dropped the night without a word.
		start, err := parseShealthTime(startStr)
		if err != nil {
			return nil, fmt.Errorf("sleep %s: start_time %q: %w", id, startStr, err)
		}
		end, err := parseShealthTime(endStr)
		if err != nil {
			return nil, fmt.Errorf("sleep %s: end_time %q: %w", id, endStr, err)
		}
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
	// An iteration that stopped early returns fewer rows than the query matched,
	// and every one of them looks like an ordinary quiet day.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return results, nil
}

// dbLoadSleepStages returns the stage breakdown per sleep session. It reports its
// failures: a stage table that cannot be read used to come back as an empty map,
// and every night then looked like a night the watch recorded no stages.
func dbLoadSleepStages(db *sql.DB) (map[string]*SleepStages, error) {
	rows, err := db.Query(`SELECT sleep_uuid, stage, start_time, end_time FROM sleep_stage`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*SleepStages)
	for rows.Next() {
		var uuid, startStr, endStr string
		var stage int
		if err := rows.Scan(&uuid, &stage, &startStr, &endStr); err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

		// Measured against the zero time, a stage's minutes are not short — they are
		// astronomical, and they were being added to the night's totals.
		start, err := parseShealthTime(startStr)
		if err != nil {
			return nil, fmt.Errorf("sleep_stage of %s: start_time %q: %w", uuid, startStr, err)
		}
		end, err := parseShealthTime(endStr)
		if err != nil {
			return nil, fmt.Errorf("sleep_stage of %s: end_time %q: %w", uuid, endStr, err)
		}
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read sleep stages: %w", err)
	}

	for _, s := range result {
		s.DeepMin = math.Round(s.DeepMin*10) / 10
		s.LightMin = math.Round(s.LightMin*10) / 10
		s.RemMin = math.Round(s.RemMin*10) / 10
		s.AwakeMin = math.Round(s.AwakeMin*10) / 10
	}
	return result, nil
}

// dbQuerySteps returns daily step records from DB.
func dbQuerySteps(cfg *Config, w Window) ([]StepRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	from, to := w.dateBounds()
	rows, err := db.Query(`
		SELECT date, SUM(count) as total
		FROM steps_daily WHERE date >= ? AND date < ?
		GROUP BY date ORDER BY date DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []StepRecord
	for rows.Next() {
		var date string
		var total int
		if err := rows.Scan(&date, &total); err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		if total > 0 {
			results = append(results, StepRecord{
				ID:    denoteDayID(date),
				Date:  date,
				Steps: total,
			})
		}
	}
	// An iteration that stopped early returns fewer rows than the query matched,
	// and every one of them looks like an ordinary quiet day.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return results, nil
}

// dbQueryHeart returns daily heart rate records from DB.
func dbQueryHeart(cfg *Config, w Window) ([]HeartRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	from, to := w.shealthBounds()
	rows, err := db.Query(`
		SELECT DATE(start_time) as date,
		       ROUND(AVG(heart_rate), 1),
		       CAST(MIN(heart_rate) AS INTEGER),
		       CAST(MAX(heart_rate) AS INTEGER),
		       COUNT(*)
		FROM heart_rate
		WHERE start_time >= ? AND start_time < ? AND heart_rate > 0
		GROUP BY date ORDER BY date DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []HeartRecord
	for rows.Next() {
		var r HeartRecord
		if err := rows.Scan(&r.Date, &r.AvgHR, &r.MinHR, &r.MaxHR, &r.Samples); err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		r.ID = denoteDayID(r.Date)
		results = append(results, r)
	}
	// An iteration that stopped early returns fewer rows than the query matched,
	// and every one of them looks like an ordinary quiet day.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return results, nil
}

// dbQueryStress returns daily stress records from DB.
func dbQueryStress(cfg *Config, w Window) ([]StressRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	from, to := w.shealthBounds()
	rows, err := db.Query(`
		SELECT DATE(start_time) as date,
		       ROUND(AVG(score), 1),
		       CAST(MIN(score) AS INTEGER),
		       CAST(MAX(score) AS INTEGER),
		       COUNT(*)
		FROM stress
		WHERE start_time >= ? AND start_time < ? AND score >= 0
		GROUP BY date ORDER BY date DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []StressRecord
	for rows.Next() {
		var r StressRecord
		if err := rows.Scan(&r.Date, &r.AvgScore, &r.MinScore, &r.MaxScore, &r.Samples); err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		r.ID = denoteDayID(r.Date)
		results = append(results, r)
	}
	// An iteration that stopped early returns fewer rows than the query matched,
	// and every one of them looks like an ordinary quiet day.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return results, nil
}

// dbQueryExercise returns exercise records from DB.
func dbQueryExercise(cfg *Config, w Window) ([]ExerciseRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	from, to := w.shealthBounds()
	rows, err := db.Query(`
		SELECT id, start_time, exercise_type, duration_ms, calorie, mean_hr, max_hr
		FROM exercise
		WHERE start_time >= ? AND start_time < ?
		ORDER BY start_time DESC`, from, to)
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
		if err := rows.Scan(&id, &startStr, &exType, &durMs, &cal, &meanHR, &maxHR); err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

		// The one that does not skip. exercise takes its duration from another column,
		// so a start_time we could not read passed every guard and went out as a
		// record dated 0001-01-01 — the tool asserting a date it never read.
		start, err := parseShealthTime(startStr)
		if err != nil {
			return nil, fmt.Errorf("exercise %s: start_time %q: %w", id, startStr, err)
		}
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
	// An iteration that stopped early returns fewer rows than the query matched,
	// and every one of them looks like an ordinary quiet day.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return results, nil
}

// dbQueryTime returns time tracking records from DB (aTimeLogger).
//
// A block is attributed to the day it STARTS on, so a sleep block running
// 21:14 → 05:48 belongs entirely to the earlier day. 12% of blocks cross
// midnight, so this is the common case, not an edge case. Callers depend on it.
//
// The day is derived by shifting the stored epoch onto the KST axis (+9h), never
// by SQLite's 'localtime' modifier — that reads the invoking shell's $TZ and
// would silently re-bucket a day's blocks depending on who called us.
//
// comment is never selected here, and must never be: those blocks carry family
// names in plain text. The DB holds them; this CLI is the only door out, so the
// door is where the contract lives. See TestCommentNeverEscapes.
func dbQueryTime(cfg *Config, w Window) ([]TimeRecord, error) {
	db, err := openDB(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	from, to := w.epochBounds()
	rows, err := db.Query(`
		SELECT c.name,
		       DATE(i.start_time, 'unixepoch', '+9 hours') as date,
		       SUM(i.end_time - i.start_time) / 60.0 as minutes
		FROM atl_interval i
		JOIN atl_category c ON i.category_id = c.id
		WHERE i.is_deleted = 0 AND i.start_time >= ? AND i.start_time < ?
		GROUP BY date, c.name
		ORDER BY date DESC, minutes DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dateMap := make(map[string][]TimeCategory)
	for rows.Next() {
		var name, date string
		var minutes float64
		if err := rows.Scan(&name, &date, &minutes); err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

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
	// An iteration that stopped early returns fewer rows than the query matched,
	// and every one of them looks like an ordinary quiet day.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return results, nil
}

// dbQueryTimeline returns timeline entries from DB.
// dbQueryTimeline assembles the day view from six streams.
//
// Every one of those calls used to end in `_`. Drop a table and the timeline still
// came back, one stream short, shaped exactly like a stretch of quiet days — and
// the collector downstream would have written that hole into the record as a zero.
// A stream that cannot answer fails the command: a hole is not a zero.
func dbQueryTimeline(cfg *Config, w Window) ([]TimelineEntry, error) {
	steps, err := dbQuerySteps(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("steps: %w", err)
	}
	sleeps, err := dbQuerySleep(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("sleep: %w", err)
	}
	hearts, err := dbQueryHeart(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("heart: %w", err)
	}
	stresses, err := dbQueryStress(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("stress: %w", err)
	}
	exercises, err := dbQueryExercise(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("exercise: %w", err)
	}
	times, err := dbQueryTime(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("time: %w", err)
	}

	return buildTimeline(steps, sleeps, hearts, stresses, exercises, times), nil
}

// dbQueryDay returns a single day's detail from DB.
func dbQueryDay(cfg *Config, day time.Time) (interface{}, error) {
	dateS := dateStr(day)
	dayID := denoteDayID(dateS)

	w := dayWindow(day)
	steps, err := dbQuerySteps(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("steps: %w", err)
	}
	sleeps, err := dbQuerySleep(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("sleep: %w", err)
	}
	hearts, err := dbQueryHeart(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("heart: %w", err)
	}
	stresses, err := dbQueryStress(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("stress: %w", err)
	}
	exercises, err := dbQueryExercise(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("exercise: %w", err)
	}
	times, err := dbQueryTime(cfg, w)
	if err != nil {
		return nil, fmt.Errorf("time: %w", err)
	}

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
	// Only "this table does not have it" moves on to the next kind. Any other
	// error is the table failing to answer, and turning that into "no event found"
	// is the tool reporting an absence it never established.
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("sleep: %w", err)
	}
	if err == nil {
		start, perr := parseShealthTime(startStr)
		if perr != nil {
			return nil, fmt.Errorf("sleep %s: start_time %q: %w", id, startStr, perr)
		}
		end, perr := parseShealthTime(endStr)
		if perr != nil {
			return nil, fmt.Errorf("sleep %s: end_time %q: %w", id, endStr, perr)
		}
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
		stageMap, err := dbLoadSleepStages(db)
		if err != nil {
			return nil, err
		}
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
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("exercise: %w", err)
	}
	if err == nil {
		start, perr := parseShealthTime(eStartStr)
		if perr != nil {
			return nil, fmt.Errorf("exercise %s: start_time %q: %w", id, eStartStr, perr)
		}
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
