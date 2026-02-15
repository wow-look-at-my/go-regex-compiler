package parser

import (
	"regexp/syntax"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"literal", "abc", false},
		{"character class", "[a-z]", false},
		{"dot", ".", false},
		{"star", "a*", false},
		{"plus", "a+", false},
		{"question", "a?", false},
		{"alternation", "a|b", false},
		{"grouping", "(ab)+", false},
		{"anchors", "^abc$", false},
		{"digit shorthand", `\d+`, false},
		{"word shorthand", `\w+`, false},
		{"space shorthand", `\s+`, false},
		{"counted repetition", "a{2,5}", false},
		{"case insensitive", "(?i)abc", false},
		{"complex", `[a-z]+@[a-z]+\.[a-z]{2,}`, false},
		{"empty", "", false},
		{"invalid unclosed bracket", "[abc", true},
		{"invalid bad escape", `\`, true},
		{"invalid bad repetition", "a{2,1}", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := Parse(tt.pattern)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, prog)
			assert.NotEmpty(t, prog.Inst)
		})
	}
}

func TestParseProducesValidProgram(t *testing.T) {
	prog, err := Parse("[a-z]+")
	require.NoError(t, err)

	assert.GreaterOrEqual(t, prog.Start, 0)
	assert.Less(t, prog.Start, len(prog.Inst))

	hasMatch := false
	for _, inst := range prog.Inst {
		if inst.Op == syntax.InstMatch {
			hasMatch = true
			break
		}
	}
	assert.True(t, hasMatch, "program should have an InstMatch instruction")
}
