package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	stdout, _, err := executeCapture(t, args...)
	return stdout, err
}

func executeCapture(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	regex = ""
	pkg = ""
	funcName = "Match"
	outputPath = ""
	matchMode = "full"
	submatch = false
	submatchFn = "FindSubmatch"

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestRunToStdout(t *testing.T) {
	stdout, err := execute(t, "--regex", "[a-z]+", "--package", "mypkg", "--func", "MatchLower")
	require.NoError(t, err)
	assert.Contains(t, stdout, "package mypkg")
	assert.Contains(t, stdout, "func MatchLower(input string) bool")
}

func TestRunToFile(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "match.go")

	_, err := execute(t, "--regex", "[0-9]+", "--package", "testpkg", "--func", "MatchDigits", "--output", outFile)
	require.NoError(t, err)

	content, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "package testpkg")
	assert.Contains(t, string(content), "func MatchDigits(input string) bool")
}

func TestRunMissingRegex(t *testing.T) {
	_, err := execute(t)
	assert.Error(t, err)
}

func TestRunInvalidRegex(t *testing.T) {
	_, err := execute(t, "--regex", "[unclosed")
	assert.Error(t, err)
}

func TestRunDefaultPackage(t *testing.T) {
	stdout, err := execute(t, "--regex", "abc")
	require.NoError(t, err)
	assert.Contains(t, stdout, "package main")
}

func TestRunGOPACKAGEEnv(t *testing.T) {
	t.Setenv("GOPACKAGE", "envpkg")
	stdout, err := execute(t, "--regex", "abc")
	require.NoError(t, err)
	assert.Contains(t, stdout, "package envpkg")
}

func TestRunInvalidOutputPath(t *testing.T) {
	_, err := execute(t, "--regex", "abc", "--output", "/nonexistent/dir/file.go")
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

			_, err := execute(t, "--regex", tt.pattern, "--output", outFile)
			require.NoError(t, err)

			content, err := os.ReadFile(outFile)
			require.NoError(t, err)
			assert.Contains(t, string(content), "func Match(input string) bool")
		})
	}
}

func TestRunWithSubmatch(t *testing.T) {
	stdout, err := execute(t, "--regex", `([a-z]+)@([a-z]+)`, "--package", "mypkg", "--func", "MatchEmail", "--submatch", "--submatch-func", "FindEmailSubmatch")
	require.NoError(t, err)
	assert.Contains(t, stdout, "package mypkg")
	assert.Contains(t, stdout, "func MatchEmail(input string) bool")
	assert.Contains(t, stdout, "func FindEmailSubmatch(input string) []string")
}

func TestRunSubmatchRequiresFullMode(t *testing.T) {
	for _, mode := range []string{"prefix", "contains"} {
		t.Run(mode, func(t *testing.T) {
			_, err := execute(t, "--regex", "(a)b", "--submatch", "--match", mode)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--submatch requires --match full")
		})
	}
}

func TestRunSubmatchNoGroupsNote(t *testing.T) {
	stdout, stderr, err := executeCapture(t, "--regex", "abc", "--submatch")
	require.NoError(t, err)
	assert.Contains(t, stderr, "note: --submatch ignored: the regex has no capture groups")
	assert.Contains(t, stdout, "func Match(input string) bool")
	assert.NotContains(t, stdout, "FindSubmatch", "no submatch family should be generated without capture groups")
}

func TestRunSubmatchWithGroupsNoNote(t *testing.T) {
	_, stderr, err := executeCapture(t, "--regex", "(a)b", "--submatch")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "note:")
}

func TestRunInvalidMatchMode(t *testing.T) {
	_, err := execute(t, "--regex", "abc", "--match", "badmode")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --match mode")
}

func TestRunExplicitPackageOverridesEnv(t *testing.T) {
	t.Setenv("GOPACKAGE", "envpkg")
	stdout, err := execute(t, "--regex", "abc", "--package", "explicit")
	require.NoError(t, err)
	assert.Contains(t, stdout, "package explicit")
	assert.NotContains(t, stdout, "package envpkg")
}
