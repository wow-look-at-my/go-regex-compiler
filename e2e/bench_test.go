package e2e

import (
	"regexp"
	"strings"
	"testing"
)

var benchPatterns = []struct {
	name       string
	regex      string
	matchFn    func(string) bool
	matchInput string
	noMatch    string
}{
	{"char_class", `[a-z]+`, MatchCharClass, "hello", "12345"},
	{"ssn", `\d{3}-\d{2}-\d{4}`, MatchSSN, "123-45-6789", "abc-de-fghi"},
	{"identifier", `[A-Za-z_][A-Za-z0-9_]*`, MatchIdentifier, "camelCase123", "-dash"},
	{"url", `(https?://)?[a-z]+\.[a-z]{2,}`, MatchURL, "https://example.com", "12345"},
	{"case_insensitive", `(?i)hello`, MatchCaseInsensitive, "HeLLo", "world"},
}

func BenchmarkGenerated(b *testing.B) {
	for _, p := range benchPatterns {
		long := strings.Repeat(p.matchInput+"x", 100)
		b.Run(p.name+"/match", func(b *testing.B) {
			for b.Loop() {
				p.matchFn(p.matchInput)
			}
		})
		b.Run(p.name+"/no_match", func(b *testing.B) {
			for b.Loop() {
				p.matchFn(p.noMatch)
			}
		})
		b.Run(p.name+"/long", func(b *testing.B) {
			for b.Loop() {
				p.matchFn(long)
			}
		})
	}
}

func BenchmarkRegexp(b *testing.B) {
	for _, p := range benchPatterns {
		re := regexp.MustCompile("^(?:" + p.regex + ")$")
		long := strings.Repeat(p.matchInput+"x", 100)
		b.Run(p.name+"/match", func(b *testing.B) {
			for b.Loop() {
				re.MatchString(p.matchInput)
			}
		})
		b.Run(p.name+"/no_match", func(b *testing.B) {
			for b.Loop() {
				re.MatchString(p.noMatch)
			}
		})
		b.Run(p.name+"/long", func(b *testing.B) {
			for b.Loop() {
				re.MatchString(long)
			}
		})
	}
}
