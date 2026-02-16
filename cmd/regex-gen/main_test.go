package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 16384)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func TestRunToStdout(t *testing.T) {
	var runErr error
	output := captureStdout(t, func() {
		runErr = run("[a-z]+", "mypkg", "MatchLower", "", "full", false, "FindSubmatch")
	})
	require.NoError(t, runErr)

	assert.Contains(t, output, "package mypkg")
	assert.Contains(t, output, "func MatchLower(input string) bool")
}

func TestRunToFile(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "match.go")

	err := run("[0-9]+", "testpkg", "MatchDigits", outFile, "full", false, "FindSubmatch")
	require.NoError(t, err)

	content, err := os.ReadFile(outFile)
	require.NoError(t, err)

	output := string(content)
	assert.Contains(t, output, "package testpkg")
	assert.Contains(t, output, "func MatchDigits(input string) bool")
}

func TestRunMissingRegex(t *testing.T) {
	err := run("", "pkg", "Match", "", "full", false, "FindSubmatch")
	assert.Error(t, err)
}

func TestRunInvalidRegex(t *testing.T) {
	err := run("[unclosed", "pkg", "Match", "", "full", false, "FindSubmatch")
	assert.Error(t, err)
}

func TestRunDefaultPackage(t *testing.T) {
	var runErr error
	output := captureStdout(t, func() {
		runErr = run("abc", "", "Match", "", "full", false, "FindSubmatch")
	})
	require.NoError(t, runErr)
	assert.Contains(t, output, "package main")
}

func TestRunGOPACKAGEEnv(t *testing.T) {
	t.Setenv("GOPACKAGE", "envpkg")

	var runErr error
	output := captureStdout(t, func() {
		runErr = run("abc", "", "Match", "", "full", false, "FindSubmatch")
	})
	require.NoError(t, runErr)
	assert.Contains(t, output, "package envpkg")
}

func TestRunInvalidOutputPath(t *testing.T) {
	err := run("abc", "pkg", "Match", "/nonexistent/dir/file.go", "full", false, "FindSubmatch")
	assert.Error(t, err)
}

func TestRunComplexPatterns(t *testing.T) {
	patterns := []struct {
		name    string
		pattern string
	}{
		{"email", `[a-z]+@[a-z]+\.[a-z]{2,}`},
		{"ssn", `\d{3}-\d{2}-\d{4}`},
		{"url", `(https?://)?[a-z]+\.[a-z]{2,}`},
	}

	for _, tt := range patterns {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			outFile := filepath.Join(tmpDir, "match.go")

			err := run(tt.pattern, "pkg", "Match", outFile, "full", false, "FindSubmatch")
			require.NoError(t, err)

			content, err := os.ReadFile(outFile)
			require.NoError(t, err)
			assert.Contains(t, string(content), "func Match(input string) bool")
		})
	}
}

func TestRunWithSubmatch(t *testing.T) {
	var runErr error
	output := captureStdout(t, func() {
		runErr = run(`([a-z]+)@([a-z]+)`, "mypkg", "MatchEmail", "", "full", true, "FindEmailSubmatch")
	})
	require.NoError(t, runErr)

	assert.Contains(t, output, "package mypkg")
	assert.Contains(t, output, "func MatchEmail(input string) bool")
	assert.Contains(t, output, "func FindEmailSubmatch(input string) []string")
}

func TestRunInvalidMatchMode(t *testing.T) {
	err := run("abc", "pkg", "Match", "", "badmode", false, "FindSubmatch")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid -match mode")
}

func TestRunExplicitPackageOverridesEnv(t *testing.T) {
	t.Setenv("GOPACKAGE", "envpkg")

	var runErr error
	output := captureStdout(t, func() {
		runErr = run("abc", "explicit", "Match", "", "full", false, "FindSubmatch")
	})
	require.NoError(t, runErr)

	// Explicit -package flag should win over $GOPACKAGE
	assert.Contains(t, output, "package explicit")
	assert.NotContains(t, output, "package envpkg")
}
