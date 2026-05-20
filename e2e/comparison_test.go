package e2e_test

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

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

func anchoredRegexp(pattern string) *regexp.Regexp {
	return regexp.MustCompile("^(?:" + pattern + ")$")
}

// TestCorrectnessVsRegexp generates code for all patterns, compiles them in a
// single binary, and compares results against regexp.MatchString.
func TestCorrectnessVsRegexp(t *testing.T) {
	tmpDir := t.TempDir()

	for i, tp := range testPatterns {
		funcName := fmt.Sprintf("Match%d", i)
		src := generateNamed(t, tp.pattern, funcName, codegen.MatchFull)
		writeGenerated(t, tmpDir, fmt.Sprintf("matcher_%d.go", i), src)
	}

	var harness bytes.Buffer
	harness.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() {\n\tfailed := false\n")

	for i, tp := range testPatterns {
		re := anchoredRegexp(tp.pattern)
		funcName := fmt.Sprintf("Match%d", i)
		for _, input := range testInputs {
			expected := re.MatchString(input)
			fmt.Fprintf(&harness, "\tif %s(%q) != %v {\n", funcName, input, expected)
			fmt.Fprintf(&harness, "\t\tfmt.Fprintf(os.Stderr, \"MISMATCH [%s]: %s(%%q) = %%v, want %v\\n\", %q, %s(%q))\n",
				tp.name, funcName, expected, input, funcName, input)
			harness.WriteString("\t\tfailed = true\n\t}\n")
		}
	}

	harness.WriteString("\tif failed {\n\t\tos.Exit(1)\n\t}\n\tfmt.Println(\"PASS\")\n}\n")
	writeGenerated(t, tmpDir, "main.go", harness.Bytes())
	modInitAndRun(t, tmpDir)
}

// TestCodeSize reports the generated code size (bytes and lines) for each pattern.
func TestCodeSize(t *testing.T) {
	for _, tp := range testPatterns {
		t.Run(tp.name, func(t *testing.T) {
			src := generateNamed(t, tp.pattern, "Match", codegen.MatchFull)
			lines := bytes.Count(src, []byte("\n"))
			assert.NotEmpty(t, src, "generated code should not be empty")
			t.Logf("pattern=%-35s  bytes=%-6d  lines=%-4d", fmt.Sprintf("%q", tp.pattern), len(src), lines)
		})
	}
}

// TestBenchmarkVsRegexp generates code for all patterns, compiles a single
// benchmark binary, and runs it.
func TestBenchmarkVsRegexp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmarks in short mode")
	}

	tmpDir := t.TempDir()

	for i, tp := range testPatterns {
		funcName := fmt.Sprintf("Match%d", i)
		src := generateNamed(t, tp.pattern, funcName, codegen.MatchFull)
		writeGenerated(t, tmpDir, fmt.Sprintf("matcher_%d.go", i), src)
	}

	var bench bytes.Buffer
	bench.WriteString("package main\n\nimport (\n\t\"regexp\"\n\t\"testing\"\n)\n\n")

	for i, tp := range testPatterns {
		re := anchoredRegexp(tp.pattern)
		funcName := fmt.Sprintf("Match%d", i)

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
		if noMatchInput == "" {
			noMatchInput = "ZZZZZ_no_match"
		}

		fmt.Fprintf(&bench, "var re%d = regexp.MustCompile(%q)\n\n", i, "^(?:"+tp.pattern+")$")

		fmt.Fprintf(&bench, "func BenchmarkGenerated_%s_Match(b *testing.B) {\n\tinput := %q\n\tfor b.Loop() {\n\t\t%s(input)\n\t}\n}\n\n",
			tp.name, matchInput, funcName)
		fmt.Fprintf(&bench, "func BenchmarkRegexp_%s_Match(b *testing.B) {\n\tinput := %q\n\tfor b.Loop() {\n\t\tre%d.MatchString(input)\n\t}\n}\n\n",
			tp.name, matchInput, i)

		fmt.Fprintf(&bench, "func BenchmarkGenerated_%s_NoMatch(b *testing.B) {\n\tinput := %q\n\tfor b.Loop() {\n\t\t%s(input)\n\t}\n}\n\n",
			tp.name, noMatchInput, funcName)
		fmt.Fprintf(&bench, "func BenchmarkRegexp_%s_NoMatch(b *testing.B) {\n\tinput := %q\n\tfor b.Loop() {\n\t\tre%d.MatchString(input)\n\t}\n}\n\n",
			tp.name, noMatchInput, i)

		longInput := strings.Repeat(matchInput+"x", 100)
		fmt.Fprintf(&bench, "func BenchmarkGenerated_%s_Long(b *testing.B) {\n\tinput := %q\n\tfor b.Loop() {\n\t\t%s(input)\n\t}\n}\n\n",
			tp.name, longInput, funcName)
		fmt.Fprintf(&bench, "func BenchmarkRegexp_%s_Long(b *testing.B) {\n\tinput := %q\n\tfor b.Loop() {\n\t\tre%d.MatchString(input)\n\t}\n}\n\n",
			tp.name, longInput, i)
	}

	writeGenerated(t, tmpDir, "matcher_test.go", bench.Bytes())

	initCmd := exec.Command("go", "mod", "init", "testmod")
	initCmd.Dir = tmpDir
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "go mod init: %s", out)

	runCmd := exec.Command("go", "test", "-bench=.", "-benchmem", "-count=1", "-benchtime=100ms")
	runCmd.Dir = tmpDir
	out, err = runCmd.CombinedOutput()
	require.NoError(t, err, "benchmark failed:\n%s", out)
	t.Logf("benchmarks:\n%s", out)
}
