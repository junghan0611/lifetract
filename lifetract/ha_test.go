package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockHA returns a test HA server + client bound to it. Pass per-path handlers.
func mockHA(t *testing.T, routes map[string]string) (*httptest.Server, *HAClient) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &HAClient{
		BaseURL: srv.URL,
		Token:   "test-token",
		HTTP:    srv.Client(),
	}
}

func TestHAPing(t *testing.T) {
	_, c := mockHA(t, map[string]string{
		"/api/": `{"message":"API running."}`,
	})
	if err := c.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestHAPingBadResponse(t *testing.T) {
	_, c := mockHA(t, map[string]string{
		"/api/": `{"message":"nope"}`,
	})
	if err := c.Ping(); err == nil {
		t.Fatal("expected error on unexpected ping response")
	}
}

func TestHAGetState(t *testing.T) {
	body := `{
		"entity_id": "sensor.sm_s942n_s26_glgman_sleep_duration",
		"state": "415.0",
		"attributes": {"unit_of_measurement": "min"},
		"last_changed": "2026-05-17T19:53:44.674190+00:00",
		"last_updated": "2026-05-17T19:53:44.674190+00:00"
	}`
	_, c := mockHA(t, map[string]string{
		"/api/states/sensor.sm_s942n_s26_glgman_sleep_duration": body,
	})
	s, err := c.GetState("sensor.sm_s942n_s26_glgman_sleep_duration")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if v, ok := s.FloatValue(); !ok || v != 415.0 {
		t.Errorf("FloatValue = (%v, %v), want (415, true)", v, ok)
	}
	if s.Unit() != "min" {
		t.Errorf("Unit = %q, want %q", s.Unit(), "min")
	}
}

func TestHAGetStateUnknown(t *testing.T) {
	body := `{
		"entity_id": "sensor.foo",
		"state": "unknown",
		"attributes": {},
		"last_changed": "2026-05-17T19:53:44Z",
		"last_updated": "2026-05-17T19:53:44Z"
	}`
	_, c := mockHA(t, map[string]string{"/api/states/sensor.foo": body})
	s, err := c.GetState("sensor.foo")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.FloatValue(); ok {
		t.Error("unknown state should not parse as float")
	}
}

func TestHAGetAllStates(t *testing.T) {
	body := `[
		{"entity_id":"sensor.a","state":"1.0","attributes":{}},
		{"entity_id":"sensor.b","state":"2.0","attributes":{}}
	]`
	_, c := mockHA(t, map[string]string{"/api/states": body})
	all, err := c.GetAllStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d states, want 2", len(all))
	}
}

func TestHAUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &HAClient{BaseURL: srv.URL, Token: "bad", HTTP: srv.Client()}
	err := c.Ping()
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got: %v", err)
	}
}

// --- entity registry ---

func TestEntityByID(t *testing.T) {
	e, ok := EntityByID("sensor.sm_s942n_s26_glgman_sleep_duration")
	if !ok {
		t.Fatal("expected sleep_duration entity to be registered")
	}
	if e.Kind != KindSleepDuration {
		t.Errorf("Kind = %q, want %q", e.Kind, KindSleepDuration)
	}
	if e.Unit != "min" {
		t.Errorf("Unit = %q, want %q", e.Unit, "min")
	}
}

func TestEntityByIDUnknown(t *testing.T) {
	if _, ok := EntityByID("sensor.does.not.exist"); ok {
		t.Error("expected not-registered entity to return false")
	}
}

func TestResolveEntityRefByKind(t *testing.T) {
	id, ok := ResolveEntityRef("heart_rate")
	if !ok {
		t.Fatal("kind 'heart_rate' should resolve")
	}
	if id != "sensor.sm_s942n_s26_glgman_heart_rate" {
		t.Errorf("got %q", id)
	}
}

func TestResolveEntityRefByEntityID(t *testing.T) {
	id, ok := ResolveEntityRef("sensor.sm_s942n_s26_glgman_weight")
	if !ok {
		t.Fatal("entity_id should resolve to itself")
	}
	if id != "sensor.sm_s942n_s26_glgman_weight" {
		t.Errorf("got %q", id)
	}
}

func TestEntitiesByKindBuilt(t *testing.T) {
	if len(EntitiesByKind[KindSleepDuration]) == 0 {
		t.Error("KindSleepDuration should have at least one registered entity")
	}
	if len(EntitiesByKind[KindHeartRate]) == 0 {
		t.Error("KindHeartRate should have at least one registered entity")
	}
}

// --- cmd surface ---

func TestCmdHAPingViaDispatcher(t *testing.T) {
	_, c := mockHA(t, map[string]string{"/api/": `{"message":"API running."}`})
	// inject by calling haPing directly — full cmdHA path requires NewHAClient
	// (which loads real tokens). The dispatcher logic is exercised in
	// TestCmdHAUnknownSub below.
	out, err := haPing(c)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	if m["ok"] != true {
		t.Errorf("ok = %v", m["ok"])
	}
}

func TestCmdHAStateAnnotatesKind(t *testing.T) {
	body := `{
		"entity_id":"sensor.sm_s942n_s26_glgman_heart_rate",
		"state":"72.0",
		"attributes":{"unit_of_measurement":"bpm"},
		"last_changed":"2026-05-17T19:53:44Z",
		"last_updated":"2026-05-17T19:53:44Z"
	}`
	_, c := mockHA(t, map[string]string{
		"/api/states/sensor.sm_s942n_s26_glgman_heart_rate": body,
	})
	out, err := haState(c, "heart_rate")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := out.(HAStateResult)
	if !ok {
		t.Fatalf("expected HAStateResult, got %T", out)
	}
	if r.Kind != string(KindHeartRate) {
		t.Errorf("Kind = %q, want %q", r.Kind, KindHeartRate)
	}
	if r.Value == nil || *r.Value != 72.0 {
		t.Errorf("Value = %v, want 72.0", r.Value)
	}
}

// --- history ---

func TestHAGetHistory(t *testing.T) {
	// HA returns [[HAState, HAState, ...]] — outer = per entity, inner = chronological state changes
	body := `[[
		{"entity_id":"sensor.s","state":"427.0","attributes":{"unit_of_measurement":"min"},"last_changed":"2026-05-17T11:56:58Z","last_updated":"2026-05-17T11:56:58Z"},
		{"entity_id":"sensor.s","state":"415.0","attributes":{"unit_of_measurement":"min"},"last_changed":"2026-05-17T19:53:44Z","last_updated":"2026-05-17T19:53:44Z"}
	]]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Path must start with /api/history/period/
		if !strings.HasPrefix(r.URL.Path, "/api/history/period/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Query must include filter_entity_id and end_time
		if r.URL.Query().Get("filter_entity_id") == "" || r.URL.Query().Get("end_time") == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	c := &HAClient{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()}
	end := time.Now()
	start := end.AddDate(0, 0, -7)
	states, err := c.GetHistory("sensor.s", start, end)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("got %d states, want 2", len(states))
	}
	if v, ok := states[0].FloatValue(); !ok || v != 427.0 {
		t.Errorf("first state value = (%v, %v), want 427", v, ok)
	}
}

func TestHAGetHistoryEmpty(t *testing.T) {
	// Sensor exists but has no history yet
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	c := &HAClient{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()}
	end := time.Now()
	states, err := c.GetHistory("sensor.s", end.AddDate(0, 0, -1), end)
	if err != nil {
		t.Fatalf("GetHistory empty: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("expected 0 states for empty series, got %d", len(states))
	}
}

func TestCmdHAHistoryShapesPoints(t *testing.T) {
	body := `[[
		{"entity_id":"sensor.sm_s942n_s26_glgman_sleep_duration","state":"427.0","attributes":{"unit_of_measurement":"min"},"last_changed":"2026-05-17T11:56:58Z","last_updated":"2026-05-17T11:56:58Z"},
		{"entity_id":"sensor.sm_s942n_s26_glgman_sleep_duration","state":"415.0","attributes":{"unit_of_measurement":"min"},"last_changed":"2026-05-17T19:53:44Z","last_updated":"2026-05-17T19:53:44Z"}
	]]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	c := &HAClient{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()}
	w := Window{
		From: time.Date(2026, 5, 17, 0, 0, 0, 0, KST),
		To:   time.Date(2026, 5, 19, 0, 0, 0, 0, KST),
	}
	out, err := haHistory(c, "sleep_duration", w)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := out.(HAHistoryResult)
	if !ok {
		t.Fatalf("expected HAHistoryResult, got %T", out)
	}
	if r.Count != 2 || len(r.Points) != 2 {
		t.Errorf("Count=%d, len(Points)=%d, want 2/2", r.Count, len(r.Points))
	}
	if r.Kind != string(KindSleepDuration) {
		t.Errorf("Kind = %q, want %q", r.Kind, KindSleepDuration)
	}
	if r.Days != 2 {
		t.Errorf("Days = %d, want 2", r.Days)
	}
	if r.From != "2026-05-17T00:00:00+09:00" || r.To != "2026-05-19T00:00:00+09:00" {
		t.Errorf("window = %s .. %s, want KST midnights", r.From, r.To)
	}
	if r.Points[0].Value == nil || *r.Points[0].Value != 427.0 {
		t.Errorf("first point value = %v, want 427", r.Points[0].Value)
	}
}

func TestCmdHAAllKnownStatesMarksMissing(t *testing.T) {
	// Return only one known sensor; the rest should be marked missing.
	body := `[
		{"entity_id":"sensor.sm_s942n_s26_glgman_heart_rate","state":"72.0","attributes":{"unit_of_measurement":"bpm"}}
	]`
	_, c := mockHA(t, map[string]string{"/api/states": body})
	out, err := haAllKnownStates(c)
	if err != nil {
		t.Fatal(err)
	}
	results, ok := out.([]HAStateResult)
	if !ok {
		t.Fatalf("expected []HAStateResult, got %T", out)
	}
	if len(results) != len(KnownEntities) {
		t.Errorf("got %d, want %d known entities", len(results), len(KnownEntities))
	}
	var foundHR, foundMissing bool
	for _, r := range results {
		if r.Kind == string(KindHeartRate) && r.Value != nil && *r.Value == 72.0 {
			foundHR = true
		}
		if r.State == "missing" {
			foundMissing = true
		}
	}
	if !foundHR {
		t.Error("heart_rate result missing")
	}
	if !foundMissing {
		t.Error("at least one entity should be marked missing")
	}
}

func TestHAHistoryDropsPointOutsideWindow(t *testing.T) {
	body := `[[
		{"entity_id":"sensor.s","state":"1","attributes":{},"last_changed":"2026-05-16T14:59:00Z","last_updated":"2026-05-16T14:59:00Z"},
		{"entity_id":"sensor.s","state":"2","attributes":{},"last_changed":"2026-05-17T00:00:00+09:00","last_updated":"2026-05-17T00:00:00+09:00"},
		{"entity_id":"sensor.s","state":"3","attributes":{},"last_changed":"2026-05-18T00:00:00+09:00","last_updated":"2026-05-18T00:00:00+09:00"}
	]]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	c := &HAClient{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()}
	w := Window{
		From: time.Date(2026, 5, 17, 0, 0, 0, 0, KST),
		To:   time.Date(2026, 5, 18, 0, 0, 0, 0, KST),
	}
	out, err := haHistory(c, "sensor.s", w)
	if err != nil {
		t.Fatal(err)
	}
	r := out.(HAHistoryResult)
	if r.Count != 1 || r.Points[0].State != "2" {
		t.Fatalf("kept %d points state=%v, want the inclusive start only", r.Count, r.Points)
	}
}

func TestHAHistoryRequestsWindowNotNow(t *testing.T) {
	wantFrom := time.Date(2026, 9, 25, 0, 0, 0, 0, KST)
	wantTo := time.Date(2026, 10, 2, 0, 0, 0, 0, KST)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/history/period/2026-09-24T15:00:00") {
			t.Errorf("path = %s, want KST 2026-09-25 midnight as UTC", r.URL.Path)
		}
		if got := r.URL.Query().Get("end_time"); got != "2026-10-01T15:00:00+00:00" {
			t.Errorf("end_time = %q, want exclusive KST midnight in UTC", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	c := &HAClient{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()}
	if _, err := haHistory(c, "sensor.s", Window{From: wantFrom, To: wantTo}); err != nil {
		t.Fatal(err)
	}
}

func TestHAEntitiesDomainFilter(t *testing.T) {
	body := `[{"entity_id":"sensor.a","state":"1","attributes":{}},{"entity_id":"person.b","state":"home","attributes":{}}]`
	_, c := mockHA(t, map[string]string{"/api/states": body})
	out, err := haAllEntities(c, "sensor")
	if err != nil {
		t.Fatal(err)
	}
	rows := out.([]HAEntityInfo)
	if len(rows) != 1 || rows[0].EntityID != "sensor.a" {
		t.Fatalf("got %#v", rows)
	}
	if _, err := haAllEntities(c, "weather"); err == nil || !strings.Contains(err.Error(), "weather") {
		t.Fatalf("unknown domain should fail, got %v", err)
	}
}

func TestHALogbookFiltersWindow(t *testing.T) {
	body := `[
		{"entity_id":"sensor.a","message":"before","when":"2026-05-16T14:00:00Z","state":"1"},
		{"entity_id":"sensor.a","message":"inside","when":"2026-05-17T01:00:00Z","state":"2"},
		{"entity_id":"sensor.a","message":"at-end","when":"2026-05-18T00:00:00+09:00","state":"3"}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/logbook/") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("entity") != "sensor.a" {
			t.Errorf("entity = %q", r.URL.Query().Get("entity"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	c := &HAClient{BaseURL: srv.URL, Token: "test-token", HTTP: srv.Client()}
	w := Window{
		From: time.Date(2026, 5, 17, 0, 0, 0, 0, KST),
		To:   time.Date(2026, 5, 18, 0, 0, 0, 0, KST),
	}
	out, err := haLogbook(c, "sensor.a", w)
	if err != nil {
		t.Fatal(err)
	}
	r := out.(HALogbookResult)
	if r.Count != 1 || r.Entries[0].Message != "inside" {
		t.Fatalf("kept %#v, want the inside row only", r.Entries)
	}
	if r.Days != 1 {
		t.Errorf("Days = %d, want 1", r.Days)
	}
}

func TestCheckHAFlags(t *testing.T) {
	if err := checkHAFlags("ping", map[string]string{"days": "7"}); err == nil {
		t.Fatal("ha ping --days must be refused")
	}
	if err := checkHAFlags("history", map[string]string{"days": "7", "from": "2026-10-01"}); err != nil {
		t.Fatal(err)
	}
	if err := checkHAFlags("entities", map[string]string{"domain": "sensor"}); err != nil {
		t.Fatal(err)
	}
	if err := checkHAFlags("history", map[string]string{"domain": "sensor"}); err == nil {
		t.Fatal("ha history --domain must be refused")
	}
	if err := checkFlagsFor("ha", map[string]string{"days": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := checkFlagsFor("heart", map[string]string{"domain": "sensor"}); err == nil {
		t.Fatal("heart --domain must be refused")
	}
}
