package e2e_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
)

type testCase struct {
	input string
	match bool
}

func TestIntegration(t *testing.T) {
	tests := []struct {
		regex string
		cases []testCase
	}{
		{
			regex: "abc",
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
			regex: "[a-z]+",
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
			regex: "a*",
			cases: []testCase{
				{"", true},
				{"a", true},
				{"aaa", true},
				{"b", false},
				{"ab", false},
			},
		},
		{
			regex: "a+",
			cases: []testCase{
				{"a", true},
				{"aaa", true},
				{"", false},
				{"b", false},
				{"ab", false},
			},
		},
		{
			regex: "a?",
			cases: []testCase{
				{"", true},
				{"a", true},
				{"aa", false},
				{"b", false},
			},
		},
		{
			regex: "a|b",
			cases: []testCase{
				{"a", true},
				{"b", true},
				{"", false},
				{"c", false},
				{"ab", false},
			},
		},
		{
			regex: "(a|b)*c",
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
			regex: `\d{3}-\d{2}-\d{4}`,
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
			regex: `[A-Za-z_][A-Za-z0-9_]*`,
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
			regex: `[0-9]+\.[0-9]+`,
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
			regex: "(foo|bar)baz",
			cases: []testCase{
				{"foobaz", true},
				{"barbaz", true},
				{"baz", false},
				{"fobaz", false},
				{"foobazz", false},
			},
		},
		{
			regex: `(https?://)?[a-z]+\.[a-z]{2,}`,
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
			regex: "",
			cases: []testCase{
				{"", true},
				{"a", false},
			},
		},
		{
			regex: ".",
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
			regex: "(?i)abc",
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
			regex: `\w+`,
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
			regex: `\s+`,
			cases: []testCase{
				{" ", true},
				{"\t", true},
				{"\n", true},
				{"  \t\n", true},
				{"", false},
				{"a", false},
			},
		},
		{
			regex: `[a-z0-9][a-z0-9._-]{0,127}`,
			cases: []testCase{
				{"a", true},
				{"abc", true},
				{"a123", true},
				{"project.name", true},
				{"my-project", true},
				{"my_project", true},
				{"a.b-c_d", true},
				{"9start", true},
				{strings.Repeat("a", 128), true},
				{"", false},
				{"A", false},
				{"-start", false},
				{".start", false},
				{"_start", false},
				{strings.Repeat("a", 129), false},
				{"has space", false},
				{"UPPER", false},
			},
		},
	}

	runBatchedMatchTest(t, tests, codegen.MatchFull)
}

func TestIntegrationPrefix(t *testing.T) {
	tests := []struct {
		regex string
		cases []testCase
	}{
		{
			regex: "[a-z]+",
			cases: []testCase{
				{"hello", true},
				{"hello123", true},
				{"hello world", true},
				{"", false},
				{"123", false},
			},
		},
		{
			regex: `\d{3}-\d{2}`,
			cases: []testCase{
				{"123-45", true},
				{"123-45-6789", true},
				{"12-45", false},
				{"abc", false},
			},
		},
		{
			regex: "a+b",
			cases: []testCase{
				{"ab", true},
				{"aab", true},
				{"aabcdef", true},
				{"a", false},
				{"aaac", false},
				{"", false},
			},
		},
	}

	runBatchedMatchTest(t, tests, codegen.MatchPrefix)
}

func TestIntegrationContains(t *testing.T) {
	tests := []struct {
		regex string
		cases []testCase
	}{
		{
			regex: "[a-z]+",
			cases: []testCase{
				{"hello", true},
				{"123hello456", true},
				{"HELLO", false},
				{"123", false},
				{"", false},
				{"test@example.com", true},
			},
		},
		{
			regex: `\d{3}-\d{2}-\d{4}`,
			cases: []testCase{
				{"123-45-6789", true},
				{"SSN: 123-45-6789!", true},
				{"abc", false},
				{"123-45", false},
			},
		},
		{
			regex: "error",
			cases: []testCase{
				{"error", true},
				{"an error occurred", true},
				{"ERROR", false},
				{"", false},
			},
		},
	}

	runBatchedMatchTest(t, tests, codegen.MatchContains)
}

// runBatchedMatchTest generates all matchers, compiles them in a single binary, and runs assertions.
func runBatchedMatchTest(t *testing.T, tests []struct {
	regex string
	cases []testCase
}, mode codegen.MatchMode) {
	t.Helper()

	tmpDir := t.TempDir()

	funcNames := make([]string, len(tests))
	for i, tt := range tests {
		funcNames[i] = fmt.Sprintf("Match%d", i)
		src := generateNamed(t, tt.regex, funcNames[i], mode)
		writeGenerated(t, tmpDir, fmt.Sprintf("match_%d.go", i), src)
	}

	var harness bytes.Buffer
	harness.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() {\n\tfailed := false\n")

	for i, tt := range tests {
		for j, tc := range tt.cases {
			fmt.Fprintf(&harness, "\tif %s(%q) != %v {\n", funcNames[i], tc.input, tc.match)
			fmt.Fprintf(&harness, "\t\tfmt.Fprintln(os.Stderr, %q)\n",
				fmt.Sprintf("FAIL [%s] case %d: %s(%q) expected %v", tt.regex, j, funcNames[i], tc.input, tc.match))
			harness.WriteString("\t\tfailed = true\n\t}\n")
		}
	}

	harness.WriteString("\tif failed {\n\t\tos.Exit(1)\n\t}\n\tfmt.Println(\"PASS\")\n}\n")
	writeGenerated(t, tmpDir, "main.go", harness.Bytes())
	modInitAndRun(t, tmpDir)
}

type submatchCase struct {
	input  string
	groups []string
}

func TestIntegrationSubmatch(t *testing.T) {
	tests := []struct {
		regex string
		cases []submatchCase
	}{
		{
			regex: `([a-z]+)@([a-z]+)`,
			cases: []submatchCase{
				{"user@host", []string{"user@host", "user", "host"}},
				{"abc@xyz", []string{"abc@xyz", "abc", "xyz"}},
				{"123", nil},
				{"", nil},
			},
		},
		{
			regex: `(\d{3})-(\d{2})-(\d{4})`,
			cases: []submatchCase{
				{"123-45-6789", []string{"123-45-6789", "123", "45", "6789"}},
				{"000-00-0000", []string{"000-00-0000", "000", "00", "0000"}},
				{"abc", nil},
			},
		},
		{
			regex: `(a+)(b+)`,
			cases: []submatchCase{
				{"ab", []string{"ab", "a", "b"}},
				{"aaabbb", []string{"aaabbb", "aaa", "bbb"}},
				{"a", nil},
				{"b", nil},
			},
		},
		{
			regex: `(foo|bar)baz`,
			cases: []submatchCase{
				{"foobaz", []string{"foobaz", "foo"}},
				{"barbaz", []string{"barbaz", "bar"}},
				{"baz", nil},
			},
		},
		{
			regex: `([a-z]+)(\.[a-z]+)*`,
			cases: []submatchCase{
				{"hello", []string{"hello", "hello", ""}},
				{"abc.def", []string{"abc.def", "abc", ".def"}},
			},
		},
	}

	tmpDir := t.TempDir()

	for i, tt := range tests {
		matchFunc := fmt.Sprintf("Match%d", i)
		submatchFunc := fmt.Sprintf("FindSubmatch%d", i)
		src := generateWithSubmatch(t, tt.regex, matchFunc, submatchFunc)
		writeGenerated(t, tmpDir, fmt.Sprintf("match_%d.go", i), src)
	}

	var harness bytes.Buffer
	harness.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n)\n\nfunc main() {\n\tfailed := false\n")

	for i, tt := range tests {
		submatchFunc := fmt.Sprintf("FindSubmatch%d", i)
		for j, tc := range tt.cases {
			if tc.groups == nil {
				msg := fmt.Sprintf("FAIL [%s] case %d: %s(%q) expected nil", tt.regex, j, submatchFunc, tc.input)
				fmt.Fprintf(&harness, "\tif result := %s(%q); result != nil {\n", submatchFunc, tc.input)
				fmt.Fprintf(&harness, "\t\tfmt.Fprintln(os.Stderr, %q, result)\n", msg+", got:")
				harness.WriteString("\t\tfailed = true\n\t}\n")
			} else {
				nilMsg := fmt.Sprintf("FAIL [%s] case %d: %s(%q) returned nil, expected %v", tt.regex, j, submatchFunc, tc.input, tc.groups)
				wrongMsg := fmt.Sprintf("FAIL [%s] case %d: %s(%q) wrong groups, expected %v, got:", tt.regex, j, submatchFunc, tc.input, tc.groups)
				fmt.Fprintf(&harness, "\t{\n")
				fmt.Fprintf(&harness, "\t\tresult := %s(%q)\n", submatchFunc, tc.input)
				fmt.Fprintf(&harness, "\t\texpected := []string{%s}\n", joinQuoted(tc.groups))
				fmt.Fprintf(&harness, "\t\tif result == nil {\n")
				fmt.Fprintf(&harness, "\t\t\tfmt.Fprintln(os.Stderr, %q)\n", nilMsg)
				fmt.Fprintf(&harness, "\t\t\tfailed = true\n")
				fmt.Fprintf(&harness, "\t\t} else if strings.Join(result, \",\") != strings.Join(expected, \",\") {\n")
				fmt.Fprintf(&harness, "\t\t\tfmt.Fprintln(os.Stderr, %q, result)\n", wrongMsg)
				fmt.Fprintf(&harness, "\t\t\tfailed = true\n")
				fmt.Fprintf(&harness, "\t\t}\n")
				fmt.Fprintf(&harness, "\t}\n")
			}
		}
	}

	harness.WriteString("\tif failed {\n\t\tos.Exit(1)\n\t}\n\tfmt.Println(\"PASS\")\n}\n")
	writeGenerated(t, tmpDir, "main.go", harness.Bytes())
	modInitAndRun(t, tmpDir)
}

func joinQuoted(ss []string) string {
	var parts []string
	for _, s := range ss {
		parts = append(parts, fmt.Sprintf("%q", s))
	}
	return strings.Join(parts, ", ")
}
