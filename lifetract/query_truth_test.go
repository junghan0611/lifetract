package main

import (
	"testing"
)

// A stream that cannot answer must not be folded into the answer.
//
// The aggregators used to call six queries and drop every error into `_`. Drop a
// table and `timeline` still returned a list — one stream short, shaped exactly
// like a stretch of quiet days. The collector downstream writes that into the
// record as a zero, and the hole becomes a fact about a life.
//
// A reviewer found these two after the leaf queries were already fixed: the leaves
// reported honestly and the aggregator above them threw the report away.
func TestAggregatorsDoNotSwallowABrokenStream(t *testing.T) {
	cfg, _ := lossCfg(t)
	if _, err := execImport(cfg); err != nil {
		t.Fatal(err)
	}

	db, err := openDB(dbPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE heart_rate`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	day, err := parseDenoteID("20250115T000000")
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]func() (interface{}, error){
		"timeline": func() (interface{}, error) { return cmdTimeline(cfg) },
		"read":     func() (interface{}, error) { return dbQueryDay(cfg, day) },
		"today":    func() (interface{}, error) { return cmdToday(cfg) },
	}

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := run()
			if err == nil {
				t.Errorf("%s = %#v with heart_rate gone — a missing stream came back as a quiet day", name, result)
			}
		})
	}
}
