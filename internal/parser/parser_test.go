package parser

import (
	"regexp"
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

func TestGroupNamesParity(t *testing.T) {
	patterns := []string{
		`(?P<year>\d{4})-(?P<month>\d{2})`,         // all named
		`(\d{4})-(\d{2})`,                          // all unnamed
		`(?P<a>x)(y)(?P<b>z)`,                      // mixed
		`((?P<inner>a)(b))`,                        // nested, mixed
		`(?:foo)(bar)`,                             // non-capturing then capturing
		`(?P<ip>\d+\.\d+\.\d+\.\d+):(?P<port>\d+)`, // realistic
		`abc`,             // no groups at all
		`(a)(b)(c)(d)(e)`, // many unnamed
	}
	for _, pat := range patterns {
		t.Run(pat, func(t *testing.T) {
			res, err := ParseResult(pat)
			require.NoError(t, err)
			want := regexp.MustCompile(pat).SubexpNames()
			assert.Equal(t, want, res.GroupNames, "GroupNames must mirror regexp.SubexpNames for %q", pat)
			// Length invariant: NumGroups+.
			assert.Equal(t, res.NumGroups+1, len(res.GroupNames))
			assert.Equal(t, "", res.GroupNames[0], "index 0 (whole match) must be empty")
		})
	}
}

func TestGroupNamesRejectsEmptyName(t *testing.T) {
	// Go's syntax.Parse rejects an empty explicit group name; the parse error
	_, err := ParseResult(`(?P<>a)`)
	assert.Error(t, err, "expected parse error for empty group name")
}

func TestGroupNamesDuplicateMatchesStdlib(t *testing.T) {
	// NOTE: Go's regexp/syntax.Parse (and regexp.Compile) does NOT reject
	pat := `(?P<x>a)(?P<x>b)`
	res, err := ParseResult(pat)
	require.NoError(t, err)
	want := regexp.MustCompile(pat).SubexpNames()
	assert.Equal(t, want, res.GroupNames)
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
