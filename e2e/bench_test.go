//go:generate go run ../cmd/go-regex-compiler --regex"[a-z]+" --funcMatchCharClass --packagee2e --outputgen_charclass.go
//go:generate go run ../cmd/go-regex-compiler --regex"\\d{3}-\\d{2}-\\d{4}" --funcMatchSSN --packagee2e --outputgen_ssn.go
//go:generate go run ../cmd/go-regex-compiler --regex"[A-Za-z_][A-Za-z0-9_]*" --funcMatchIdentifier --packagee2e --outputgen_identifier.go
//go:generate go run ../cmd/go-regex-compiler --regex"(https?://)?[a-z]+\\.[a-z]{2,}" --funcMatchURL --packagee2e --outputgen_url.go
//go:generate go run ../cmd/go-regex-compiler --regex"(?i)hello" --funcMatchCaseInsensitive --packagee2e --outputgen_casei.go

package e2e_test

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	. "github.com/wow-look-at-my/go-regex-compiler/e2e"
	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
	"github.com/wow-look-at-my/testify/require"
)

func BenchmarkPipeline(b *testing.B) {
	for _, tp := range testPatterns {
		b.Run(tp.name, func(b *testing.B) {
			var buf bytes.Buffer
			opts := codegen.Options{
				PackageName:	"bench",
				FuncName:	"Match",
				Regex:		tp.pattern,
			}
			for b.Loop() {
				buf.Reset()
				prog, err := parser.Parse(tp.pattern)
				require.Nil(b, err)

				d, err := dfa.Build(prog)
				require.Nil(b, err)

				_ = codegen.Generate(&buf, d, opts)
			}
		})
	}
}

var generatedBenchPatterns = []struct {
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
	for _, p := range generatedBenchPatterns {
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
	for _, p := range generatedBenchPatterns {
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
