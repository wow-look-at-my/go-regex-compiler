package dfa

import (
	"github.com/stretchr/testify/require"
	"regexp/syntax"
	"testing"
)

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

func compileProg(b *testing.B, pattern string) *syntax.Prog {
	b.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	require.Nil(b, err)

	re = re.Simplify()
	prog, err := syntax.Compile(re)
	require.Nil(b, err)

	return prog
}

func BenchmarkBuild(b *testing.B) {
	for _, bp := range benchPatterns {
		prog := compileProg(b, bp.pattern)
		b.Run(bp.name, func(b *testing.B) {
			for b.Loop() {
				_, _ = Build(prog)
			}
		})
	}
}
