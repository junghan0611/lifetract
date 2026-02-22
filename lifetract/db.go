package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const dbFileName = "lifetract.db"

// dbPath returns the path to the lifetract database.
func dbPath(cfg *Config) string {
	return filepath.Join(cfg.DataDir, dbFileName)
}

// openDB opens or creates the lifetract database.
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// Performance pragmas
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA synchronous=NORMAL")
	db.Exec("PRAGMA cache_size=-64000") // 64MB
	return db, nil
}

// initSchema creates all tables if they don't exist.
func initSchema(db *sql.DB) error {
	schema := `
	-- Samsung Health: sleep sessions
	CREATE TABLE IF NOT EXISTS sleep (
		id TEXT PRIMARY KEY,           -- denote ID (YYYYMMDDTHHMMSS)
		uuid TEXT,                     -- original Samsung UUID
		start_time TEXT NOT NULL,      -- "2025-01-15 23:30:00.000"
		end_time TEXT NOT NULL,
		duration_min REAL,
		sleep_score INTEGER,
		efficiency REAL,
		total_light_min REAL,
		total_rem_min REAL,
		source TEXT DEFAULT 'samsung_health'
	);

	-- Samsung Health: sleep stages
	CREATE TABLE IF NOT EXISTS sleep_stage (
		id TEXT PRIMARY KEY,
		sleep_uuid TEXT NOT NULL,      -- references sleep.uuid
		start_time TEXT NOT NULL,
		end_time TEXT NOT NULL,
		stage INTEGER NOT NULL,        -- 40001=Awake, 40002=Light, 40003=Deep, 40004=REM
		source TEXT DEFAULT 'samsung_health'
	);

	-- Samsung Health: heart rate
	CREATE TABLE IF NOT EXISTS heart_rate (
		id TEXT PRIMARY KEY,           -- denote ID
		start_time TEXT NOT NULL,
		heart_rate REAL NOT NULL,
		source TEXT DEFAULT 'samsung_health'
	);

	-- Samsung Health: daily steps
	CREATE TABLE IF NOT EXISTS steps_daily (
		id TEXT PRIMARY KEY,           -- denote day ID
		date TEXT NOT NULL,            -- YYYY-MM-DD
		day_time_ms INTEGER,           -- epoch millis
		count INTEGER NOT NULL,
		distance REAL,
		calorie REAL,
		source TEXT DEFAULT 'samsung_health'
	);

	-- Samsung Health: stress
	CREATE TABLE IF NOT EXISTS stress (
		id TEXT PRIMARY KEY,
		start_time TEXT NOT NULL,
		score REAL,
		min_score REAL,
		max_score REAL,
		source TEXT DEFAULT 'samsung_health'
	);

	-- Samsung Health: exercise
	CREATE TABLE IF NOT EXISTS exercise (
		id TEXT PRIMARY KEY,           -- denote ID
		start_time TEXT NOT NULL,
		end_time TEXT,
		exercise_type INTEGER,
		duration_ms INTEGER,
		calorie REAL,
		mean_hr REAL,
		max_hr REAL,
		distance REAL,
		source TEXT DEFAULT 'samsung_health'
	);

	-- Samsung Health: weight
	CREATE TABLE IF NOT EXISTS weight (
		id TEXT PRIMARY KEY,
		start_time TEXT NOT NULL,
		weight REAL,
		body_fat REAL,
		muscle_mass REAL,
		source TEXT DEFAULT 'samsung_health'
	);

	-- Samsung Health: HRV
	CREATE TABLE IF NOT EXISTS hrv (
		id TEXT PRIMARY KEY,
		start_time TEXT NOT NULL,
		hrv_rmssd REAL,
		source TEXT DEFAULT 'samsung_health'
	);

	-- aTimeLogger: categories (original, no remapping)
	CREATE TABLE IF NOT EXISTS atl_category (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		color INTEGER,
		is_group INTEGER DEFAULT 0,
		parent_id INTEGER DEFAULT 0
	);

	-- aTimeLogger: time intervals (all records, no filtering)
	CREATE TABLE IF NOT EXISTS atl_interval (
		id INTEGER PRIMARY KEY,
		guid TEXT,
		start_time INTEGER NOT NULL,   -- unix epoch seconds
		end_time INTEGER NOT NULL,
		comment TEXT,
		category_id INTEGER NOT NULL,
		is_deleted INTEGER DEFAULT 0,
		FOREIGN KEY(category_id) REFERENCES atl_category(id)
	);

	-- Import metadata
	CREATE TABLE IF NOT EXISTS import_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		imported_at TEXT NOT NULL,
		source TEXT NOT NULL,
		table_name TEXT NOT NULL,
		rows_imported INTEGER,
		source_path TEXT
	);

	-- Indexes
	CREATE INDEX IF NOT EXISTS idx_sleep_start ON sleep(start_time);
	CREATE INDEX IF NOT EXISTS idx_hr_start ON heart_rate(start_time);
	CREATE INDEX IF NOT EXISTS idx_stress_start ON stress(start_time);
	CREATE INDEX IF NOT EXISTS idx_exercise_start ON exercise(start_time);
	CREATE INDEX IF NOT EXISTS idx_sleep_stage_uuid ON sleep_stage(sleep_uuid);
	CREATE INDEX IF NOT EXISTS idx_steps_date ON steps_daily(date);
	CREATE INDEX IF NOT EXISTS idx_atl_start ON atl_interval(start_time);
	CREATE INDEX IF NOT EXISTS idx_atl_cat ON atl_interval(category_id);
	CREATE INDEX IF NOT EXISTS idx_atl_deleted ON atl_interval(is_deleted);
	`
	_, err := db.Exec(schema)
	return err
}

// logImport records an import operation.
func logImport(db *sql.DB, source, tableName string, rows int, sourcePath string) {
	db.Exec(`INSERT INTO import_log (imported_at, source, table_name, rows_imported, source_path)
		VALUES (?, ?, ?, ?, ?)`,
		time.Now().Format(time.RFC3339), source, tableName, rows, sourcePath)
}

// dbExists checks if the lifetract.db exists.
func dbExists(cfg *Config) bool {
	_, err := os.Stat(dbPath(cfg))
	return err == nil
}
