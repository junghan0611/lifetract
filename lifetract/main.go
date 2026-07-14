package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

const version = "0.1.0"

func usage() {
	fmt.Fprintf(os.Stderr, `lifetract v%s — Life tracking CLI for AI agents

Usage:
  lifetract <command> [flags]

Commands:
  status                 Show data source availability and stats
  today                  Today's unified health + time summary
  timeline [--days N]    Date-indexed unified view (denotecli-compatible)
  read <denote-id>       Read by Denote ID (YYYYMMDDT000000 or YYYY-MM-DD)
  sleep   [--days N]     Sleep session analysis
  steps   [--days N]     Daily step counts
  heart   [--days N]     Heart rate statistics
  stress  [--days N]     Stress level analysis
  exercise [--days N]    Exercise sessions
  time    [--days N]     Time category analysis (aTimeLogger)
  import                 Show import manifest (CSV+SQLite → lifetract.db)
  export                 Show export plan (public-safe DB)
  ha <sub> [arg]         Home Assistant REST (ping|state|states|entities|history)

Flags:
  --days N               Window length (default: 7)
  --from YYYY-MM-DD      Window start, inclusive
  --to   YYYY-MM-DD      Window end, EXCLUSIVE
  --data-dir DIR         Data directory (default: ~/repos/gh/self-tracking-data)
  --summary              Summary/aggregated mode
  --category CAT         Filter time category

Windows (every combination means one thing; --days is never ignored):
  --days N               [today-N, tomorrow)   the last N days, and today
  --days N --to T        [T-N, T)              N days ending at T
  --days N --from F      [F, F+N)              N days starting at F
  --from F --to T        [F, T)
  --from F               [F, tomorrow)
  --to T                 everything before T
  --days N --from F --to T   → error (overspecified: say which two you mean)

Time contract:
  All dates are KST (fixed +09:00). The answer never depends on the caller's $TZ.
  Windows are half-open [from, to): --from 2026-07-01 --to 2026-07-08 is 7 days,
  Jul 1 through Jul 7. Adjacent windows tile without overlap.
  A block is attributed to the day it STARTS on: sleep 21:14 → 05:48 is all the
  earlier day.
  Use --from/--to for anything that must be reproducible; --days is relative to
  today and so answers a different question tomorrow.

All output is JSON.
`, version)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		usage()
		return
	}
	if cmd == "--version" || cmd == "-v" {
		fmt.Println(version)
		return
	}

	// Parse flags and positional args
	args := os.Args[2:]
	flags, err := parseFlags(args)
	if err != nil {
		fail(err)
	}
	cfg, err := newConfig(flags)
	if err != nil {
		fail(err)
	}

	// Extract positional arg (for read command)
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		cfg.ReadID = args[0]
	}

	// --exec flag
	if flags["exec"] == "true" {
		cfg.Exec = true
	}

	var result interface{}

	switch cmd {
	case "status":
		result, err = cmdStatus(cfg)
	case "today":
		result, err = cmdToday(cfg)
	case "timeline":
		result, err = cmdTimeline(cfg)
	case "read":
		result, err = cmdRead(cfg)
	case "sleep":
		result, err = cmdSleep(cfg)
	case "steps":
		result, err = cmdSteps(cfg)
	case "heart":
		result, err = cmdHeart(cfg)
	case "stress":
		result, err = cmdStress(cfg)
	case "exercise":
		result, err = cmdExercise(cfg)
	case "time":
		result, err = cmdTime(cfg)
	case "import":
		result, err = cmdImport(cfg)
	case "export":
		result, err = cmdExport(cfg)
	case "ha":
		sub := ""
		haArg := ""
		if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
			sub = args[0]
		}
		if len(args) > 1 && !strings.HasPrefix(args[1], "--") {
			haArg = args[1]
		}
		result, err = cmdHA(cfg, sub, haArg)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}

	if err != nil {
		fail(err)
	}

	out, err := json.MarshalIndent(emptyList(result), "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"error": "json marshal: %s"}`, err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

// fail reports on stderr as JSON and exits non-zero. Never on stdout: a caller
// piping us into jq must not find an error object where a list belongs.
func fail(err error) {
	errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
	fmt.Fprintln(os.Stderr, string(errJSON))
	os.Exit(1)
}

// emptyList turns a nil slice into an empty one, so a quiet day marshals to []
// rather than null. A caller looping over the result must not have to tell "you
// logged no exercise" apart from "the tool broke" — zero is an answer, null is a
// hole. Every list command funnels through here, so a new one cannot reopen it.
func emptyList(v interface{}) interface{} {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		return reflect.MakeSlice(rv.Type(), 0, 0).Interface()
	}
	return v
}

// Every flag the CLI understands. A flag not in here is a typo, and a typo has to
// be told, not absorbed.
var (
	boolFlags  = map[string]bool{"summary": true, "exec": true}
	valueFlags = map[string]bool{
		"days": true, "from": true, "to": true,
		"category": true, "data-dir": true, "shealth-dir": true,
	}
)

// parseFlags parses --key value pairs, and refuses everything it does not
// understand.
//
// It used to shrug. `--fro 2026-07-01` was dropped on the floor and the query
// answered for the default window with exit 0 — the caller asked about July and
// was told about this week, in the same shape, with nothing to mark the swap.
// `--from` with no value did the same. A parser that silently discards input is
// the tool lying about what question it answered.
func parseFlags(args []string) (map[string]string, error) {
	flags := make(map[string]string)

	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue // positional (e.g. a Denote ID); handled by the caller
		}
		key := strings.TrimPrefix(a, "--")

		switch {
		case boolFlags[key]:
			if _, dup := flags[key]; dup {
				return nil, fmt.Errorf("--%s given twice", key)
			}
			flags[key] = "true"
		case valueFlags[key]:
			if _, dup := flags[key]; dup {
				return nil, fmt.Errorf("--%s given twice", key)
			}
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return nil, fmt.Errorf("--%s needs a value", key)
			}
			flags[key] = args[i+1]
			i++
		default:
			return nil, fmt.Errorf("unknown flag --%s", key)
		}
	}
	return flags, nil
}

func flagDays(flags map[string]string) int {
	if s, ok := flags["days"]; ok {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 7
}
