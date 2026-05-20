package e2e_test

import (
	"bytes"
	"testing"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
)

func BenchmarkPipeline(b *testing.B) {
	for _, tp := range testPatterns {
		b.Run(tp.name, func(b *testing.B) {
			var buf bytes.Buffer
			opts := codegen.Options{
				PackageName: "bench",
				FuncName:    "Match",
				Regex:       tp.pattern,
			}
			for b.Loop() {
				buf.Reset()
				prog, err := parser.Parse(tp.pattern)
				if err != nil {
					b.Fatal(err)
				}
				d, err := dfa.Build(prog)
				if err != nil {
					b.Fatal(err)
				}
				_ = codegen.Generate(&buf, d, opts)
			}
		})
	}
}
