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
	out, err := haHistory(c, "sleep_duration", 7)
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
	if r.Days != 7 {
		t.Errorf("Days = %d, want 7", r.Days)
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
