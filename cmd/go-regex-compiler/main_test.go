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

// TestRunUnsupportedAssertions verifies that empty-width assertions the DFA
// cannot honor are rejected with a descriptive error instead of silently
// generating a wrong matcher (foo\bbar used to full-match "foobar").
func TestRunUnsupportedAssertions(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"mid_dollar", []string{"--regex", `a$b`}},
		{"dollar_in_prefix_mode", []string{"--regex", `ab$`, "--match", "prefix"}},
		{"caret_in_contains_mode", []string{"--regex", `^abc`, "--match", "contains"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execute(t, tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "regex", "error should explain the rejected construct")
		})
	}
}

// A word boundary is decided by the pair of characters around it, which the DFA
// resolves per outgoing rune range, so \b compiles in every position and every
// mode. foo\bbar is one of those: it matches nothing, and it compiles.
func TestRunWordBoundaryCompilesAnywhere(t *testing.T) {
	for _, tc := range []struct{ name, regex, mode string }{
		{"interior", `foo\bbar`, "full"},
		{"leading_contains", `\bcat\b`, "contains"},
		{"trailing_prefix", `\bused to\b`, "prefix"},
		{"negated_interior", `foo\Bbar`, "full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execute(t, "--regex", tc.regex, "--match", tc.mode)
			require.NoError(t, err)
		})
	}
}

// TestRunSubmatchRequiresFullMode: --submatch with prefix/contains used to
// generate a self-contradictory pair (Match(input) true while
// FindSubmatch(input) returned nil, since extraction is full-anchored).
func TestRunSubmatchRequiresFullMode(t *testing.T) {
	for _, m := range []string{"prefix", "contains"} {
		t.Run(m, func(t *testing.T) {
			_, err := execute(t, "--regex", "(a+)b", "--match", m, "--submatch")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--submatch requires --match full")
		})
	}
}

// TestRunSupportedAssertions: anchors and word boundaries that are always
// satisfied at their position keep compiling.
func TestRunSupportedAssertions(t *testing.T) {
	for _, pattern := range []string{`^abc$`, `\babc`, `(\w+)\b`} {
		t.Run(pattern, func(t *testing.T) {
			stdout, err := execute(t, "--regex", pattern)
			require.NoError(t, err)
			assert.Contains(t, stdout, "func Match(input string) bool")
		})
	}
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
