package cmd

import (
	"fmt"
	"os"
	"testing"
)

// Default logging must never write into the developer's real home during tests.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "repocli-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Setenv("HOME", home); err != nil {
		os.RemoveAll(home)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
