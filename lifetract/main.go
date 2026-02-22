package main

import (
	"encoding/json"
	"fmt"
	"os"
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

Flags:
  --days N               Days to look back (default: 7)
  --data-dir DIR         Data directory (default: ~/repos/gh/self-tracking-data)
  --summary              Summary/aggregated mode
  --category CAT         Filter time category

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
	flags := parseFlags(args)
	cfg := newConfig(flags)

	// Extract positional arg (for read command)
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		cfg.ReadID = args[0]
	}

	var result interface{}
	var err error

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
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}

	if err != nil {
		errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintln(os.Stderr, string(errJSON))
		os.Exit(1)
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"error": "json marshal: %s"}`, err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

// parseFlags parses --key value pairs from args.
func parseFlags(args []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if len(args[i]) > 2 && args[i][:2] == "--" {
			key := args[i][2:]
			if key == "summary" {
				flags[key] = "true"
				continue
			}
			if i+1 < len(args) {
				flags[key] = args[i+1]
				i++
			}
		}
	}
	return flags
}

func flagDays(flags map[string]string) int {
	if s, ok := flags["days"]; ok {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 7
}
