package e2e

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
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
				require.Nil(b, err)

				d, err := dfa.Build(prog)
				require.Nil(b, err)

				_ = codegen.Generate(&buf, d, opts)
			}
		})
	}
}
