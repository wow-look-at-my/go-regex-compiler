package e2e

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
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
	// Invalid UTF-8: regexp decodes each bad byte as one U+FFFD rune (so "."
	// and ".+" match it). Generated matchers must agree byte-for-byte.
	"\xff", "a\xffb", "\xed\xa0\x80", "abc\x80", "\xc3", "caf\xc3",
	"\xef\xbf\xbd", // valid encoding of U+FFFD itself
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
				assert.Equal(t, expected, got)

			}
		})
	}
}

// TestCodeSize reports the generated code size (bytes and lines) for each pattern.
func TestCodeSize(t *testing.T) {
	for _, tp := range testPatterns {
		t.Run(tp.name, func(t *testing.T) {
			prog, err := parser.Parse(tp.pattern)
			require.Nil(t, err)

			d, err := dfa.Build(prog)
			require.Nil(t, err)

			var buf bytes.Buffer
			opts := codegen.Options{
				PackageName: "test",
				FuncName:    "Match",
				Regex:       tp.pattern,
			}
			require.NoError(t, codegen.Generate(&buf, d, opts))

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

// BenchmarkSubmatchVsRegexp compares generated submatch extraction against
// regexp.FindStringSubmatch and FindStringSubmatchIndex over representative
// patterns, including a realistic Apache access-log line.
func BenchmarkSubmatchVsRegexp(b *testing.B) {
	cases := []struct {
		name    string
		pattern string
		findFn  func(string) []string
		indexFn func(string) []int
		input   string
	}{
		{
			"rfc3339",
			`(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})T(?P<hour>\d{2}):(?P<min>\d{2}):(?P<sec>\d{2})Z`,
			FindRFC, FindRFCIndex, "2020-01-02T03:04:05Z",
		},
		{
			"apache",
			`(?P<ip>\d+\.\d+\.\d+\.\d+) - - \[(?P<ts>[^\]]+)\] "(?P<method>[A-Z]+) (?P<path>[^ ]+) (?P<proto>[^"]+)" (?P<status>\d{3}) (?P<size>\d+)`,
			FindApache, FindApacheIndex,
			`127.0.0.1 - - [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`,
		},
		{
			"logfmt",
			`(?P<key>\w+)="(?P<val>[^"]*)"`,
			FindLogfmt, FindLogfmtIndex, `name="quoted value"`,
		},
	}
	for _, c := range cases {
		re := anchoredRegexp(c.pattern)
		find := c.findFn
		idx := c.indexFn
		in := c.input
		b.Run(c.name+"/generated_submatch", func(b *testing.B) {
			for b.Loop() {
				find(in)
			}
		})
		b.Run(c.name+"/regexp_submatch", func(b *testing.B) {
			for b.Loop() {
				re.FindStringSubmatch(in)
			}
		})
		b.Run(c.name+"/generated_index", func(b *testing.B) {
			for b.Loop() {
				idx(in)
			}
		})
		b.Run(c.name+"/regexp_index", func(b *testing.B) {
			for b.Loop() {
				re.FindStringSubmatchIndex(in)
			}
		})
	}
}
