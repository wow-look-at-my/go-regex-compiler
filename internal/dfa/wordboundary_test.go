package dfa

import (
	"regexp"
	"regexp/syntax"
	"testing"

	"github.com/stretchr/testify/require"
)

// simulate runs a built DFA over input and reports the verdict for the mode.
// prefix and contains read Accept on entry; every mode reads AcceptAtEnd on the
// state the input lands in.
func simulate(d *DFA, input string, mode string) bool {
	state := d.States[d.Start]
	if mode != "full" && state.Accept {
		return true
	}
	for _, r := range input {
		var next *State
		for _, tr := range state.Transitions {
			if tr.Lo <= r && r <= tr.Hi {
				next = d.States[tr.Next]
				break
			}
		}
		if next == nil {
			if mode == "contains" {
				state = d.States[d.Start]
				continue
			}
			return false
		}
		state = next
		if mode != "full" && state.Accept {
			return true
		}
	}
	// Every mode also asks the final state: a trailing \b can be satisfied by
	// the end of the input, which leaves no rune to enter an accepting state on.
	return state.AcceptAtEnd
}

func buildFor(t *testing.T, pattern, mode string) *DFA {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	require.NoError(t, err)
	prog, err := syntax.Compile(re.Simplify())
	require.NoError(t, err)

	anchorStart, anchorEnd := true, true
	switch mode {
	case "prefix":
		anchorEnd = false
	case "contains":
		anchorStart, anchorEnd = false, false
	}
	require.NoError(t, ValidateAssertionsForBoolMatcher(prog, anchorStart, anchorEnd),
		"the bool-matcher validator must accept a word boundary in %s mode", mode)

	build := Build
	if mode == "contains" {
		build = BuildSearch
	}
	d, err := build(prog)
	require.NoError(t, err)
	return d
}

// stdlib is the oracle. A word boundary is exactly the construct the DFA used
// to refuse, so the only proof worth having is agreement with the engine that
// already gets it right.
func TestWordBoundaryAgreesWithStdlib(t *testing.T) {
	patterns := []string{
		`\bused to\b`,
		`\berror`,
		`error\b`,
		`\bcat\b`,
		`foo\bbar`,
		`foo\Bbar`,
		`\Bing\b`,
		`\b\w+\b`,
		`a\b.`,
		`\b[0-9]+\b`,
		`(?i)\bTODO\b`,
		`\bab?\b`,
		`x\B`,
		`\B`,
		`\b`,
	}
	inputs := []string{
		"", " ", "a", "ab", "used to", "we used to run", "usedto", "xused to",
		"used tox", "error", "an error here", "errors", "reerror", "cat",
		"cats", "the cat sat", "concat", "foobar", "foo bar", "fooBbar",
		"singing", "sing", "ing", "42", "x42y", "a-b", "_a_", "TODO", "todo x",
		"  spaced  ", "a\tb", "é", "aéb", "ab?", "x", "xx", "1a2b3",
	}

	for _, mode := range []string{"full", "prefix", "contains"} {
		for _, pattern := range patterns {
			d := buildFor(t, pattern, mode)
			oracle := regexp.MustCompile(oracleFor(pattern, mode))
			for _, in := range inputs {
				require.Equal(t, oracle.MatchString(in), simulate(d, in, mode),
					"mode=%s pattern=%q input=%q", mode, pattern, in)
			}
		}
	}
}

// oracleFor expresses the mode as an anchoring stdlib understands. prefix has
// no direct spelling, so it becomes "some prefix matches", which is what the
// generated prefix matcher reports.
func oracleFor(pattern, mode string) string {
	switch mode {
	case "full":
		return `\A(?:` + pattern + `)\z`
	case "prefix":
		return `\A(?:` + pattern + `)`
	default:
		return pattern
	}
}

// The plain construction must be untouched by any of this, or every existing
// pattern silently changes shape.
func TestPatternsWithoutAWordBoundaryKeepTheOriginalConstruction(t *testing.T) {
	for _, pattern := range []string{`abc`, `a+b`, `[0-9]{2,4}`, `foo|bar`, `(?i)k`} {
		re, err := syntax.Parse(pattern, syntax.Perl)
		require.NoError(t, err)
		prog, err := syntax.Compile(re.Simplify())
		require.NoError(t, err)
		require.False(t, hasWordBoundary(prog), "%q carries no boundary", pattern)

		d, err := Build(prog)
		require.NoError(t, err)
		for _, s := range d.States {
			require.Equal(t, s.Accept, s.AcceptAtEnd,
				"without a boundary the two acceptance answers must not diverge")
		}
	}
}

// A boundary in contains mode is the case that used to be refused outright, and
// the restart default is what makes it delicate: a restart carries no memory of
// the character that caused it, so the alphabet has to cover every rune.
func TestContainsModeCoversEveryRune(t *testing.T) {
	d := buildFor(t, `\bcat\b`, "contains")
	for _, s := range d.States {
		var covered rune
		for _, tr := range s.Transitions {
			require.LessOrEqual(t, tr.Lo, covered+1, "gap before %q in state %d", tr.Lo, s.ID)
			if tr.Hi > covered {
				covered = tr.Hi
			}
		}
		require.Equal(t, rune(0x10FFFF), covered, "state %d stops covering at %q", s.ID, covered)
	}
}
