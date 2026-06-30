package codegen

import (
	"bytes"
	"regexp/syntax"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
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

type preparedPattern struct {
	name    string
	pattern string
	dfa     *dfa.DFA
	prog    *syntax.Prog
}

func prepareAll(b *testing.B) []preparedPattern {
	b.Helper()
	out := make([]preparedPattern, len(benchPatterns))
	for i, bp := range benchPatterns {
		re, err := syntax.Parse(bp.pattern, syntax.Perl)
		require.Nil(b, err)

		numGroups := 0
		if re.Op == syntax.OpCapture {
			numGroups = re.Cap
		}
		_ = numGroups
		re = re.Simplify()
		prog, err := syntax.Compile(re)
		require.Nil(b, err)

		d, err := dfa.Build(prog)
		require.Nil(b, err)

		out[i] = preparedPattern{
			name:    bp.name,
			pattern: bp.pattern,
			dfa:     d,
			prog:    prog,
		}
	}
	return out
}

func BenchmarkGenerate(b *testing.B) {
	prepared := prepareAll(b)
	var buf bytes.Buffer
	for _, pp := range prepared {
		b.Run(pp.name, func(b *testing.B) {
			opts := Options{
				PackageName: "bench",
				FuncName:    "Match",
				Regex:       pp.pattern,
			}
			for b.Loop() {
				buf.Reset()
				_ = Generate(&buf, pp.dfa, opts)
			}
		})
	}
}

func BenchmarkGeneratePrefix(b *testing.B) {
	prepared := prepareAll(b)
	var buf bytes.Buffer
	for _, pp := range prepared {
		b.Run(pp.name, func(b *testing.B) {
			opts := Options{
				PackageName: "bench",
				FuncName:    "Match",
				Regex:       pp.pattern,
				Mode:        MatchPrefix,
			}
			for b.Loop() {
				buf.Reset()
				_ = Generate(&buf, pp.dfa, opts)
			}
		})
	}
}

func BenchmarkGenerateContains(b *testing.B) {
	prepared := prepareAll(b)
	var buf bytes.Buffer
	for _, pp := range prepared {
		b.Run(pp.name, func(b *testing.B) {
			opts := Options{
				PackageName: "bench",
				FuncName:    "Match",
				Regex:       pp.pattern,
				Mode:        MatchContains,
			}
			for b.Loop() {
				buf.Reset()
				_ = Generate(&buf, pp.dfa, opts)
			}
		})
	}
}

func BenchmarkGenerateSubmatch(b *testing.B) {
	submatchPatterns := []struct {
		name    string
		pattern string
	}{
		{"simple_groups", `([a-z]+)@([a-z]+)`},
		{"ssn", `(\d{3})-(\d{2})-(\d{4})`},
		{"alternation", `(foo|bar)baz`},
	}

	type prepared struct {
		name    string
		pattern string
		dfa     *dfa.DFA
		prog    *syntax.Prog
		groups  int
	}

	items := make([]prepared, len(submatchPatterns))
	for i, sp := range submatchPatterns {
		re, err := syntax.Parse(sp.pattern, syntax.Perl)
		require.Nil(b, err)

		groups := countGroupsBench(re)
		re = re.Simplify()
		prog, err := syntax.Compile(re)
		require.Nil(b, err)

		d, err := dfa.Build(prog)
		require.Nil(b, err)

		items[i] = prepared{
			name:    sp.name,
			pattern: sp.pattern,
			dfa:     d,
			prog:    prog,
			groups:  groups,
		}
	}

	var buf bytes.Buffer
	for _, pp := range items {
		b.Run(pp.name, func(b *testing.B) {
			opts := Options{
				PackageName: "bench",
				FuncName:    "Match",
				Regex:       pp.pattern,
				Submatch: &SubmatchOptions{
					FuncName:  "FindSubmatch",
					MatchFunc: "Match",
					Regex:     pp.pattern,
					Prog:      pp.prog,
					NumGroups: pp.groups,
				},
			}
			for b.Loop() {
				buf.Reset()
				_ = Generate(&buf, pp.dfa, opts)
			}
		})
	}
}

func countGroupsBench(re *syntax.Regexp) int {
	n := 0
	if re.Op == syntax.OpCapture {
		n = 1
	}
	for _, sub := range re.Sub {
		n += countGroupsBench(sub)
	}
	return n
}
