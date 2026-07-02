package bench

import (
	"regexp"
	"strings"
	"testing"
)

type benchCase struct {
	name      string
	pattern   string
	generated func(string) bool
	match     string
	noMatch   string
}

var cases = []benchCase{
	{"literal", `abc`, MatchLiteral, "abc", "xyz"},
	{"char_class", `[a-z]+`, MatchCharClass, "hello", "12345"},
	{"ssn", `\d{3}-\d{2}-\d{4}`, MatchSSN, "123-45-6789", "abc-de-fghi"},
	{"email", `[a-z]+@[a-z]+\.[a-z]{2,}`, MatchEmail, "user@example.com", "not-an-email"},
	{"identifier", `[A-Za-z_][A-Za-z0-9_]*`, MatchIdentifier, "foo_bar123", "123abc"},
	{"url", `(https?://)?[a-z]+\.[a-z]{2,}`, MatchURL, "https://example.com", "ftp://x"},
	{"case_insensitive", `(?i)hello`, MatchCaseInsensitive, "HeLLo", "world"},
	{"hex_color", `#[0-9a-f]{6}`, MatchHexColor, "#aabbcc", "#gghhii"},
}

func anchoredRe(pattern string) *regexp.Regexp {
	return regexp.MustCompile("^(?:" + pattern + ")$")
}

func BenchmarkGenerated(b *testing.B) {
	for _, bc := range cases {
		compiled := anchoredRe(bc.pattern)

		b.Run(bc.name+"/match", func(b *testing.B) {
			for b.Loop() {
				bc.generated(bc.match)
			}
		})
		b.Run(bc.name+"/no_match", func(b *testing.B) {
			for b.Loop() {
				bc.generated(bc.noMatch)
			}
		})

		long := strings.Repeat(bc.match, 100)
		b.Run(bc.name+"/long", func(b *testing.B) {
			for b.Loop() {
				bc.generated(long)
			}
		})

		_ = compiled
	}
}

func BenchmarkCompiledRegexp(b *testing.B) {
	for _, bc := range cases {
		compiled := anchoredRe(bc.pattern)

		b.Run(bc.name+"/match", func(b *testing.B) {
			for b.Loop() {
				compiled.MatchString(bc.match)
			}
		})
		b.Run(bc.name+"/no_match", func(b *testing.B) {
			for b.Loop() {
				compiled.MatchString(bc.noMatch)
			}
		})

		long := strings.Repeat(bc.match, 100)
		b.Run(bc.name+"/long", func(b *testing.B) {
			for b.Loop() {
				compiled.MatchString(long)
			}
		})
	}
}

// containsCases benchmark unanchored (contains-mode) matchers against
// unanchored regexp. Haystacks are large so the scan cost dominates.
var containsCases = []struct {
	name      string
	pattern   string
	generated func(string) bool
	haystack  string
}{
	{"contains_literal_hit", "error", ContainsError,
		strings.Repeat("all quiet on this line ", 400) + "error"},
	{"contains_literal_miss", "error", ContainsError,
		strings.Repeat("all quiet on this line ", 400)},
	{"contains_ssn_miss", `\d{3}-\d{2}-\d{4}`, ContainsSSN,
		strings.Repeat("phone 555-01x1 is not an ssn ", 300)},
	// Worst case of the old restart-at-every-position loop: every start
	// position scanned to the end of the input (O(n^2)).
	{"contains_astarb_miss", "a*b", ContainsAStarB, strings.Repeat("a", 10000)},
}

func BenchmarkContains(b *testing.B) {
	for _, bc := range containsCases {
		re := regexp.MustCompile(bc.pattern)
		if re.MatchString(bc.haystack) != bc.generated(bc.haystack) {
			b.Fatalf("%s: generated and regexp disagree", bc.name)
		}
		b.Run(bc.name+"/generated", func(b *testing.B) {
			for b.Loop() {
				bc.generated(bc.haystack)
			}
		})
		b.Run(bc.name+"/regexp", func(b *testing.B) {
			for b.Loop() {
				re.MatchString(bc.haystack)
			}
		})
	}
}

func BenchmarkUncompiledRegexp(b *testing.B) {
	for _, bc := range cases {
		anchored := "^(?:" + bc.pattern + ")$"

		b.Run(bc.name+"/match", func(b *testing.B) {
			for b.Loop() {
				regexp.MatchString(anchored, bc.match)
			}
		})
		b.Run(bc.name+"/no_match", func(b *testing.B) {
			for b.Loop() {
				regexp.MatchString(anchored, bc.noMatch)
			}
		})

		long := strings.Repeat(bc.match, 100)
		b.Run(bc.name+"/long", func(b *testing.B) {
			for b.Loop() {
				regexp.MatchString(anchored, long)
			}
		})
	}
}
