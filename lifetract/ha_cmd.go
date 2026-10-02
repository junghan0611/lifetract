package main

// CLI surface for HA REST. Phase 3 keeps these read-only — no DB writes yet.
// Phase 4 wires the same client into cmdToday / cmdRead as a lazy fallback.

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Flags each ha subcommand actually reads. The union is allowed on "ha" by
// commandFlags; this map is what stops `ha ping --days 7` from being accepted
// and ignored.
var haSubFlags = map[string]map[string]bool{
	"ping":     {},
	"state":    {},
	"states":   {},
	"entities": {"domain": true},
	"history":  {"days": true, "from": true, "to": true},
	"logbook":  {"days": true, "from": true, "to": true},
}

// checkHAFlags rejects a flag this ha subcommand would ignore.
// An unknown subcommand is left to cmdHA — a flag error would hide that.
func checkHAFlags(sub string, flags map[string]string) error {
	if sub == "" {
		sub = "ping"
	}
	allowed, known := haSubFlags[sub]
	if !known {
		return nil
	}
	for f := range flags {
		if globalFlags[f] || allowed[f] {
			continue
		}
		return fmt.Errorf("--%s means nothing to \"ha %s\" — it would be ignored", f, sub)
	}
	return nil
}

// HAEntityInfo is the JSON shape returned for entity listings.
type HAEntityInfo struct {
	EntityID string `json:"entity_id"`
	Kind     string `json:"kind,omitempty"`
	Unit     string `json:"unit,omitempty"`
	State    string `json:"state,omitempty"`
	Known    bool   `json:"known"`
}

// HAStateResult mirrors the HA /api/states/<entity_id> response with the
// lifetract Kind annotation attached when known.
type HAStateResult struct {
	EntityID    string                 `json:"entity_id"`
	Kind        string                 `json:"kind,omitempty"`
	State       string                 `json:"state"`
	Value       *float64               `json:"value,omitempty"`
	Unit        string                 `json:"unit,omitempty"`
	LastChanged string                 `json:"last_changed,omitempty"`
	LastUpdated string                 `json:"last_updated,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
}

// cmdHA dispatches `lifetract ha <sub> [arg]`.
func cmdHA(cfg *Config, sub string, arg string) (interface{}, error) {
	client, err := NewHAClient()
	if err != nil {
		return nil, err
	}

	switch sub {
	case "", "ping":
		return haPing(client)
	case "state":
		if arg == "" {
			return nil, fmt.Errorf("ha state: kind or entity_id required (e.g. 'sleep_duration' or 'sensor.foo')")
		}
		return haState(client, arg)
	case "states":
		return haAllKnownStates(client)
	case "entities":
		return haAllEntities(client, cfg.Domain)
	case "history":
		if arg == "" {
			return nil, fmt.Errorf("ha history: kind or entity_id required (e.g. 'sleep_duration')")
		}
		return haHistory(client, arg, cfg.queryWindow())
	case "logbook":
		return haLogbook(client, arg, cfg.queryWindow())
	default:
		return nil, fmt.Errorf("unknown ha subcommand: %q (ping|state|states|entities|history|logbook)", sub)
	}
}

func haPing(c *HAClient) (interface{}, error) {
	if err := c.Ping(); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"base_url": c.BaseURL,
		"ok":       true,
		"message":  "API running.",
	}, nil
}

func haState(c *HAClient, ref string) (interface{}, error) {
	entityID, ok := ResolveEntityRef(ref)
	if !ok {
		entityID = ref
	}
	s, err := c.GetState(entityID)
	if err != nil {
		return nil, err
	}
	return toStateResult(s), nil
}

// haAllKnownStates fetches /api/states once and returns just the entities
// lifetract has registered, keeping the response compact.
func haAllKnownStates(c *HAClient) (interface{}, error) {
	all, err := c.GetAllStates()
	if err != nil {
		return nil, err
	}
	known := make(map[string]HAState, len(all))
	for _, s := range all {
		if _, ok := EntityByID(s.EntityID); ok {
			known[s.EntityID] = s
		}
	}
	out := make([]HAStateResult, 0, len(KnownEntities))
	for _, e := range KnownEntities {
		s, present := known[e.EntityID]
		if !present {
			out = append(out, HAStateResult{
				EntityID: e.EntityID,
				Kind:     string(e.Kind),
				Unit:     e.Unit,
				State:    "missing",
			})
			continue
		}
		out = append(out, toStateResult(&s))
	}
	return out, nil
}

// haAllEntities returns every entity HA exposes, flagged with `known: true`
// for the ones lifetract has registered. Useful for "what else could I pull?"
func haAllEntities(c *HAClient, domain string) (interface{}, error) {
	all, err := c.GetAllStates()
	if err != nil {
		return nil, err
	}
	out := make([]HAEntityInfo, 0, len(all))
	seen := map[string]bool{}
	var domains []string
	for _, s := range all {
		info := HAEntityInfo{
			EntityID: s.EntityID,
			State:    s.State,
			Unit:     s.Unit(),
		}
		if e, ok := EntityByID(s.EntityID); ok {
			info.Known = true
			info.Kind = string(e.Kind)
		}
		d := entityDomain(s.EntityID)
		if d != "" && !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
		if domain != "" && d != domain {
			continue
		}
		out = append(out, info)
	}
	if domain != "" && len(out) == 0 {
		sort.Strings(domains)
		return nil, fmt.Errorf("ha entities: domain %q has no entities (have: %s)", domain, strings.Join(domains, ", "))
	}
	return out, nil
}

func entityDomain(entityID string) string {
	d, _, ok := strings.Cut(entityID, ".")
	if !ok {
		return ""
	}
	return d
}

// HAHistoryResult is the JSON shape returned for `ha history <kind>`.
type HAHistoryResult struct {
	EntityID string          `json:"entity_id"`
	Kind     string          `json:"kind,omitempty"`
	Unit     string          `json:"unit,omitempty"`
	Days     int             `json:"days"`
	From     string          `json:"from"`
	To       string          `json:"to"`
	Count    int             `json:"count"`
	Points   []HAStateResult `json:"points"`
}

// haHistory wraps HAClient.GetHistory on the same half-open KST window every
// other command uses. A point whose last_changed is outside [from, to) is
// dropped, including one HA returns as the value at the period start. A point
// with no timestamp cannot be placed, so it is an error, not a silent drop.
func haHistory(c *HAClient, ref string, w Window) (interface{}, error) {
	entityID, _ := ResolveEntityRef(ref)
	if entityID == "" {
		entityID = ref
	}
	states, err := c.GetHistory(entityID, w.From, w.To)
	if err != nil {
		return nil, err
	}
	points := make([]HAStateResult, 0, len(states))
	for i := range states {
		if states[i].LastChanged.IsZero() {
			return nil, fmt.Errorf("ha history: %s returned a point with no last_changed", entityID)
		}
		if !w.contains(states[i].LastChanged) {
			continue
		}
		points = append(points, toStateResult(&states[i]))
	}
	out := HAHistoryResult{
		EntityID: entityID,
		Days:     windowDays(w),
		From:     w.From.Format("2006-01-02T15:04:05Z07:00"),
		To:       w.To.Format("2006-01-02T15:04:05Z07:00"),
		Count:    len(points),
		Points:   points,
	}
	if e, ok := EntityByID(entityID); ok {
		out.Kind = string(e.Kind)
		out.Unit = e.Unit
	}
	return out, nil
}

// HALogbookResult is the JSON shape for `ha logbook [kind|entity_id]`.
type HALogbookResult struct {
	EntityID string       `json:"entity_id,omitempty"`
	Kind     string       `json:"kind,omitempty"`
	Days     int          `json:"days"`
	From     string       `json:"from"`
	To       string       `json:"to"`
	Count    int          `json:"count"`
	Entries  []HALogEntry `json:"entries"`
}

// haLogbook reads GET /api/logbook for the same window as ha history.
// arg empty means every entity. A row whose when will not parse is an error.
func haLogbook(c *HAClient, ref string, w Window) (interface{}, error) {
	entityID := ""
	if ref != "" {
		entityID, _ = ResolveEntityRef(ref)
		if entityID == "" {
			entityID = ref
		}
	}
	entries, err := c.GetLogbook(entityID, w.From, w.To)
	if err != nil {
		return nil, err
	}
	kept := make([]HALogEntry, 0, len(entries))
	for _, e := range entries {
		if e.When.IsZero() {
			return nil, fmt.Errorf("ha logbook: entry has no when")
		}
		if !w.contains(e.When) {
			continue
		}
		e.When = e.When.In(KST)
		kept = append(kept, e)
	}
	out := HALogbookResult{
		EntityID: entityID,
		Days:     windowDays(w),
		From:     w.From.Format("2006-01-02T15:04:05Z07:00"),
		To:       w.To.Format("2006-01-02T15:04:05Z07:00"),
		Count:    len(kept),
		Entries:  kept,
	}
	if e, ok := EntityByID(entityID); ok {
		out.Kind = string(e.Kind)
	}
	return out, nil
}

// windowDays is the calendar length of a midnight-bounded window. flagRange
// and daysWindow only build those, so this is the day count, not a rounding.
func windowDays(w Window) int {
	d := int(w.To.Sub(w.From) / (24 * time.Hour))
	if d < 0 {
		return 0
	}
	return d
}

func toStateResult(s *HAState) HAStateResult {
	r := HAStateResult{
		EntityID:    s.EntityID,
		State:       s.State,
		Unit:        s.Unit(),
		LastChanged: s.LastChanged.Format("2006-01-02T15:04:05Z07:00"),
		LastUpdated: s.LastUpdated.Format("2006-01-02T15:04:05Z07:00"),
		Attributes:  s.Attributes,
	}
	if e, ok := EntityByID(s.EntityID); ok {
		r.Kind = string(e.Kind)
		if r.Unit == "" {
			r.Unit = e.Unit
		}
	}
	if v, ok := s.FloatValue(); ok {
		r.Value = &v
	}
	return r
}
