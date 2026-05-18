package main

// CLI surface for HA REST. Phase 3 keeps these read-only — no DB writes yet.
// Phase 4 wires the same client into cmdToday / cmdRead as a lazy fallback.

import (
	"fmt"
)

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
		return haAllEntities(client)
	default:
		return nil, fmt.Errorf("unknown ha subcommand: %q (ping|state|states|entities)", sub)
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
func haAllEntities(c *HAClient) (interface{}, error) {
	all, err := c.GetAllStates()
	if err != nil {
		return nil, err
	}
	out := make([]HAEntityInfo, 0, len(all))
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
		out = append(out, info)
	}
	return out, nil
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
