package main

// Home Assistant REST client.
//
// lifetract treats HA as the live SSOT for health metrics that originate on
// the phone (Samsung Health → Health Connect → HA Companion App). The CLI
// pulls states on demand; the DB serves as a per-device lazy cache.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHABaseURL = "https://ha.junghanacs.com"
	defaultHATimeout = 10 * time.Second
)

// HAClient talks to a Home Assistant instance via its REST API.
type HAClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// HAState mirrors the JSON shape HA returns from /api/states/<entity_id>.
type HAState struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	Attributes  map[string]interface{} `json:"attributes"`
	LastChanged time.Time              `json:"last_changed"`
	LastUpdated time.Time              `json:"last_updated"`
}

// Unit returns the canonical unit_of_measurement attribute if HA provides one.
func (s *HAState) Unit() string {
	if u, ok := s.Attributes["unit_of_measurement"].(string); ok {
		return u
	}
	return ""
}

// FloatValue parses the state string as a float. Returns (0, false) for
// "unknown" / "unavailable" / unparseable values — HA uses these strings for
// sensors that have no fresh reading.
func (s *HAState) FloatValue() (float64, bool) {
	if s.State == "" || s.State == "unknown" || s.State == "unavailable" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s.State, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// NewHAClient builds a client. Token resolution order:
//  1. HA_TOKEN environment variable
//  2. pass show 2fa/totp/ha/junghanacs (password-store entry)
//  3. ~/.lifetract/ha.env (KEY=VALUE; HA_TOKEN=...)
//
// BaseURL: HA_URL env > defaultHABaseURL.
func NewHAClient() (*HAClient, error) {
	tok, err := loadHAToken()
	if err != nil {
		return nil, fmt.Errorf("ha token: %w", err)
	}
	base := os.Getenv("HA_URL")
	if base == "" {
		base = defaultHABaseURL
	}
	return &HAClient{
		BaseURL: strings.TrimRight(base, "/"),
		Token:   tok,
		HTTP:    &http.Client{Timeout: defaultHATimeout},
	}, nil
}

// loadHAToken resolves the access token from one of the three sources.
func loadHAToken() (string, error) {
	if t := os.Getenv("HA_TOKEN"); t != "" {
		return strings.TrimSpace(t), nil
	}
	if t, err := loadTokenFromPass(); err == nil {
		return t, nil
	}
	if t, err := loadTokenFromEnvFile(); err == nil {
		return t, nil
	}
	return "", fmt.Errorf("no token (set HA_TOKEN, or pass insert 2fa/totp/ha/junghanacs, or write ~/.lifetract/ha.env)")
}

func loadTokenFromPass() (string, error) {
	cmd := exec.Command("pass", "show", "2fa/totp/ha/junghanacs")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if tok == "" {
		return "", fmt.Errorf("empty pass entry")
	}
	return tok, nil
}

func loadTokenFromEnvFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".lifetract", "ha.env")
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == "HA_TOKEN" {
			return strings.Trim(strings.TrimSpace(v), `"'`), nil
		}
	}
	return "", fmt.Errorf("HA_TOKEN not found in %s", path)
}

// Ping checks /api/ is reachable with the configured token.
func (c *HAClient) Ping() error {
	body, err := c.get("/api/")
	if err != nil {
		return err
	}
	var resp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("decode ping: %w", err)
	}
	if resp.Message != "API running." {
		return fmt.Errorf("unexpected ping response: %q", resp.Message)
	}
	return nil
}

// GetState fetches one entity's current state.
func (c *HAClient) GetState(entityID string) (*HAState, error) {
	body, err := c.get("/api/states/" + entityID)
	if err != nil {
		return nil, err
	}
	var s HAState
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("decode state %s: %w", entityID, err)
	}
	return &s, nil
}

// GetAllStates fetches every entity HA currently exposes.
func (c *HAClient) GetAllStates() ([]HAState, error) {
	body, err := c.get("/api/states")
	if err != nil {
		return nil, err
	}
	var out []HAState
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode states: %w", err)
	}
	return out, nil
}

// GetHistory fetches the recorded state changes for one entity between start
// and end. HA returns [][]HAState (outer = per entity, inner = chronological
// state changes); we flatten to a single slice for the common single-entity
// case. HA's recorder keeps changes only — values stay the same emit no rows —
// and only data accumulated since the recorder started exists. There is no
// backfill for periods before HA was deployed.
func (c *HAClient) GetHistory(entityID string, start, end time.Time) ([]HAState, error) {
	q := url.Values{}
	q.Set("filter_entity_id", entityID)
	q.Set("end_time", end.UTC().Format("2006-01-02T15:04:05+00:00"))
	startStr := start.UTC().Format("2006-01-02T15:04:05+00:00")
	path := "/api/history/period/" + startStr + "?" + q.Encode()
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var series [][]HAState
	if err := json.Unmarshal(body, &series); err != nil {
		return nil, fmt.Errorf("decode history %s: %w", entityID, err)
	}
	if len(series) == 0 {
		return []HAState{}, nil
	}
	return series[0], nil
}

// get issues an authenticated GET and returns the response body.
func (c *HAClient) get(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
