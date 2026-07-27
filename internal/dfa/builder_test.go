package dfa

import (
	"regexp/syntax"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parse(t *testing.T, pattern string) *syntax.Prog {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	require.NoError(t, err)
	re = re.Simplify()
	prog, err := syntax.Compile(re)
	require.NoError(t, err)
	return prog
}

func countAccepting(d *DFA) int {
	n := 0
	for _, s := range d.States {
		if s.Accept {
			n++
		}
	}
	return n
}

func hasAcceptingState(d *DFA) bool {
	return countAccepting(d) > 0
}

func TestBuildSimpleLiteral(t *testing.T) {
	prog := parse(t, "abc")
	d, err := Build(prog)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(d.States), 4, "should have states for: start, after 'a', after 'b', after 'c'")
	assert.Equal(t, 1, countAccepting(d), "exactly one accepting state")
}

func TestBuildCharacterClass(t *testing.T) {
	prog := parse(t, "[a-z]")
	d, err := Build(prog)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(d.States), 2, "at least start and accept states")
	assert.NotEmpty(t, d.States[d.Start].Transitions, "start state should have transitions")
}

func TestBuildAlternation(t *testing.T) {
	prog := parse(t, "a|b")
	d, err := Build(prog)
	require.NoError(t, err)

	// May be merged into a single range [a,b] by alphabet partitioning
	assert.NotEmpty(t, d.States[d.Start].Transitions, "start state should have transitions for 'a|b'")
	assert.True(t, hasAcceptingState(d), "should have an accepting state")
}

func TestBuildStar(t *testing.T) {
	prog := parse(t, "a*")
	d, err := Build(prog)
	require.NoError(t, err)

	assert.True(t, d.States[d.Start].Accept, "start state should be accepting for 'a*'")
}

func TestBuildPlus(t *testing.T) {
	prog := parse(t, "a+")
	d, err := Build(prog)
	require.NoError(t, err)

	assert.False(t, d.States[d.Start].Accept, "start state should not be accepting for 'a+'")
	assert.True(t, hasAcceptingState(d), "should have an accepting state")
}

func TestBuildEmptyRegex(t *testing.T) {
	prog := parse(t, "")
	d, err := Build(prog)
	require.NoError(t, err)

	assert.True(t, d.States[d.Start].Accept, "start state should be accepting for empty regex")
}

func TestBuildDot(t *testing.T) {
	prog := parse(t, ".")
	d, err := Build(prog)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(d.States), 2, "at least start and accept states")
	assert.NotEmpty(t, d.States[d.Start].Transitions, "start state should have transitions for '.'")
}

func TestBuildComplex(t *testing.T) {
	patterns := []string{
		`[a-z]+@[a-z]+\.[a-z]{2,}`,
		`(foo|bar)baz`,
		`\d{3}-\d{2}-\d{4}`,
		`[A-Za-z_][A-Za-z0-9_]*`,
		`(a|b)*c`,
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			prog := parse(t, pattern)
			d, err := Build(prog)
			require.NoError(t, err)
			assert.NotEmpty(t, d.States, "DFA should have states")
		})
	}
}

func TestBuildCaseInsensitive(t *testing.T) {
	prog := parse(t, "(?i)abc")
	d, err := Build(prog)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(d.States), 4)
	assert.Equal(t, 1, countAccepting(d), "exactly one accepting state")
}

func TestExpandFoldCaseNoCap(t *testing.T) {
	// U+212A (KELVIN SIGN) sits at offset 0x12A (> 256) from U+2000; its fold
	// orbit {k, K} must not be dropped by any per-range expansion cap.
	got := ExpandFoldCase([]rune{0x2000, 0x2200})
	assert.True(t, rangesContain(got, 'k'), "fold expansion must include 'k' (fold of U+212A)")
	assert.True(t, rangesContain(got, 'K'), "fold expansion must include 'K' (fold of U+212A)")
	assert.True(t, rangesContain(got, 0x2100), "original range must be preserved")

	// A range entirely outside the foldable band expands to itself.
	assert.Equal(t, []rune{0x30000, 0x30010}, ExpandFoldCase([]rune{0x30000, 0x30010}))
}

func rangesContain(pairs []rune, r rune) bool {
	for i := 0; i+1 < len(pairs); i += 2 {
		if r >= pairs[i] && r <= pairs[i+1] {
			return true
		}
	}
	return false
}

func TestMergeRuneRanges(t *testing.T) {
	tests := []struct {
		name   string
		input  []rune
		expect []rune
	}{
		{"no overlap", []rune{'a', 'c', 'e', 'g'}, []rune{'a', 'c', 'e', 'g'}},
		{"overlap", []rune{'a', 'd', 'c', 'f'}, []rune{'a', 'f'}},
		{"adjacent", []rune{'a', 'c', 'd', 'f'}, []rune{'a', 'f'}},
		{"single", []rune{'a', 'z'}, []rune{'a', 'z'}},
		{"contained", []rune{'a', 'z', 'c', 'f'}, []rune{'a', 'z'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeRuneRanges(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

// simulateSearch runs a search DFA (BuildSearch) over input the way the
// generated contains-mode code does: transition on each rune, restart at the
// start state when no transition matches, and report true as soon as an
// accepting state is entered.
func simulateSearch(d *DFA, input string) bool {
	if d.States[d.Start].Accept {
		return true
	}
	state := d.Start
	for _, r := range input {
		next := d.Start
		for _, tr := range d.States[state].Transitions {
			if tr.Lo <= r && r <= tr.Hi {
				next = tr.Next
				break
			}
		}
		if d.States[next].Accept {
			return true
		}
		state = next
	}
	return false
}

func TestBuildSearchSeedsStartEverywhere(t *testing.T) {
	prog := parse(t, "aab")
	d, err := BuildSearch(prog)
	require.NoError(t, err)

	// The classic overlap case: the match at offset 1 starts inside the
	// failed attempt at offset 0. A restart-on-failure scanner misses it;
	// the search DFA must not.
	assert.True(t, simulateSearch(d, "aaab"))
	assert.True(t, simulateSearch(d, "aab"))
	assert.True(t, simulateSearch(d, "xxaabyy"))
	assert.False(t, simulateSearch(d, "aa"))
	assert.False(t, simulateSearch(d, "ab"))
	assert.False(t, simulateSearch(d, ""))
}

func TestBuildSearchUnanchoredScan(t *testing.T) {
	prog := parse(t, "a*b")
	d, err := BuildSearch(prog)
	require.NoError(t, err)

	assert.True(t, simulateSearch(d, "b"))
	assert.True(t, simulateSearch(d, "zzzaaabzzz"))
	assert.False(t, simulateSearch(d, "aaaa"))
	assert.False(t, simulateSearch(d, "zzz"))
}

func TestBuildSearchEmptyPattern(t *testing.T) {
	prog := parse(t, "")
	d, err := BuildSearch(prog)
	require.NoError(t, err)
	assert.True(t, d.States[d.Start].Accept, "empty pattern matches everywhere")
}

func TestBuildSearchOmitsRestartTransitions(t *testing.T) {
	prog := parse(t, "abc")
	d, err := BuildSearch(prog)
	require.NoError(t, err)

	for _, s := range d.States {
		for _, tr := range s.Transitions {
			assert.NotEqual(t, d.Start, tr.Next,
				"transitions back to the start state must be omitted (state %d)", s.ID)
		}
	}
}
