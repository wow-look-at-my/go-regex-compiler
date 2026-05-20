package e2e

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
	"github.com/wow-look-at-my/testify/assert"
)

// testPatterns are the regex patterns used across correctness, benchmark, and size tests.
var testPatterns = []struct {
	name    string
	pattern string
	matchFn func(string) bool
}{
	{"literal", "abc", MatchLiteral},
	{"char_class", "[a-z]+", MatchCharClass},
	{"alternation", "a|b|c", MatchAlternation},
	{"star", "a*b", MatchStarB},
	{"plus", "[0-9]+", MatchDigits},
	{"question", "colou?r", MatchColour},
	{"grouped_alt", "(foo|bar|baz)qux", MatchGroupedAltQux},
	{"ssn", `\d{3}-\d{2}-\d{4}`, MatchSSN},
	{"identifier", `[A-Za-z_][A-Za-z0-9_]*`, MatchIdentifier},
	{"dotted_number", `[0-9]+\.[0-9]+`, MatchDottedNumber},
	{"url_like", `(https?://)?[a-z]+\.[a-z]{2,}`, MatchURL},
	{"complex_alt", "(a|b)*abb", MatchComplexAlt},
	{"case_insensitive", "(?i)hello", MatchCaseIHello},
	{"whitespace", `\s+`, MatchWhitespace},
	{"word_chars", `\w+`, MatchWordChars},
	{"dot", ".+", MatchDotPlus},
	{"empty", "", MatchEmpty},
	{"nested_quant", "(ab?c)+", MatchNestedQuant},
	{"email_like", `[a-z]+@[a-z]+\.[a-z]{2,}`, MatchEmail},
	{"hex_color", `#[0-9a-f]{6}`, MatchHexColor},
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

// anchoredRegexp returns a compiled regexp that does full-string matching.
func anchoredRegexp(pattern string) *regexp.Regexp {
	return regexp.MustCompile("^(?:" + pattern + ")$")
}

// TestCorrectnessVsRegexp verifies each pre-generated matcher produces the
// same results as regexp.MatchString for all test inputs.
func TestCorrectnessVsRegexp(t *testing.T) {
	for _, tp := range testPatterns {
		t.Run(tp.name, func(t *testing.T) {
			t.Parallel()
			re := anchoredRegexp(tp.pattern)
			for _, input := range testInputs {
				expected := re.MatchString(input)
				got := tp.matchFn(input)
				if got != expected {
					t.Errorf("Match(%q) = %v, want %v", input, got, expected)
				}
			}
		})
	}
}

// TestCodeSize reports the generated code size (bytes and lines) for each pattern.
func TestCodeSize(t *testing.T) {
	for _, tp := range testPatterns {
		t.Run(tp.name, func(t *testing.T) {
			prog, err := parser.Parse(tp.pattern)
			if err != nil {
				t.Fatal(err)
			}
			d, err := dfa.Build(prog)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			opts := codegen.Options{
				PackageName: "test",
				FuncName:    "Match",
				Regex:       tp.pattern,
			}
			if err := codegen.Generate(&buf, d, opts); err != nil {
				t.Fatal(err)
			}
			lines := bytes.Count(buf.Bytes(), []byte("\n"))
			assert.NotEmpty(t, buf.Bytes(), "generated code should not be empty")
			t.Logf("pattern=%-35s  bytes=%-6d  lines=%-4d", fmt.Sprintf("%q", tp.pattern), buf.Len(), lines)
		})
	}
}

// BenchmarkVsRegexp benchmarks generated matchers against regexp.MatchString.
func BenchmarkVsRegexp(b *testing.B) {
	for _, tp := range testPatterns {
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
		if noMatchInput == "" {
			noMatchInput = "ZZZZZ_no_match"
		}

		matchFn := tp.matchFn
		b.Run(tp.name+"/generated_match", func(b *testing.B) {
			for b.Loop() {
				matchFn(matchInput)
			}
		})
		b.Run(tp.name+"/regexp_match", func(b *testing.B) {
			for b.Loop() {
				re.MatchString(matchInput)
			}
		})
		b.Run(tp.name+"/generated_nomatch", func(b *testing.B) {
			for b.Loop() {
				matchFn(noMatchInput)
			}
		})
		b.Run(tp.name+"/regexp_nomatch", func(b *testing.B) {
			for b.Loop() {
				re.MatchString(noMatchInput)
			}
		})

		longInput := strings.Repeat(matchInput+"x", 100)
		b.Run(tp.name+"/generated_long", func(b *testing.B) {
			for b.Loop() {
				matchFn(longInput)
			}
		})
		b.Run(tp.name+"/regexp_long", func(b *testing.B) {
			for b.Loop() {
				re.MatchString(longInput)
			}
		})
	}
}
