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

// haFloatStateForKind fetches the current numeric state of the first registered
// entity for a Kind, but only if that state actually describes today.
//
// A sensor that stops reporting does not raise an error — GetState keeps handing back
// the last value it ever saw, forever. The phone's heart_rate sensor froze at 112
// on 2026-07-03 and lifetract went on stamping "112" into the journal as today's
// fact for eleven days. A stale live reading is worse than a missing one: the
// journal is permanent, and a later reader cannot tell the difference.
//
// So the state must have changed today, on the KST axis. These sensors describe
// today (today's steps, a reading taken today); if none arrived today, we do not
// have the value, and saying nothing is the honest answer.
func haFloatStateForKind(c *HAClient, kind EntityKind) (float64, bool) {
	es, ok := EntitiesByKind[kind]
	if !ok || len(es) == 0 {
		return 0, false
	}
	s, err := c.GetState(es[0].EntityID)
	if err != nil {
		return 0, false
	}
	if !isToday(dateStr(s.LastChanged)) {
		return 0, false
	}
	return s.FloatValue()
}

// haTodayAvgHR averages every heart rate HA recorded today, on the KST axis.
//
// The current state is a single instantaneous reading — it is not an average, and
// it must not be poured into a field called avg_hr just because the types line up.
// The DB path computes AVG(heart_rate) across the day; the live path has to mean
// the same thing or the two sources silently disagree.
//
// History replays the state as it stood at the window's start, so a sensor that
// died weeks ago still appears inside today's window — and HA stamps that
// replayed row with the window start, not with when the value was really last
// set. The frozen 112 came back dated 00:00 today, which is why a "is it older
// than midnight?" test waved it through.
//
// So a sample counts only if it changed strictly AFTER the window opened. That
// holds whichever timestamp HA reports: a replay clamped to the boundary fails
// it, and so does a true timestamp from July. A dead sensor then yields no
// samples at all, which is the honest answer — rather than its last gasp, or an
// average contaminated by a value carried over from a previous day.
func haTodayAvgHR(c *HAClient) (float64, bool) {
	es, ok := EntitiesByKind[KindHeartRate]
	if !ok || len(es) == 0 {
		return 0, false
	}

	now := nowKST()
	start := startOfDay(now)
	states, err := c.GetHistory(es[0].EntityID, start, now)
	if err != nil {
		return 0, false
	}

	var sum float64
	var n int
	for _, s := range states {
		if !s.LastChanged.After(start) {
			continue // replayed from the boundary: not a reading taken today
		}
		v, ok := s.FloatValue()
		if !ok || v <= 0 {
			continue
		}
		sum += v
		n++
	}
	if n == 0 {
		return 0, false
	}
	return round1(sum / float64(n)), true
}

// sleepEndLocalDay returns the local calendar day (YYYY-MM-DD) a sleep_duration
// state belongs to, taken from its endTime attribute (the session's wake time).
// ok=false when the attribute is absent or unparseable — older firmware and the
// unit tests emit states without it, and the caller falls back to LastChanged.
func sleepEndLocalDay(s HAState) (string, bool) {
	raw, has := s.Attributes["endTime"].(string)
	if !has || raw == "" {
		return "", false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", false
	}
	// HA returns a real instant with a zone; attribute it on the KST axis, not
	// on whatever zone the calling shell happens to be in.
	return t.In(KST).Format("2006-01-02"), true
}

// haRecentSleepMinutes returns today's HA sleep minutes (main sleep + naps).
// HA Companion App emits one state change per recorded sleep *session*, each
// carrying the session's total duration and an endTime attribute marking when
// it ended. We bucket sessions by the local calendar day of that endTime and
// sum only today's — deduping re-syncs of the same session by endTime.
//
// We must NOT key off LastChanged (HA record time): the phone syncs sleeps
// hours-to-days late, so a prior night's session lands inside today's 36h
// LastChanged window. Summing those conflated 2-3 nights + naps into a single
// 12-20h figure (the inflation bug). endTime attribution fixes that; states
// lacking the attribute fall back to the old 36h window so legacy data and
// tests still resolve. Returns (0,false) when nothing falls in window.
func haRecentSleepMinutes(c *HAClient) (float64, bool) {
	es, ok := EntitiesByKind[KindSleepDuration]
	if !ok || len(es) == 0 {
		return 0, false
	}
	now := nowKST()
	states, err := c.GetHistory(es[0].EntityID, now.AddDate(0, 0, -2), now)
	if err != nil {
		return 0, false
	}
	today := now.Format("2006-01-02")
	cutoff := now.Add(-36 * time.Hour)

	byEnd := map[string]float64{} // endTime -> max duration (dedup re-syncs)
	var fallback float64
	seen := false
	for _, s := range states {
		v, ok := s.FloatValue()
		if !ok || v <= 0 {
			continue
		}
		if day, ok := sleepEndLocalDay(s); ok {
			if day != today {
				continue // a different night's sleep synced into the window
			}
			key := s.Attributes["endTime"].(string)
			if v > byEnd[key] {
				byEnd[key] = v
			}
			seen = true
			continue
		}
		// No endTime attribute: fall back to the 36h LastChanged window.
		if s.LastChanged.Before(cutoff) {
			continue
		}
		fallback += v
		seen = true
	}
	if !seen {
		return 0, false
	}
	total := fallback
	for _, v := range byEnd {
		total += v
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
	sleeps, err := dbQuerySleep(cfg, daysWindow(2))
	if err != nil || len(sleeps) == 0 {
		return false
	}
	latest := sleeps[0].Date
	now := nowKST()
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
		if v, ok := haTodayAvgHR(c); ok && v > 0 {
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
			if sleeps, err := dbQuerySleep(cfg, daysWindow(2)); err == nil && len(sleeps) > 0 {
				now := nowKST()
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
		if v, ok := haTodayAvgHR(c); ok && v > 0 {
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

// isToday returns true when the given date string is today on the KST axis.
func isToday(date string) bool {
	return date == nowKST().Format("2006-01-02")
}
