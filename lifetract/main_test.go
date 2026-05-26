package main

import (
	"os"
	"testing"
)

// TestMain disables the HA fallback by default for the full test run.
// Tests that exercise the fallback path inject their own *HAClient via
// the *FromHAClient helpers, so they don't need real network access or
// the LIFETRACT_NO_HA gate.
func TestMain(m *testing.M) {
	_ = os.Setenv("LIFETRACT_NO_HA", "1")
	os.Exit(m.Run())
}
