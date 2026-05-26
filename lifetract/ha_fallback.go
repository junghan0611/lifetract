package main

// Read-only HA fallback for "today's gap" — cmdToday / cmdRead of today's
// date. When the DB doesn't yet carry today's row (Samsung CSV only dumps
// periodically) we ask Home Assistant for the live state. Past dates are
// never enriched: HA recorder doesn't backfill.
//
// Phase 7 (HA → DB lazy ingest, plan.md) eventually upserts these into the
// DB so the same answer survives offline; for now this is read-only and
// silent on failure — no token / unreachable HA / disabled = skip cleanly.

import (
	"os"
	"strings"
	"time"
)

// haFallbackEnabled returns false when the operator opted out via
// LIFETRACT_NO_HA=1. Default is enabled.
func haFallbackEnabled() bool {
	v := strings.TrimSpace(os.Getenv("LIFETRACT_NO_HA"))
	return v != "1" && !strings.EqualFold(v, "true")
}

// dialHAFallback returns a ready HAClient, or nil if HA is disabled / token
// missing / any setup error. Callers must treat nil as "no fallback available"
// and return whatever DB/CSV already produced.
func dialHAFallback() *HAClient {
	if !haFallbackEnabled() {
		return nil
	}
	c, err := NewHAClient()
	if err != nil {
		return nil
	}
	return c
}

// haFloatStateForKind fetches the current numeric state of the first
// registered entity for a Kind. Returns (0,false) on any failure or
// unknown/unavailable state.
func haFloatStateForKind(c *HAClient, kind EntityKind) (float64, bool) {
	es, ok := EntitiesByKind[kind]
	if !ok || len(es) == 0 {
		return 0, false
	}
	s, err := c.GetState(es[0].EntityID)
	if err != nil {
		return 0, false
	}
	return s.FloatValue()
}

// haRecentSleepMinutes sums sleep_duration history entries from the last 36h.
// HA Companion App emits one state change per recorded sleep session, so the
// sum covers main sleep + naps for the current day cluster. Returns
// (0,false) when no entries fall in window.
func haRecentSleepMinutes(c *HAClient) (float64, bool) {
	es, ok := EntitiesByKind[KindSleepDuration]
	if !ok || len(es) == 0 {
		return 0, false
	}
	end := time.Now()
	start := end.AddDate(0, 0, -2)
	states, err := c.GetHistory(es[0].EntityID, start, end)
	if err != nil {
		return 0, false
	}
	cutoff := end.Add(-36 * time.Hour)
	total := 0.0
	seen := false
	for _, s := range states {
		if s.LastChanged.Before(cutoff) {
			continue
		}
		v, ok := s.FloatValue()
		if !ok || v <= 0 {
			continue
		}
		total += v
		seen = true
	}
	if !seen {
		return 0, false
	}
	return total, true
}

// todaySleepStale reports whether the DB's "most recent" sleep row is too old
// to represent today's sleep. cmdToday's sleep query naively picks the latest
// sleep row regardless of date, so a DB that lags by 2+ days returns yesterday's
// nap as "today's sleep". We treat that as stale and let HA overwrite it.
//
// The decision uses the latest sleep date returned by dbQuerySleep(2). If that
// date is today or yesterday it's a legitimate overnight; older = stale.
func todaySleepStale(cfg *Config, today *TodayResult) bool {
	if today.SleepHours == 0 {
		return false // gap, not stale — handled by the "needs fill" branch
	}
	if !dbExists(cfg) {
		return false
	}
	sleeps, err := dbQuerySleep(cfg, 2)
	if err != nil || len(sleeps) == 0 {
		return false
	}
	latest := sleeps[0].Date
	now := time.Now()
	todayS := now.Format("2006-01-02")
	yesterdayS := now.AddDate(0, 0, -1).Format("2006-01-02")
	return latest != todayS && latest != yesterdayS
}

// enrichTodayWithHA fills Steps / SleepHours / AvgHR from HA when the DB/CSV
// answer is missing or (for sleep) stale. Adds "+ha" to Source and lists the
// patched fields in HASources. Silent skip when HA isn't available.
func enrichTodayWithHA(cfg *Config, today *TodayResult) {
	c := dialHAFallback()
	if c == nil {
		return
	}
	enrichTodayFromHAClient(cfg, today, c)
}

// enrichTodayFromHAClient is the testable core — same logic, injected client.
func enrichTodayFromHAClient(cfg *Config, today *TodayResult, c *HAClient) {
	stale := todaySleepStale(cfg, today)
	var used []string

	if today.Steps == 0 {
		if v, ok := haFloatStateForKind(c, KindStepsDaily); ok && v > 0 {
			today.Steps = int(v)
			used = append(used, "steps")
		}
	}
	if today.AvgHR == 0 {
		if v, ok := haFloatStateForKind(c, KindHeartRate); ok && v > 0 {
			today.AvgHR = v
			used = append(used, "heart_rate")
		}
	}
	if today.SleepHours == 0 || stale {
		if mins, ok := haRecentSleepMinutes(c); ok && mins > 0 {
			today.SleepHours = round1(mins / 60.0)
			used = append(used, "sleep")
		}
	}

	if len(used) > 0 {
		if today.Source != "" && !strings.Contains(today.Source, "+ha") {
			today.Source = today.Source + "+ha"
		} else if today.Source == "" {
			today.Source = "ha"
		}
		today.HASources = used
	}
}

// enrichTimelineEntryWithHA fills today-only gaps on a TimelineEntry. Caller
// guarantees `entry.Date == today`.
func enrichTimelineEntryWithHA(cfg *Config, entry *TimelineEntry) {
	c := dialHAFallback()
	if c == nil {
		return
	}
	enrichTimelineEntryFromHAClient(cfg, entry, c)
}

// enrichTimelineEntryFromHAClient is the testable core.
func enrichTimelineEntryFromHAClient(cfg *Config, entry *TimelineEntry, c *HAClient) {
	h := entry.Health
	stale := false
	if h != nil && h.SleepHours > 0 {
		// Mirror todaySleepStale's logic without round-tripping TodayResult.
		if dbExists(cfg) {
			if sleeps, err := dbQuerySleep(cfg, 2); err == nil && len(sleeps) > 0 {
				now := time.Now()
				todayS := now.Format("2006-01-02")
				yesterdayS := now.AddDate(0, 0, -1).Format("2006-01-02")
				if sleeps[0].Date != todayS && sleeps[0].Date != yesterdayS {
					stale = true
				}
			}
		}
	}

	var used []string

	needSteps := h == nil || h.Steps == 0
	if needSteps {
		if v, ok := haFloatStateForKind(c, KindStepsDaily); ok && v > 0 {
			if h == nil {
				h = &HealthMetrics{}
				entry.Health = h
			}
			h.Steps = int(v)
			used = append(used, "steps")
		}
	}

	needHR := h == nil || h.AvgHR == 0
	if needHR {
		if v, ok := haFloatStateForKind(c, KindHeartRate); ok && v > 0 {
			if h == nil {
				h = &HealthMetrics{}
				entry.Health = h
			}
			h.AvgHR = v
			used = append(used, "heart_rate")
		}
	}

	needSleep := h == nil || h.SleepHours == 0 || stale
	if needSleep {
		if mins, ok := haRecentSleepMinutes(c); ok && mins > 0 {
			if h == nil {
				h = &HealthMetrics{}
				entry.Health = h
			}
			h.SleepHours = round1(mins / 60.0)
			used = append(used, "sleep")
		}
	}

	if len(used) > 0 {
		entry.HASources = used
	}
}

// isToday returns true when the given date string matches today's date in
// the local timezone.
func isToday(date string) bool {
	return date == time.Now().Format("2006-01-02")
}
