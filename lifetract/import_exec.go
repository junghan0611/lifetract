package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// execImport performs the actual CSV+SQLite → lifetract.db conversion.
func execImport(cfg *Config) (*ImportResult, error) {
	path := dbPath(cfg)

	// Remove old DB if exists (fresh import)
	os.Remove(path)

	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	result := &ImportResult{
		DBPath:  path,
		Tables:  []TableResult{},
		StartAt: time.Now(),
	}

	// Samsung Health CSVs
	importFuncs := []struct {
		name string
		fn   func(*sql.DB, *Config) (int, error)
	}{
		{"sleep", importSleep},
		{"sleep_stage", importSleepStage},
		{"heart_rate", importHeartRate},
		{"steps_daily", importStepsDaily},
		{"stress", importStress},
		{"exercise", importExercise},
		{"weight", importWeight},
		{"hrv", importHRV},
	}

	for _, f := range importFuncs {
		rows, err := f.fn(db, cfg)
		status := "ok"
		if err != nil {
			status = err.Error()
		}
		result.Tables = append(result.Tables, TableResult{
			Name:   f.name,
			Rows:   rows,
			Status: status,
		})
		result.TotalRows += rows
		if rows > 0 {
			logImport(db, "samsung_health", f.name, rows, cfg.ShealthDir)
		}
	}

	// aTimeLogger
	atlRows, err := importATimeLogger(db, cfg)
	status := "ok"
	if err != nil {
		status = err.Error()
	}
	result.Tables = append(result.Tables, TableResult{
		Name:   "atl_category+atl_interval",
		Rows:   atlRows,
		Status: status,
	})
	result.TotalRows += atlRows
	if atlRows > 0 {
		logImport(db, "atimelogger", "atl_interval", atlRows, cfg.ATimeLoggerDB)
	}

	// VACUUM for compact size
	db.Exec("VACUUM")

	info, _ := os.Stat(path)
	if info != nil {
		result.DBSizeMB = float64(info.Size()) / (1024 * 1024)
	}
	result.Duration = time.Since(result.StartAt).String()

	return result, nil
}

type ImportResult struct {
	DBPath    string        `json:"db_path"`
	Tables    []TableResult `json:"tables"`
	TotalRows int           `json:"total_rows"`
	DBSizeMB  float64       `json:"db_size_mb"`
	Duration  string        `json:"duration"`
	StartAt   time.Time     `json:"-"`
}

type TableResult struct {
	Name   string `json:"name"`
	Rows   int    `json:"rows"`
	Status string `json:"status"`
}

// --- Samsung Health importers ---

func importSleep(db *sql.DB, cfg *Config) (int, error) {
	path := cfg.shealthCSV("com.samsung.shealth.sleep.")
	if path == "" {
		return 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO sleep
		(id, uuid, start_time, end_time, duration_min, sleep_score, efficiency, total_light_min, total_rem_min)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count := 0
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
		end, err := parseShealthTime(endStr)
		if err != nil {
			continue
		}

		dur := end.Sub(start).Minutes()
		uuid := rec["com.samsung.health.sleep.datauuid"]

		stmt.Exec(
			denoteID(start),
			uuid,
			startStr,
			endStr,
			dur,
			parseInt(rec["sleep_score"]),
			parseFloat(rec["efficiency"]),
			parseFloat(rec["total_light_duration"]),
			parseFloat(rec["total_rem_duration"]),
		)
		count++
	}
	tx.Commit()
	return count, nil
}

func importSleepStage(db *sql.DB, cfg *Config) (int, error) {
	path := cfg.shealthCSV("com.samsung.health.sleep_stage.")
	if path == "" {
		return 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO sleep_stage
		(id, sleep_uuid, start_time, end_time, stage)
		VALUES (?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count := 0
	for _, rec := range records {
		uuid := rec["datauuid"]
		sleepUUID := rec["sleep_id"]
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

		id := uuid
		if id == "" {
			id = denoteID(start) + "_" + stageStr
		}

		stmt.Exec(id, sleepUUID, startStr, endStr, parseInt(stageStr))
		count++
	}
	tx.Commit()
	return count, nil
}

func importHeartRate(db *sql.DB, cfg *Config) (int, error) {
	path := cfg.shealthCSV("com.samsung.shealth.tracker.heart_rate.")
	if path == "" {
		return 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO heart_rate (id, start_time, heart_rate) VALUES (?, ?, ?)`)
	defer stmt.Close()

	count := 0
	for _, rec := range records {
		startStr := rec["com.samsung.health.heart_rate.start_time"]
		hrStr := rec["com.samsung.health.heart_rate.heart_rate"]
		if startStr == "" || hrStr == "" {
			continue
		}
		hr := parseFloat(hrStr)
		if hr <= 0 {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}

		uuid := rec["com.samsung.health.heart_rate.datauuid"]
		id := uuid
		if id == "" {
			id = denoteID(start)
		}

		stmt.Exec(id, startStr, hr)
		count++
	}
	tx.Commit()
	return count, nil
}

func importStepsDaily(db *sql.DB, cfg *Config) (int, error) {
	path := cfg.shealthCSV("com.samsung.shealth.step_daily_trend.")
	if path == "" {
		return 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO steps_daily
		(id, date, day_time_ms, count, distance, calorie) VALUES (?, ?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count := 0
	for _, rec := range records {
		// source_type=-2 is Samsung Health's merged/deduplicated record
		// across multiple devices (phone + watch). Other values are per-device
		// raw counts that would cause double-counting if summed.
		if rec["source_type"] != "-2" {
			continue
		}

		countStr := rec["count"]
		if countStr == "" {
			continue
		}
		steps := parseInt(countStr)
		if steps <= 0 {
			continue
		}

		dayTimeStr := rec["day_time"]
		var date string
		var dayTimeMs int64

		if dayTimeStr != "" {
			ms, err := strconv.ParseInt(dayTimeStr, 10, 64)
			if err == nil {
				t := time.Unix(ms/1000, 0)
				date = dateStr(t)
				dayTimeMs = ms
			}
		}
		if date == "" {
			ctStr := rec["create_time"]
			if ctStr == "" {
				continue
			}
			ct, err := parseShealthTime(ctStr)
			if err != nil {
				continue
			}
			date = dateStr(ct)
		}

		id := denoteDayID(date)
		uuid := rec["datauuid"]
		if uuid != "" {
			id = uuid // use original UUID to avoid dedup issues
		}

		stmt.Exec(id, date, dayTimeMs, steps,
			parseFloat(rec["distance"]),
			parseFloat(rec["calorie"]))
		count++
	}
	tx.Commit()
	return count, nil
}

func importStress(db *sql.DB, cfg *Config) (int, error) {
	path := cfg.shealthCSV("com.samsung.shealth.stress.")
	if path == "" {
		return 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO stress
		(id, start_time, score, min_score, max_score) VALUES (?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count := 0
	for _, rec := range records {
		startStr := rec["start_time"]
		scoreStr := rec["score"]
		if startStr == "" || scoreStr == "" {
			continue
		}

		uuid := rec["datauuid"]
		id := uuid
		if id == "" {
			start, err := parseShealthTime(startStr)
			if err != nil {
				continue
			}
			id = denoteID(start)
		}

		stmt.Exec(id, startStr,
			parseFloat(scoreStr),
			parseFloat(rec["min"]),
			parseFloat(rec["max"]))
		count++
	}
	tx.Commit()
	return count, nil
}

func importExercise(db *sql.DB, cfg *Config) (int, error) {
	// Find exact exercise CSV (not photo/program variants)
	matches, _ := filepath.Glob(filepath.Join(cfg.ShealthDir, "com.samsung.shealth.exercise.2*.csv"))
	if len(matches) == 0 {
		return 0, fmt.Errorf("csv not found")
	}
	path := matches[0]

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO exercise
		(id, start_time, end_time, exercise_type, duration_ms, calorie, mean_hr, max_hr, distance)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count := 0
	for _, rec := range records {
		startStr := rec["com.samsung.health.exercise.start_time"]
		if startStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}

		uuid := rec["com.samsung.health.exercise.datauuid"]
		id := uuid
		if id == "" {
			id = denoteID(start)
		}

		typeCode := rec["com.samsung.health.exercise.exercise_type"]
		if typeCode == "" {
			typeCode = rec["activity_type"]
		}

		stmt.Exec(id, startStr,
			rec["com.samsung.health.exercise.end_time"],
			parseInt(typeCode),
			parseInt(rec["com.samsung.health.exercise.duration"]),
			parseFloat(rec["total_calorie"]),
			parseFloat(rec["com.samsung.health.exercise.mean_heart_rate"]),
			parseFloat(rec["com.samsung.health.exercise.max_heart_rate"]),
			parseFloat(rec["com.samsung.health.exercise.total_distance"]))
		count++
	}
	tx.Commit()
	return count, nil
}

func importWeight(db *sql.DB, cfg *Config) (int, error) {
	path := cfg.shealthCSV("com.samsung.health.weight.")
	if path == "" {
		return 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO weight
		(id, start_time, weight, body_fat, muscle_mass) VALUES (?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count := 0
	for _, rec := range records {
		startStr := rec["start_time"]
		if startStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}

		uuid := rec["datauuid"]
		id := uuid
		if id == "" {
			id = denoteID(start)
		}

		stmt.Exec(id, startStr,
			parseFloat(rec["weight"]),
			parseFloat(rec["body_fat"]),
			parseFloat(rec["muscle_mass"]))
		count++
	}
	tx.Commit()
	return count, nil
}

func importHRV(db *sql.DB, cfg *Config) (int, error) {
	path := cfg.shealthCSV("com.samsung.health.hrv.")
	if path == "" {
		return 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO hrv (id, start_time, hrv_rmssd) VALUES (?, ?, ?)`)
	defer stmt.Close()

	count := 0
	for _, rec := range records {
		// HRV CSV column names vary; try common patterns
		startStr := firstNonEmpty(rec,
			"com.samsung.health.hrv.start_time",
			"start_time")
		hrvStr := firstNonEmpty(rec,
			"com.samsung.health.hrv.rmssd",
			"rmssd",
			"heart_rate_variability")
		if startStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}

		uuid := firstNonEmpty(rec,
			"com.samsung.health.hrv.datauuid",
			"datauuid")
		id := uuid
		if id == "" {
			id = denoteID(start)
		}

		stmt.Exec(id, startStr, parseFloat(hrvStr))
		count++
	}
	tx.Commit()
	return count, nil
}

// --- aTimeLogger importer ---

func importATimeLogger(db *sql.DB, cfg *Config) (int, error) {
	if _, err := os.Stat(cfg.ATimeLoggerDB); err != nil {
		return 0, fmt.Errorf("atimelogger db not found: %s", cfg.ATimeLoggerDB)
	}

	srcDB, err := sql.Open("sqlite", cfg.ATimeLoggerDB)
	if err != nil {
		return 0, fmt.Errorf("open atimelogger: %w", err)
	}
	defer srcDB.Close()

	// Import categories
	rows, err := srcDB.Query(`SELECT id, name, color, is_group, parent_id FROM activity_type`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tx, _ := db.Begin()
	catStmt, _ := tx.Prepare(`INSERT OR REPLACE INTO atl_category (id, name, color, is_group, parent_id) VALUES (?, ?, ?, ?, ?)`)

	for rows.Next() {
		var id, isGroup, parentID int
		var name string
		var color sql.NullInt64
		rows.Scan(&id, &name, &color, &isGroup, &parentID)
		catStmt.Exec(id, name, color.Int64, isGroup, parentID)
	}
	catStmt.Close()

	// Import intervals (from time_interval2 which has the actual data)
	iRows, err := srcDB.Query(`SELECT id, guid, start, finish, comment, activity_type_id, is_deleted
		FROM time_interval2`)
	if err != nil {
		return 0, err
	}
	defer iRows.Close()

	intStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO atl_interval
		(id, guid, start_time, end_time, comment, category_id, is_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)

	count := 0
	for iRows.Next() {
		var id, start, finish, catID, isDeleted int
		var guid, comment sql.NullString
		iRows.Scan(&id, &guid, &start, &finish, &comment, &catID, &isDeleted)
		intStmt.Exec(id, guid.String, start, finish, comment.String, catID, isDeleted)
		count++
	}
	intStmt.Close()
	tx.Commit()

	return count, nil
}

// parseInt, parseFloat, firstNonEmpty → helpers.go
