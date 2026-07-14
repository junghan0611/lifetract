package main

import (
	"testing"
)

func TestStripBOM(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"\xEF\xBB\xBFhello", "hello"},
		{"\uFEFFhello", "hello"},
		{"hello", "hello"},
		{"", ""},
	}
	for _, tt := range tests {
		got := stripBOM(tt.input)
		if got != tt.want {
			t.Errorf("stripBOM(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseShealthTime(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"2021-01-21 01:19:00.000", false},
		{"2025-09-01 15:21:00.000", false},
		{"2017-05-23 03:20:29.996", false},
		{"invalid", true},
		{"", true},
	}
	for _, tt := range tests {
		_, err := parseShealthTime(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseShealthTime(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
	}
}

func TestParseFlags(t *testing.T) {
	flags, err := parseFlags([]string{"--days", "30", "--summary", "--category", "Deep Work"})
	if err != nil {
		t.Fatal(err)
	}
	if flags["days"] != "30" {
		t.Errorf("days = %q, want 30", flags["days"])
	}
	if flags["summary"] != "true" {
		t.Errorf("summary = %q, want true", flags["summary"])
	}
	if flags["category"] != "Deep Work" {
		t.Errorf("category = %q, want Deep Work", flags["category"])
	}
}

// A parser that discards what it does not understand makes the tool answer a
// question the caller never asked — and say nothing about the swap. `--fro` used
// to be dropped on the floor: exit 0, a perfectly shaped JSON list, for the
// default window instead of the July one.
func TestParseFlagsRefusesWhatItCannotHonour(t *testing.T) {
	bad := [][]string{
		{"--fro", "2026-07-01"},        // typo
		{"--from"},                     // no value
		{"--from", "--days", "3"},      // value eaten by the next flag
		{"--days", "3", "--days", "5"}, // said twice
		{"--summary", "--summary"},     // said twice (bool)
		{"--category"},                 // no value
	}
	for _, args := range bad {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%v) = nil error — silently swallowed", args)
		}
	}
}

func TestDenoteID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2025-10-04", "20251004T000000"},
		{"2021-01-21", "20210121T000000"},
		{"2017-12-06", "20171206T000000"},
	}
	for _, tt := range tests {
		got := denoteDayID(tt.input)
		if got != tt.want {
			t.Errorf("denoteDayID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDenoteIDFromTime(t *testing.T) {
	ts, _ := parseShealthTime("2025-10-04 21:21:00.000")
	got := denoteID(ts)
	if got != "20251004T212100" {
		t.Errorf("denoteID() = %q, want 20251004T212100", got)
	}
}

func TestParseDenoteID(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"20251004T000000", false},
		{"20251004T212100", false},
		{"invalid", true},
	}
	for _, tt := range tests {
		_, err := parseDenoteID(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseDenoteID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
	}
}

func TestFlagDays(t *testing.T) {
	tests := []struct {
		flags map[string]string
		want  int
	}{
		{map[string]string{"days": "30"}, 30},
		{map[string]string{"days": "0"}, 7},  // invalid → default
		{map[string]string{"days": "-1"}, 7}, // invalid → default
		{map[string]string{}, 7},             // missing → default
	}
	for _, tt := range tests {
		got := flagDays(tt.flags)
		if got != tt.want {
			t.Errorf("flagDays(%v) = %d, want %d", tt.flags, got, tt.want)
		}
	}
}
