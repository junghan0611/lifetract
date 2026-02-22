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
	flags := parseFlags([]string{"--days", "30", "--summary", "--category", "Deep Work"})
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

func TestFlagDays(t *testing.T) {
	tests := []struct {
		flags map[string]string
		want  int
	}{
		{map[string]string{"days": "30"}, 30},
		{map[string]string{"days": "0"}, 7},   // invalid → default
		{map[string]string{"days": "-1"}, 7},  // invalid → default
		{map[string]string{}, 7},               // missing → default
	}
	for _, tt := range tests {
		got := flagDays(tt.flags)
		if got != tt.want {
			t.Errorf("flagDays(%v) = %d, want %d", tt.flags, got, tt.want)
		}
	}
}
