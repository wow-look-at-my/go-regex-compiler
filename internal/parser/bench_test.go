package parser

import "testing"

var benchPatterns = []struct {
	name    string
	pattern string
}{
	{"literal", "abc"},
	{"char_class", "[a-z]+"},
	{"alternation", "a|b|c"},
	{"quantifiers", "a*b+c?"},
	{"ssn", `\d{3}-\d{2}-\d{4}`},
	{"identifier", `[A-Za-z_][A-Za-z0-9_]*`},
	{"email_like", `[a-z]+@[a-z]+\.[a-z]{2,}`},
	{"url_like", `(https?://)?[a-z]+\.[a-z]{2,}`},
	{"case_insensitive", "(?i)hello"},
	{"complex_alt", "(a|b)*abb"},
	{"hex_color", `#[0-9a-f]{6}`},
	{"nested_quant", "(ab?c)+"},
}

func BenchmarkParse(b *testing.B) {
	for _, bp := range benchPatterns {
		b.Run(bp.name, func(b *testing.B) {
			for b.Loop() {
				_, _ = Parse(bp.pattern)
			}
		})
	}
}

func BenchmarkParseResult(b *testing.B) {
	for _, bp := range benchPatterns {
		b.Run(bp.name, func(b *testing.B) {
			for b.Loop() {
				_, _ = ParseResult(bp.pattern)
			}
		})
	}
}
