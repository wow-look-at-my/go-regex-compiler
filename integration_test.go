package regexcompiler_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
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
	}

	for _, tt := range tests {
		t.Run(tt.regex, func(t *testing.T) {
			// Build the pipeline
			prog, err := parser.Parse(tt.regex)
			if err != nil {
				t.Fatalf("parser.Parse(%q): %v", tt.regex, err)
			}

			d, err := dfa.Build(prog)
			if err != nil {
				t.Fatalf("dfa.Build(%q): %v", tt.regex, err)
			}

			var buf bytes.Buffer
			opts := codegen.Options{
				PackageName: "main",
				FuncName:    "Match",
				Regex:       tt.regex,
			}
			err = codegen.Generate(&buf, d, opts)
			if err != nil {
				t.Fatalf("codegen.Generate(%q): %v", tt.regex, err)
			}

			// Write generated code + test harness to temp directory
			tmpDir := t.TempDir()

			err = os.WriteFile(filepath.Join(tmpDir, "matcher.go"), buf.Bytes(), 0644)
			if err != nil {
				t.Fatal(err)
			}

			// Build test harness
			var harness bytes.Buffer
			fmt.Fprintln(&harness, "package main")
			fmt.Fprintln(&harness, "")
			fmt.Fprintln(&harness, `import "fmt"`)
			fmt.Fprintln(&harness, `import "os"`)
			fmt.Fprintln(&harness, "")
			fmt.Fprintln(&harness, "func main() {")
			fmt.Fprintln(&harness, "\tfailed := false")

			for i, tc := range tt.cases {
				fmt.Fprintf(&harness, "\tif Match(%q) != %v {\n", tc.input, tc.match)
				fmt.Fprintf(&harness, "\t\tfmt.Fprintln(os.Stderr, %q)\n", fmt.Sprintf("FAIL case %d: Match(%q) expected %v", i, tc.input, tc.match))
				fmt.Fprintln(&harness, "\t\tfailed = true")
				fmt.Fprintln(&harness, "\t}")
			}

			fmt.Fprintln(&harness, "\tif failed {")
			fmt.Fprintln(&harness, "\t\tos.Exit(1)")
			fmt.Fprintln(&harness, "\t}")
			fmt.Fprintln(&harness, "\tfmt.Println(\"PASS\")")
			fmt.Fprintln(&harness, "}")

			err = os.WriteFile(filepath.Join(tmpDir, "main.go"), harness.Bytes(), 0644)
			if err != nil {
				t.Fatal(err)
			}

			// Initialize a go module in the temp directory
			initCmd := exec.Command("go", "mod", "init", "testmod")
			initCmd.Dir = tmpDir
			if out, err := initCmd.CombinedOutput(); err != nil {
				t.Fatalf("go mod init failed: %v\n%s", err, out)
			}

			// Run the generated program
			runCmd := exec.Command("go", "run", ".")
			runCmd.Dir = tmpDir
			out, err := runCmd.CombinedOutput()
			if err != nil {
				t.Fatalf("generated code failed for regex %q:\n%s", tt.regex, out)
			}
		})
	}
}
