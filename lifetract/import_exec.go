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
//
// The import builds a candidate and promotes it only if the run is sound. It used
// to delete the live DB first and build in its place, which meant that by the time
// the warning about a lost stream was printed, the DB that still had the stream was
// already gone and every query afterwards read the broken one. Saying "I lost it"
// is worth little while still handing over the loss: a warning the caller must act
// on before the damage lands is a warning, and one that arrives after is an epitaph.
func execImport(cfg *Config) (*ImportResult, error) {
	path := dbPath(cfg)
	candidate := path + ".candidate"

	// What the streams were worth last time — read from the live DB, which is
	// still there and stays there (import_ledger.go).
	base := readBaseline(path)

	if err := removeDB(candidate); err != nil {
		return nil, fmt.Errorf("clear candidate: %w", err)
	}

	db, err := openDB(candidate)
	if err != nil {
		return nil, err
	}
	defer db.Close() // no-op after the explicit Close below; here for the error paths

	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	// One stamp, one id, for the whole run. Taken here, once — a clock read per
	// row is a clock that ticks mid-import and splits one run into two.
	result := &ImportResult{
		DBPath:   path,
		Status:   statusOK,
		Tables:   []TableResult{},
		Warnings: []string{},
		Rejected: []string{},
		StartAt:  time.Now(),
		runID:    base.nextRunID(),
		runAt:    nowStamp(),
	}
	result.ImportID = result.runID
	switch {
	case base.Exists:
		result.BaselineAt = base.At
		result.PrevTotalRows = base.prevTotal()
	case base.ReadErr != "":
		// The ledger was there and we could not read it. That is not a first
		// import, and calling it one would silently disarm the loss check for
		// this run — the exact silence the check exists to break.
		result.warn("ledger unreadable — no loss check this run: " + base.ReadErr)
	default:
		result.Note = "first import — no baseline to compare against"
	}

	// The history has to survive into the candidate, or the candidate is a DB that
	// has forgotten every stream it ever held. A carry-forward that fails is not a
	// detail to log and move past: promote that file and the ledger is gone.
	if err := carryForward(db, base); err != nil {
		result.warn("ledger carry-forward failed — not promoting: " + err.Error())
	}

	// Samsung Health CSVs
	importFuncs := []struct {
		name string
		fn   func(*sql.DB, *Config) (int, int, error)
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
		rows, rejected, err := f.fn(db, cfg)
		result.record(base, f.name, "samsung_health", rows, rejected, err, db, cfg.ShealthDir)
	}

	// aTimeLogger. The stream is named for what is counted (intervals), because
	// the ledger is keyed by that name and a name that drifts is a baseline that
	// never matches.
	atlRows, atlRejected, atlErr := importATimeLogger(db, cfg)
	result.record(base, "atl_interval", "atimelogger", atlRows, atlRejected, atlErr, db, cfg.ATimeLoggerDB)

	// VACUUM for compact size
	db.Exec("VACUUM")

	// Fold the WAL back into the file before anything renames it: the sidecars are
	// not moved with it, and a DB promoted without its WAL is a DB missing whatever
	// the WAL still held.
	db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("close candidate: %w", err)
	}

	// Promote only a sound run. A warning means the live DB — which still has the
	// stream this run lost — stays exactly where it is. Nothing to protect (no live
	// DB yet) is the one case where a warning still promotes: some data beats none,
	// and the warning is on the record either way.
	live := dbExists(cfg)
	switch {
	case result.Status == statusOK || !live:
		if err := promoteDB(candidate, path); err != nil {
			return nil, fmt.Errorf("promote: %w", err)
		}
		result.DBPath = path
	default:
		result.DBPath = path // untouched, and still the good one
		result.CandidatePath = candidate
		result.warn("live DB left untouched — this run was not promoted. " +
			"The candidate is kept for inspection: " + candidate)
	}

	if info, _ := os.Stat(result.promotedPath()); info != nil {
		result.DBSizeMB = float64(info.Size()) / (1024 * 1024)
	}
	result.Duration = time.Since(result.StartAt).String()

	return result, nil
}

// promotedPath is the file the result's numbers describe: the live DB when the run
// was promoted, the candidate when it was held back.
func (r *ImportResult) promotedPath() string {
	if r.CandidatePath != "" {
		return r.CandidatePath
	}
	return r.DBPath
}

// promoteDB swaps the candidate into place. The old sidecars must go with the old
// file — a -wal left behind belongs to a database that no longer exists, and SQLite
// would read it as if it did.
func promoteDB(candidate, path string) error {
	if err := removeDB(path); err != nil {
		return err
	}
	return os.Rename(candidate, path)
}

// removeDB deletes a SQLite database and its sidecars. A remove that fails is not
// swallowed: it means the next step would build on a file we do not control.
func removeDB(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ImportResult leads with the verdict. A caller that reads one field reads
// status, so status must be the field that knows about the loss — not total_rows,
// which is happy to be smaller, and not the per-table "ok" that used to mean
// nothing worse than "we opened a file".
type ImportResult struct {
	DBPath string `json:"db_path"`
	Status string `json:"status"` // "ok" | "warning"
	// CandidatePath is set only when the run was NOT promoted: db_path still holds
	// the previous, sound database, and this is the file that was built and rejected.
	CandidatePath string `json:"candidate_path,omitempty"`
	// Never omitempty, never nil — same reason as DBStatus.Warnings: [] says the
	// run was examined and nothing was lost, an absent key says nothing at all.
	// (CandidatePath above keeps omitempty on purpose: its absence carries the
	// meaning "promoted".)
	Warnings []string `json:"warnings"`
	// Rejected is what the run refused on purpose: rows whose timestamp is a
	// placeholder, not a measurement. Never omitempty, never nil — a run that
	// threw rows away and did not say so is the same silence as one that lost
	// them. It is a separate channel from warnings because a refusal is not a
	// failure: warnings block promotion, an accounted-for refusal does not.
	Rejected      []string      `json:"rejected"`
	Note          string        `json:"note,omitempty"`
	BaselineAt    string        `json:"baseline_at,omitempty"`
	Tables        []TableResult `json:"tables"`
	TotalRows     int           `json:"total_rows"`
	PrevTotalRows int           `json:"prev_total_rows,omitempty"`
	ImportID      int           `json:"import_id"` // this run's ledger id
	DBSizeMB      float64       `json:"db_size_mb"`
	Duration      string        `json:"duration"`
	StartAt       time.Time     `json:"-"`

	runID int
	runAt string
}

// warn records a problem and makes the verdict say so. Every path that notices
// something wrong goes through here, so that no caller can find the trouble only
// by reading the warnings and none by reading the status.
func (r *ImportResult) warn(msg string) {
	r.Warnings = append(r.Warnings, msg)
	r.Status = statusWarn
}

// TableResult carries the previous count next to the new one. prev_rows/delta are
// pointers so that "no baseline for this stream" and "unchanged" cannot be
// confused: absent means nothing to compare, 0 means nothing changed.
type TableResult struct {
	Name     string `json:"name"`
	Rows     int    `json:"rows"`
	Status   string `json:"status"`
	PrevRows *int   `json:"prev_rows,omitempty"`
	Delta    *int   `json:"delta,omitempty"`
	// Rejected rows are not in Rows: the count here is what was refused, so
	// rows + rejected is what the source offered.
	Rejected int `json:"rejected,omitempty"`
}

// record judges one stream against the ledger, files it in the result, and writes
// it back to the ledger — including a zero. A zero that goes unrecorded is a loss
// that goes unnoticed next time.
func (r *ImportResult) record(base *importBaseline, name, source string, rows, rejected int, err error, db *sql.DB, sourcePath string) {
	status, warning := base.classify(name, rows, rejected, err)

	t := TableResult{Name: name, Rows: rows, Status: status, Rejected: rejected}
	if prev, ok := base.Prev[name]; ok {
		delta := rows - prev
		t.PrevRows = &prev
		t.Delta = &delta
	}
	r.Tables = append(r.Tables, t)
	r.TotalRows += rows

	// Said out loud, every run, whether or not anything else went wrong. The row
	// is gone from the DB either way; the only question is whether the tool admits
	// it. This does not set the status — a refusal we can account for is not a
	// failure, and blocking promotion on it would jam the import permanently.
	if rejected > 0 {
		r.Rejected = append(r.Rejected, fmt.Sprintf(
			"%s: %s rows rejected — timestamp before %s, a placeholder the export ships for 'unknown' (not a measurement)",
			name, comma(rejected), sentinelFloor.Format("2006-01-02")))
	}

	if warning != "" {
		r.warn(warning)
	}

	// The ledger entry is not bookkeeping we can afford to lose. A row that fails
	// to land is a stream this DB cannot vouch for next time — so the run says so,
	// and is not promoted on the strength of a record it failed to write.
	if err := logImport(db, r.runID, r.runAt, source, name, rows, sourcePath); err != nil {
		r.warn(fmt.Sprintf("%s: ledger write failed — not promoting: %v", name, err))
	}
}

// --- Samsung Health importers ---

// sentinelFloor is the date below which a timestamp is a placeholder, not a
// measurement. The Samsung export ships rows stamped 1970-01-01 (epoch zero) and
// 2000-01-01 where it never actually knew the time. Imported as if real, they
// become heart-rate days decades before the watch existed: true in the database
// and false in the world. `heart --to 2001-01-01` dutifully reported them.
//
// The floor sits far below the oldest genuine record (2017-03-04) and far above
// the placeholders, so it can only ever catch placeholders. If a real 2009 record
// ever appears, this is the one line to move — and it will announce itself,
// because the row will be reported as rejected rather than vanish.
//
// Rejected, not dropped: every run says how many it refused, per stream. A tool
// that quietly discards rows is the same silence as one that quietly loses them.
var defaultSentinelFloor = time.Date(2010, 1, 1, 0, 0, 0, 0, KST)

// sentinelFloor is a var only so a test can stand in for the older, unfiltered
// binary. Production never moves it.
var sentinelFloor = defaultSentinelFloor

func isSentinelTime(t time.Time) bool { return t.Before(sentinelFloor) }

func importSleep(db *sql.DB, cfg *Config) (int, int, error) {
	path := cfg.shealthCSV("com.samsung.shealth.sleep.")
	if path == "" {
		return 0, 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO sleep
		(id, uuid, start_time, end_time, duration_min, sleep_score, efficiency, total_light_min, total_rem_min)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count, rejected := 0, 0
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
		if isSentinelTime(start) {
			rejected++
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
	return count, rejected, nil
}

func importSleepStage(db *sql.DB, cfg *Config) (int, int, error) {
	path := cfg.shealthCSV("com.samsung.health.sleep_stage.")
	if path == "" {
		return 0, 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO sleep_stage
		(id, sleep_uuid, start_time, end_time, stage)
		VALUES (?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count, rejected := 0, 0
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
		if isSentinelTime(start) {
			rejected++
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
	return count, rejected, nil
}

func importHeartRate(db *sql.DB, cfg *Config) (int, int, error) {
	path := cfg.shealthCSV("com.samsung.shealth.tracker.heart_rate.")
	if path == "" {
		return 0, 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO heart_rate (id, start_time, heart_rate) VALUES (?, ?, ?)`)
	defer stmt.Close()

	count, rejected := 0, 0
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
		if isSentinelTime(start) {
			rejected++
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
	return count, rejected, nil
}

func importStepsDaily(db *sql.DB, cfg *Config) (int, int, error) {
	path := cfg.shealthCSV("com.samsung.shealth.step_daily_trend.")
	if path == "" {
		return 0, 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO steps_daily
		(id, date, day_time_ms, count, distance, calorie) VALUES (?, ?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count, rejected := 0, 0
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
		if d, err := time.ParseInLocation("2006-01-02", date, KST); err == nil && isSentinelTime(d) {
			rejected++
			continue
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
	return count, rejected, nil
}

func importStress(db *sql.DB, cfg *Config) (int, int, error) {
	path := cfg.shealthCSV("com.samsung.shealth.stress.")
	if path == "" {
		return 0, 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO stress
		(id, start_time, score, min_score, max_score) VALUES (?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count, rejected := 0, 0
	for _, rec := range records {
		startStr := rec["start_time"]
		scoreStr := rec["score"]
		if startStr == "" || scoreStr == "" {
			continue
		}

		// Parsed for every row, not only the ones missing a uuid: the timestamp
		// has to be judged before the row lands, and a row with a uuid can carry
		// a placeholder time just as easily as one without.
		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}
		if isSentinelTime(start) {
			rejected++
			continue
		}

		id := rec["datauuid"]
		if id == "" {
			id = denoteID(start)
		}

		stmt.Exec(id, startStr,
			parseFloat(scoreStr),
			parseFloat(rec["min"]),
			parseFloat(rec["max"]))
		count++
	}
	tx.Commit()
	return count, rejected, nil
}

func importExercise(db *sql.DB, cfg *Config) (int, int, error) {
	// Find exact exercise CSV (not photo/program variants)
	matches, _ := filepath.Glob(filepath.Join(cfg.ShealthDir, "com.samsung.shealth.exercise.2*.csv"))
	if len(matches) == 0 {
		return 0, 0, fmt.Errorf("csv not found")
	}
	path := matches[0]

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO exercise
		(id, start_time, end_time, exercise_type, duration_ms, calorie, mean_hr, max_hr, distance)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count, rejected := 0, 0
	for _, rec := range records {
		startStr := rec["com.samsung.health.exercise.start_time"]
		if startStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}
		if isSentinelTime(start) {
			rejected++
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
	return count, rejected, nil
}

func importWeight(db *sql.DB, cfg *Config) (int, int, error) {
	path := cfg.shealthCSV("com.samsung.health.weight.")
	if path == "" {
		return 0, 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO weight
		(id, start_time, weight, body_fat, muscle_mass) VALUES (?, ?, ?, ?, ?)`)
	defer stmt.Close()

	count, rejected := 0, 0
	for _, rec := range records {
		startStr := rec["start_time"]
		if startStr == "" {
			continue
		}

		start, err := parseShealthTime(startStr)
		if err != nil {
			continue
		}
		if isSentinelTime(start) {
			rejected++
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
	return count, rejected, nil
}

func importHRV(db *sql.DB, cfg *Config) (int, int, error) {
	path := cfg.shealthCSV("com.samsung.health.hrv.")
	if path == "" {
		return 0, 0, fmt.Errorf("csv not found")
	}

	_, records, err := shealthReadCSV(path)
	if err != nil {
		return 0, 0, err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT OR IGNORE INTO hrv (id, start_time, hrv_rmssd) VALUES (?, ?, ?)`)
	defer stmt.Close()

	count, rejected := 0, 0
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
		if isSentinelTime(start) {
			rejected++
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
	return count, rejected, nil
}

// --- aTimeLogger importer ---

func importATimeLogger(db *sql.DB, cfg *Config) (int, int, error) {
	if _, err := os.Stat(cfg.ATimeLoggerDB); err != nil {
		return 0, 0, fmt.Errorf("atimelogger db not found: %s", cfg.ATimeLoggerDB)
	}

	srcDB, err := sql.Open("sqlite", cfg.ATimeLoggerDB)
	if err != nil {
		return 0, 0, fmt.Errorf("open atimelogger: %w", err)
	}
	defer srcDB.Close()

	// Import categories
	rows, err := srcDB.Query(`SELECT id, name, color, is_group, parent_id FROM activity_type`)
	if err != nil {
		return 0, 0, err
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
		return 0, 0, err
	}
	defer iRows.Close()

	intStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO atl_interval
		(id, guid, start_time, end_time, comment, category_id, is_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)

	count, rejected := 0, 0
	for iRows.Next() {
		var id, start, finish, catID, isDeleted int
		var guid, comment sql.NullString
		iRows.Scan(&id, &guid, &start, &finish, &comment, &catID, &isDeleted)
		// aTimeLogger stores unix seconds; a zero start is epoch, not a block.
		if isSentinelTime(time.Unix(int64(start), 0)) {
			rejected++
			continue
		}
		intStmt.Exec(id, guid.String, start, finish, comment.String, catID, isDeleted)
		count++
	}
	intStmt.Close()
	tx.Commit()

	return count, rejected, nil
}

// parseInt, parseFloat, firstNonEmpty → helpers.go
