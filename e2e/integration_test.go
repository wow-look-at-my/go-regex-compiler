package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
)

type testCase struct {
	input	string
	match	bool
}

func TestIntegration(t *testing.T) {
	tests := []struct {
		regex	string
		cases	[]testCase
	}{
		{
			regex:	"abc",
			cases: []testCase{
				{"abc", true},
				{"", false},
				{"ab", false},
				{"abcd", false},
				{"ABC", false},
				{"xabc", false},
			},
		},
		{
			regex:	"[a-z]+",
			cases: []testCase{
				{"hello", true},
				{"a", true},
				{"abc", true},
				{"", false},
				{"123", false},
				{"Hello", false},
				{"hello world", false},
			},
		},
		{
			regex:	"a*",
			cases: []testCase{
				{"", true},
				{"a", true},
				{"aaa", true},
				{"b", false},
				{"ab", false},
			},
		},
		{
			regex:	"a+",
			cases: []testCase{
				{"a", true},
				{"aaa", true},
				{"", false},
				{"b", false},
				{"ab", false},
			},
		},
		{
			regex:	"a?",
			cases: []testCase{
				{"", true},
				{"a", true},
				{"aa", false},
				{"b", false},
			},
		},
		{
			regex:	"a|b",
			cases: []testCase{
				{"a", true},
				{"b", true},
				{"", false},
				{"c", false},
				{"ab", false},
			},
		},
		{
			regex:	"(a|b)*c",
			cases: []testCase{
				{"c", true},
				{"ac", true},
				{"bc", true},
				{"ababc", true},
				{"aabbc", true},
				{"", false},
				{"a", false},
				{"ab", false},
				{"ca", false},
			},
		},
		{
			regex:	`\d{3}-\d{2}-\d{4}`,
			cases: []testCase{
				{"123-45-6789", true},
				{"000-00-0000", true},
				{"12-45-6789", false},
				{"123-456-789", false},
				{"abc-de-fghi", false},
				{"", false},
			},
		},
		{
			regex:	`[A-Za-z_][A-Za-z0-9_]*`,
			cases: []testCase{
				{"x", true},
				{"_", true},
				{"foo", true},
				{"_bar", true},
				{"camelCase", true},
				{"with123", true},
				{"_under_score", true},
				{"", false},
				{"123abc", false},
				{"-dash", false},
			},
		},
		{
			regex:	`[0-9]+\.[0-9]+`,
			cases: []testCase{
				{"1.0", true},
				{"123.456", true},
				{"0.0", true},
				{"1.", false},
				{".1", false},
				{"123", false},
				{"abc", false},
			},
		},
		{
			regex:	"(foo|bar)baz",
			cases: []testCase{
				{"foobaz", true},
				{"barbaz", true},
				{"baz", false},
				{"fobaz", false},
				{"foobazz", false},
			},
		},
		{
			regex:	`(https?://)?[a-z]+\.[a-z]{2,}`,
			cases: []testCase{
				{"example.com", true},
				{"http://example.com", true},
				{"https://example.com", true},
				{"foo.co", true},
				{"", false},
				{"example", false},
				{"example.c", false},
			},
		},
		{
			regex:	"",
			cases: []testCase{
				{"", true},
				{"a", false},
			},
		},
		{
			regex:	".",
			cases: []testCase{
				{"a", true},
				{"1", true},
				{"Z", true},
				{"", false},
				{"\n", false},
				{"ab", false},
			},
		},
		{
			regex:	"(?i)abc",
			cases: []testCase{
				{"abc", true},
				{"ABC", true},
				{"Abc", true},
				{"aBc", true},
				{"abC", true},
				{"", false},
				{"ab", false},
				{"abcd", false},
			},
		},
		{
			regex:	`\w+`,
			cases: []testCase{
				{"hello", true},
				{"Hello123", true},
				{"_under", true},
				{"a", true},
				{"", false},
				{"hello world", false},
				{"hello!", false},
			},
		},
		{
			regex:	`\s+`,
			cases: []testCase{
				{" ", true},
				{"\t", true},
				{"\n", true},
				{"  \t\n", true},
				{"", false},
				{"a", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.regex, func(t *testing.T) {
			runGeneratedTest(t, tt.regex, codegen.MatchFull, "Match", tt.cases)
		})
	}
}

func TestIntegrationPrefix(t *testing.T) {
	tests := []struct {
		regex	string
		cases	[]testCase
	}{
		{
			regex:	"[a-z]+",
			cases: []testCase{
				{"hello", true},
				{"hello123", true},	// prefix "hello" matches
				{"hello world", true},
				{"", false},
				{"123", false},
			},
		},
		{
			regex:	`\d{3}-\d{2}`,
			cases: []testCase{
				{"123-45", true},
				{"123-45-6789", true},	// prefix matches
				{"12-45", false},
				{"abc", false},
			},
		},
		{
			regex:	"a+b",
			cases: []testCase{
				{"ab", true},
				{"aab", true},
				{"aabcdef", true},	// prefix "aab" matches
				{"a", false},		// no prefix matches (b required)
				{"aaac", false},
				{"", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.regex, func(t *testing.T) {
			runGeneratedTest(t, tt.regex, codegen.MatchPrefix, "MatchPrefix", tt.cases)
		})
	}
}

func TestIntegrationContains(t *testing.T) {
	tests := []struct {
		regex	string
		cases	[]testCase
	}{
		{
			regex:	"[a-z]+",
			cases: []testCase{
				{"hello", true},
				{"123hello456", true},	// substring matches
				{"HELLO", false},	// no lowercase substring
				{"123", false},
				{"", false},
				{"test@example.com", true},
			},
		},
		{
			regex:	`\d{3}-\d{2}-\d{4}`,
			cases: []testCase{
				{"123-45-6789", true},
				{"SSN: 123-45-6789!", true},	// contained
				{"abc", false},
				{"123-45", false},
			},
		},
		{
			regex:	"error",
			cases: []testCase{
				{"error", true},
				{"an error occurred", true},
				{"ERROR", false},
				{"", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.regex, func(t *testing.T) {
			runGeneratedTest(t, tt.regex, codegen.MatchContains, "MatchContains", tt.cases)
		})
	}
}

type submatchCase struct {
	input	string
	groups	[]string	// nil means no match, else groups[0]=full match, groups[1..]=captures
}

func TestIntegrationSubmatch(t *testing.T) {
	tests := []struct {
		regex	string
		cases	[]submatchCase
	}{
		{
			regex:	`([a-z]+)@([a-z]+)`,
			cases: []submatchCase{
				{"user@host", []string{"user@host", "user", "host"}},
				{"abc@xyz", []string{"abc@xyz", "abc", "xyz"}},
				{"123", nil},
				{"", nil},
			},
		},
		{
			regex:	`(\d{3})-(\d{2})-(\d{4})`,
			cases: []submatchCase{
				{"123-45-6789", []string{"123-45-6789", "123", "45", "6789"}},
				{"000-00-0000", []string{"000-00-0000", "000", "00", "0000"}},
				{"abc", nil},
			},
		},
		{
			regex:	`(a+)(b+)`,
			cases: []submatchCase{
				{"ab", []string{"ab", "a", "b"}},
				{"aaabbb", []string{"aaabbb", "aaa", "bbb"}},
				{"a", nil},
				{"b", nil},
			},
		},
		{
			regex:	`(foo|bar)baz`,
			cases: []submatchCase{
				{"foobaz", []string{"foobaz", "foo"}},
				{"barbaz", []string{"barbaz", "bar"}},
				{"baz", nil},
			},
		},
		{
			regex:	`([a-z]+)(\.[a-z]+)*`,
			cases: []submatchCase{
				{"hello", []string{"hello", "hello", ""}},
				{"abc.def", []string{"abc.def", "abc", ".def"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.regex, func(t *testing.T) {
			runGeneratedSubmatchTest(t, tt.regex, tt.cases)
		})
	}
}

// runGeneratedSubmatchTest generates code with submatch support, compiles, and verifies capture groups.
func runGeneratedSubmatchTest(t *testing.T, regex string, cases []submatchCase) {
	t.Helper()

	result, err := parser.ParseResult(regex)
	require.NoError(t, err)

	d, err := dfa.Build(result.Prog)
	require.NoError(t, err)

	var buf bytes.Buffer
	opts := codegen.Options{
		PackageName:	"main",
		FuncName:	"Match",
		Regex:		regex,
		Mode:		codegen.MatchFull,
	}
	if result.NumGroups > 0 {
		opts.Submatch = &codegen.SubmatchOptions{
			PackageName:	"main",
			FuncName:	"FindSubmatch",
			MatchFunc:	"Match",
			Regex:		regex,
			Prog:		result.Prog,
			NumGroups:	result.NumGroups,
		}
	}
	err = codegen.Generate(&buf, d, opts)
	require.NoError(t, err)

	tmpDir := t.TempDir()

	err = os.WriteFile(filepath.Join(tmpDir, "matcher.go"), buf.Bytes(), 0644)
	require.NoError(t, err)

	// Write generated code to stderr for debugging
	t.Logf("Generated code for %q:\n%s", regex, buf.String())

	var harness bytes.Buffer
	fmt.Fprintln(&harness, "package main")
	fmt.Fprintln(&harness, "")
	fmt.Fprintln(&harness, `import "fmt"`)
	fmt.Fprintln(&harness, `import "os"`)
	fmt.Fprintln(&harness, `import "strings"`)
	fmt.Fprintln(&harness, "")
	fmt.Fprintln(&harness, "func main() {")
	fmt.Fprintln(&harness, "\tfailed := false")

	for i, tc := range cases {
		if tc.groups == nil {
			// Expect nil (no match)
			fmt.Fprintf(&harness, "\tif result := FindSubmatch(%q); result != nil {\n", tc.input)
			fmt.Fprintf(&harness, "\t\tfmt.Fprintf(os.Stderr, \"FAIL case %d: FindSubmatch(%%q) expected nil, got %%v\\n\", %q, result)\n", i, tc.input)
			fmt.Fprintln(&harness, "\t\tfailed = true")
			fmt.Fprintln(&harness, "\t}")
		} else {
			// Expect specific groups
			fmt.Fprintf(&harness, "\t{\n")
			fmt.Fprintf(&harness, "\t\tresult := FindSubmatch(%q)\n", tc.input)
			fmt.Fprintf(&harness, "\t\texpected := []string{%s}\n", joinQuoted(tc.groups))
			fmt.Fprintf(&harness, "\t\tif result == nil {\n")
			fmt.Fprintf(&harness, "\t\t\tfmt.Fprintf(os.Stderr, \"FAIL case %d: FindSubmatch(%%q) returned nil, expected %%v\\n\", %q, expected)\n", i, tc.input)
			fmt.Fprintf(&harness, "\t\t\tfailed = true\n")
			fmt.Fprintf(&harness, "\t\t} else if strings.Join(result, \",\") != strings.Join(expected, \",\") {\n")
			fmt.Fprintf(&harness, "\t\t\tfmt.Fprintf(os.Stderr, \"FAIL case %d: FindSubmatch(%%q) = %%v, expected %%v\\n\", %q, result, expected)\n", i, tc.input)
			fmt.Fprintf(&harness, "\t\t\tfailed = true\n")
			fmt.Fprintf(&harness, "\t\t}\n")
			fmt.Fprintf(&harness, "\t}\n")
		}
	}

	fmt.Fprintln(&harness, "\tif failed {")
	fmt.Fprintln(&harness, "\t\tos.Exit(1)")
	fmt.Fprintln(&harness, "\t}")
	fmt.Fprintln(&harness, "\tfmt.Println(\"PASS\")")
	fmt.Fprintln(&harness, "}")

	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), harness.Bytes(), 0644)
	require.NoError(t, err)

	initCmd := exec.Command("go", "mod", "init", "testmod")
	initCmd.Dir = tmpDir
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "go mod init failed: %s", out)

	runCmd := exec.Command("go", "run", ".")
	runCmd.Dir = tmpDir
	out, err = runCmd.CombinedOutput()
	assert.NoError(t, err, "generated submatch code failed for regex %q:\n%s", regex, out)
}

func joinQuoted(ss []string) string {
	var parts []string
	for _, s := range ss {
		parts = append(parts, fmt.Sprintf("%q", s))
	}
	return strings.Join(parts, ", ")
}

// runGeneratedTest generates code for a pattern with the given mode, compiles, and runs test cases.
func runGeneratedTest(t *testing.T, regex string, mode codegen.MatchMode, funcName string, cases []testCase) {
	t.Helper()

	prog, err := parser.Parse(regex)
	require.NoError(t, err)

	d, err := dfa.Build(prog)
	require.NoError(t, err)

	var buf bytes.Buffer
	opts := codegen.Options{
		PackageName:	"main",
		FuncName:	funcName,
		Regex:		regex,
		Mode:		mode,
	}
	err = codegen.Generate(&buf, d, opts)
	require.NoError(t, err)

	tmpDir := t.TempDir()

	err = os.WriteFile(filepath.Join(tmpDir, "matcher.go"), buf.Bytes(), 0644)
	require.NoError(t, err)

	var harness bytes.Buffer
	fmt.Fprintln(&harness, "package main")
	fmt.Fprintln(&harness, "")
	fmt.Fprintln(&harness, `import "fmt"`)
	fmt.Fprintln(&harness, `import "os"`)
	fmt.Fprintln(&harness, "")
	fmt.Fprintln(&harness, "func main() {")
	fmt.Fprintln(&harness, "\tfailed := false")

	for i, tc := range cases {
		fmt.Fprintf(&harness, "\tif %s(%q) != %v {\n", funcName, tc.input, tc.match)
		fmt.Fprintf(&harness, "\t\tfmt.Fprintln(os.Stderr, %q)\n", fmt.Sprintf("FAIL case %d: %s(%q) expected %v", i, funcName, tc.input, tc.match))
		fmt.Fprintln(&harness, "\t\tfailed = true")
		fmt.Fprintln(&harness, "\t}")
	}

	fmt.Fprintln(&harness, "\tif failed {")
	fmt.Fprintln(&harness, "\t\tos.Exit(1)")
	fmt.Fprintln(&harness, "\t}")
	fmt.Fprintln(&harness, "\tfmt.Println(\"PASS\")")
	fmt.Fprintln(&harness, "}")

	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), harness.Bytes(), 0644)
	require.NoError(t, err)

	initCmd := exec.Command("go", "mod", "init", "testmod")
	initCmd.Dir = tmpDir
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "go mod init failed: %s", out)

	runCmd := exec.Command("go", "run", ".")
	runCmd.Dir = tmpDir
	out, err = runCmd.CombinedOutput()
	assert.NoError(t, err, "generated code failed for regex %q (mode %d):\n%s", regex, mode, out)
}
