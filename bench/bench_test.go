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
