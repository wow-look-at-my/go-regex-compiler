package regexcompiler_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
)

// testPatterns are the regex patterns used across correctness, benchmark, and size tests.
var testPatterns = []struct {
	name    string
	pattern string
}{
	{"literal", "abc"},
	{"char_class", "[a-z]+"},
	{"alternation", "a|b|c"},
	{"star", "a*b"},
	{"plus", "[0-9]+"},
	{"question", "colou?r"},
	{"grouped_alt", "(foo|bar|baz)qux"},
	{"ssn", `\d{3}-\d{2}-\d{4}`},
	{"identifier", `[A-Za-z_][A-Za-z0-9_]*`},
	{"dotted_number", `[0-9]+\.[0-9]+`},
	{"url_like", `(https?://)?[a-z]+\.[a-z]{2,}`},
	{"complex_alt", "(a|b)*abb"},
	{"case_insensitive", "(?i)hello"},
	{"whitespace", `\s+`},
	{"word_chars", `\w+`},
	{"dot", ".+"},
	{"empty", ""},
	{"nested_quant", "(ab?c)+"},
	{"email_like", `[a-z]+@[a-z]+\.[a-z]{2,}`},
	{"hex_color", `#[0-9a-f]{6}`},
}

// testInputs are diverse inputs used for correctness comparison against regexp.
var testInputs = []string{
	"",
	"a", "b", "c", "ab", "abc", "abcd", "abcabc",
	"foo", "bar", "baz", "fooqux", "barqux", "bazqux", "qux",
	"hello", "Hello", "HELLO", "hElLo",
	"colour", "color",
	"123", "456-78-9012", "000-00-0000", "12-34-5678",
	"x", "_", "foo123", "_bar", "123abc", "camelCase",
	"1.0", "3.14", "100.200", ".5", "5.", "abc.def",
	"http://example.com", "https://test.org", "example.com", "ftp://x.co",
	"aabb", "abb", "aab", "babb", "aababb",
	" ", "\t", "\n", "  \t\n", "abc def",
	"abc123_def", "hello_world", "ALL_CAPS",
	"a", "Z", "0", "9", "!", "@", "#",
	"#aabbcc", "#123456", "#gghhii", "#abc",
	"user@example.com", "test@foo.co", "a@b.cd", "@missing.com",
	"aabcabc", "abccc", "abc abc",
	strings.Repeat("a", 100),
	strings.Repeat("ab", 50),
	strings.Repeat("x", 1000),
}

// generate runs the regex-gen pipeline and returns the generated Go source code.
func generate(t *testing.T, pattern string) []byte {
	t.Helper()
	prog, err := parser.Parse(pattern)
	if err != nil {
		t.Fatalf("parser.Parse(%q): %v", pattern, err)
	}
	d, err := dfa.Build(prog)
	if err != nil {
		t.Fatalf("dfa.Build(%q): %v", pattern, err)
	}
	var buf bytes.Buffer
	opts := codegen.Options{
		PackageName: "main",
		FuncName:    "Match",
		Regex:       pattern,
	}
	if err := codegen.Generate(&buf, d, opts); err != nil {
		t.Fatalf("codegen.Generate(%q): %v", pattern, err)
	}
	return buf.Bytes()
}

// anchoredRegexp returns a compiled regexp that does full-string matching.
func anchoredRegexp(pattern string) *regexp.Regexp {
	return regexp.MustCompile("^(?:" + pattern + ")$")
}

// TestCorrectnessVsRegexp generates code for each pattern, compiles and runs it
// against all test inputs, and compares the result to regexp.MatchString.
func TestCorrectnessVsRegexp(t *testing.T) {
	for _, tp := range testPatterns {
		t.Run(tp.name, func(t *testing.T) {
			src := generate(t, tp.pattern)
			re := anchoredRegexp(tp.pattern)

			// Build a program that tests every input and reports mismatches
			tmpDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(tmpDir, "matcher.go"), src, 0644); err != nil {
				t.Fatal(err)
			}

			var harness bytes.Buffer
			harness.WriteString("package main\n\n")
			harness.WriteString("import (\n\t\"fmt\"\n\t\"os\"\n)\n\n")
			harness.WriteString("func main() {\n")
			harness.WriteString("\tfailed := false\n")

			for _, input := range testInputs {
				expected := re.MatchString(input)
				fmt.Fprintf(&harness, "\tif Match(%q) != %v {\n", input, expected)
				fmt.Fprintf(&harness, "\t\tfmt.Fprintf(os.Stderr, \"MISMATCH: Match(%%q) = %%v, want %v\\n\", %q, Match(%q))\n", expected, input, input)
				harness.WriteString("\t\tfailed = true\n")
				harness.WriteString("\t}\n")
			}

			harness.WriteString("\tif failed {\n\t\tos.Exit(1)\n\t}\n")
			harness.WriteString("\tfmt.Println(\"PASS\")\n}\n")

			if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), harness.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}

			initCmd := exec.Command("go", "mod", "init", "testmod")
			initCmd.Dir = tmpDir
			if out, err := initCmd.CombinedOutput(); err != nil {
				t.Fatalf("go mod init: %v\n%s", err, out)
			}

			runCmd := exec.Command("go", "run", ".")
			runCmd.Dir = tmpDir
			out, err := runCmd.CombinedOutput()
			if err != nil {
				t.Fatalf("correctness mismatch for pattern %q:\n%s", tp.pattern, out)
			}
		})
	}
}

// TestCodeSize reports the generated code size (bytes and lines) for each pattern.
func TestCodeSize(t *testing.T) {
	for _, tp := range testPatterns {
		t.Run(tp.name, func(t *testing.T) {
			src := generate(t, tp.pattern)
			lines := bytes.Count(src, []byte("\n"))
			t.Logf("pattern=%-35s  bytes=%-6d  lines=%-4d", fmt.Sprintf("%q", tp.pattern), len(src), lines)
		})
	}
}

// TestBenchmarkVsRegexp generates code for each pattern, compiles a benchmark
// binary, and runs it. The benchmark compares generated code vs regexp.MatchString.
func TestBenchmarkVsRegexp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmarks in short mode")
	}

	for _, tp := range testPatterns {
		t.Run(tp.name, func(t *testing.T) {
			src := generate(t, tp.pattern)
			tmpDir := t.TempDir()

			if err := os.WriteFile(filepath.Join(tmpDir, "matcher.go"), src, 0644); err != nil {
				t.Fatal(err)
			}

			// Create a benchmark file that tests both approaches
			var bench bytes.Buffer
			bench.WriteString("package main\n\n")
			bench.WriteString("import (\n\t\"regexp\"\n\t\"testing\"\n)\n\n")

			// Pick representative inputs: one that matches, one that doesn't, one long
			re := anchoredRegexp(tp.pattern)
			var matchInput, noMatchInput string
			for _, input := range testInputs {
				if re.MatchString(input) && matchInput == "" {
					matchInput = input
				}
				if !re.MatchString(input) && noMatchInput == "" && input != "" {
					noMatchInput = input
				}
				if matchInput != "" && noMatchInput != "" {
					break
				}
			}
			if matchInput == "" {
				matchInput = ""
			}
			if noMatchInput == "" {
				noMatchInput = "ZZZZZ_no_match"
			}

			fmt.Fprintf(&bench, "var re = regexp.MustCompile(%q)\n\n", "^(?:"+tp.pattern+")$")

			// Benchmark generated code - match
			fmt.Fprintf(&bench, "func BenchmarkGenerated_Match(b *testing.B) {\n")
			fmt.Fprintf(&bench, "\tinput := %q\n", matchInput)
			fmt.Fprintf(&bench, "\tfor b.Loop() {\n")
			fmt.Fprintf(&bench, "\t\tMatch(input)\n")
			fmt.Fprintf(&bench, "\t}\n}\n\n")

			// Benchmark regexp - match
			fmt.Fprintf(&bench, "func BenchmarkRegexp_Match(b *testing.B) {\n")
			fmt.Fprintf(&bench, "\tinput := %q\n", matchInput)
			fmt.Fprintf(&bench, "\tfor b.Loop() {\n")
			fmt.Fprintf(&bench, "\t\tre.MatchString(input)\n")
			fmt.Fprintf(&bench, "\t}\n}\n\n")

			// Benchmark generated code - no match
			fmt.Fprintf(&bench, "func BenchmarkGenerated_NoMatch(b *testing.B) {\n")
			fmt.Fprintf(&bench, "\tinput := %q\n", noMatchInput)
			fmt.Fprintf(&bench, "\tfor b.Loop() {\n")
			fmt.Fprintf(&bench, "\t\tMatch(input)\n")
			fmt.Fprintf(&bench, "\t}\n}\n\n")

			// Benchmark regexp - no match
			fmt.Fprintf(&bench, "func BenchmarkRegexp_NoMatch(b *testing.B) {\n")
			fmt.Fprintf(&bench, "\tinput := %q\n", noMatchInput)
			fmt.Fprintf(&bench, "\tfor b.Loop() {\n")
			fmt.Fprintf(&bench, "\t\tre.MatchString(input)\n")
			fmt.Fprintf(&bench, "\t}\n}\n\n")

			// Benchmark generated code - long input
			longInput := strings.Repeat(matchInput+"x", 100)
			fmt.Fprintf(&bench, "func BenchmarkGenerated_Long(b *testing.B) {\n")
			fmt.Fprintf(&bench, "\tinput := %q\n", longInput)
			fmt.Fprintf(&bench, "\tfor b.Loop() {\n")
			fmt.Fprintf(&bench, "\t\tMatch(input)\n")
			fmt.Fprintf(&bench, "\t}\n}\n\n")

			// Benchmark regexp - long input
			fmt.Fprintf(&bench, "func BenchmarkRegexp_Long(b *testing.B) {\n")
			fmt.Fprintf(&bench, "\tinput := %q\n", longInput)
			fmt.Fprintf(&bench, "\tfor b.Loop() {\n")
			fmt.Fprintf(&bench, "\t\tre.MatchString(input)\n")
			fmt.Fprintf(&bench, "\t}\n}\n")

			if err := os.WriteFile(filepath.Join(tmpDir, "matcher_test.go"), bench.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}

			initCmd := exec.Command("go", "mod", "init", "testmod")
			initCmd.Dir = tmpDir
			if out, err := initCmd.CombinedOutput(); err != nil {
				t.Fatalf("go mod init: %v\n%s", err, out)
			}

			runCmd := exec.Command("go", "test", "-bench=.", "-benchmem", "-count=1", "-benchtime=100ms")
			runCmd.Dir = tmpDir
			out, err := runCmd.CombinedOutput()
			if err != nil {
				t.Fatalf("benchmark failed for %q:\n%s", tp.pattern, out)
			}

			t.Logf("pattern %q:\n%s", tp.pattern, out)
		})
	}
}
