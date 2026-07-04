package codegen

import (
	"fmt"
	"regexp/syntax"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileProg(t *testing.T, pattern string) (*syntax.Prog, int) {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	require.NoError(t, err)
	n := countGroups(re)
	re = re.Simplify()
	prog, err := syntax.Compile(re)
	require.NoError(t, err)
	return prog, n
}

// onePassPatterns are patterns the compiled path must accept.
func TestBuildCapDFAOnepass(t *testing.T) {
	patterns := []struct {
		pattern string
		ascii   bool
	}{
		{`([a-z]+)@([a-z]+)`, true},
		{`(\d{3})-(\d{2})-(\d{4})`, true},
		{`(a+)(b+)`, true},
		{`(a)?b`, true},
		{`(a|b)c`, true},
		{`((a)(b))`, true},
		{`(ab)+`, true},
		{`(?:ab)(c)`, true},
		{`(foo|bar)baz`, true},
		{`([a-z]+)(\.[a-z]+)*`, true},
		{`(?P<a>x)?(?P<b>y)`, true},
		{`(a|ab)(c)`, true},       // disjoint after first token: 'b' vs 'c'
		{`(\babc)`, true},         // leading \b: resolved as a start gate
		{`(\w+)\b`, true},         // trailing \b: resolved as an accept gate
		{`^(\d+)-(\d+)$`, true},   // ^ and $ text anchors fold away for full match
		{`(a\Bb)`, true},          // interior \B (always true between word chars) folds to a no-op
		{`(foo\Bbar)`, true},      // interior \B inside a single group
		{`(foo\B)(bar)`, true},    // interior \B at a group boundary (left group)
		{`(foo)(\Bbar)`, true},    // interior \B at a group boundary (right group)
		{`([^"]*)"(\d+)"`, false}, // negated class -> non-ASCII -> rune path
	}
	for i, tt := range patterns {
		t.Run(fmt.Sprintf("ok%d", i), func(t *testing.T) {
			prog, n := compileProg(t, tt.pattern)
			d, ok := buildCapDFA(prog, n)
			require.True(t, ok, "expected one-pass for %q", tt.pattern)
			require.NotNil(t, d)
			assert.Equal(t, (n+1)*2, d.numSlots)
			assert.Equal(t, tt.ascii, d.ascii, "ascii mismatch for %q", tt.pattern)
			assert.NotEmpty(t, d.states)
			// At least one state must accept (the pattern is matchable).
			accepting := false
			for _, s := range d.states {
				if s.accept {
					accepting = true
				}
			}
			assert.True(t, accepting)
		})
	}
}

// TestBuildCapDFAFallback lists patterns the one-pass path must decline
// (ok=false) so the caller moves on to the TDFA register machine. Interior
// empty-width assertions are NOT in this list: dfa.ValidateAssertions vets those
// before codegen (an always-true \b/\B folds to a no-op, a conditional one like
// (a)\b(.) errors), so buildCapDFA trusts the surviving assertion instead of
// re-rejecting it here — the same way it trusts the match mode for text anchors.
func TestBuildCapDFAFallback(t *testing.T) {
	patterns := []string{
		`(a*)(a*)`,  // ambiguous captures: both groups match 'a'
		`(?i)(abc)`, // fold-case class (unsupported in compiled path)
	}
	for i, p := range patterns {
		t.Run(fmt.Sprintf("fb%d", i), func(t *testing.T) {
			prog, n := compileProg(t, p)
			_, ok := buildCapDFA(prog, n)
			assert.False(t, ok, "expected fallback for %q", p)
		})
	}
}

func TestBuildCapDFAAnyRune(t *testing.T) {
	// (.*) with (?s) uses opRuneAny; without it, opRuneAnyNotNL. Both must
	// compile via the any-rune edge (emitted as the switch default / != '\n').
	for i, p := range []string{`(a)(.*)`, `(?s)(a)(.*)`} {
		t.Run(fmt.Sprintf("any%d", i), func(t *testing.T) {
			prog, n := compileProg(t, p)
			d, ok := buildCapDFA(prog, n)
			require.True(t, ok, "expected one-pass for %q", p)
			assert.False(t, d.ascii, "dot patterns span the full rune range")
		})
	}
}

func TestInstRangesFoldCaseRejected(t *testing.T) {
	prog, _ := compileProg(t, `(?i)abc`)
	// Find the consuming instruction and confirm instRanges rejects fold-case.
	found := false
	for pc, inst := range prog.Inst {
		if inst.Op == syntax.InstRune || inst.Op == syntax.InstRune1 {
			if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
				_, _, _, ok := instRanges(prog, pc)
				assert.False(t, ok, "fold-case class must be rejected")
				found = true
			}
		}
	}
	assert.True(t, found, "expected a fold-case consuming instruction")
}

func TestEdgesDisjoint(t *testing.T) {
	assert.True(t, edgesDisjoint([]capEdge{
		{ranges: []capRange{{'a', 'z'}}},
		{ranges: []capRange{{'0', '9'}}},
	}))
	assert.False(t, edgesDisjoint([]capEdge{
		{ranges: []capRange{{'a', 'z'}}},
		{ranges: []capRange{{'m', 'p'}}},
	}))
	// An any-rune edge overlaps everything.
	assert.False(t, edgesDisjoint([]capEdge{
		{ranges: []capRange{{'a', 'z'}}},
		{anyRune: true},
	}))
	// A single edge with multiple ranges is fine.
	assert.True(t, edgesDisjoint([]capEdge{
		{ranges: []capRange{{'a', 'z'}, {'0', '9'}}},
	}))
}

func TestOnepassEmitHelpers(t *testing.T) {
	// Byte-mode conditions.
	c, used := rangeCond('a', 'z', true)
	assert.Equal(t, "match.InRange(c, 'a', 'z')", c)
	assert.True(t, used)
	c, used = rangeCond('@', '@', true)
	assert.Equal(t, "c == '@'", c)
	assert.False(t, used)
	// Rune-mode conditions.
	c, _ = rangeCond('a', 'z', false)
	assert.Equal(t, "match.InRange(r, 'a', 'z')", c)

	// Edge body writes the current offset i and skips group-0 slots.
	assert.Equal(t, "caps[2] = i; state = 5", onepassEdgeBody([]int{0, 2}, 5))
	assert.Equal(t, "state = 5", onepassEdgeBody([]int{0, 1}, 5))
	// Accept body writes len(input) and skips group-0 slots.
	assert.Equal(t, "caps[5] = len(input)", onepassAcceptBody([]int{1, 5}))
	assert.Equal(t, "", onepassAcceptBody([]int{0, 1}))
}

// TestGenerateOnepassGates verifies the compiled path handles empty-width
// boundary assertions: a leading \b becomes a start gate, a trailing \b an
// accept gate (both emitting a word-char helper), and ^/$ text anchors fold away
// entirely for full-match. All stay COMPILED (no interpreter).
func TestGenerateOnepassGates(t *testing.T) {
	t.Run("leading_wordboundary", func(t *testing.T) {
		out := generateNamedSubmatch(t, `(\babc)`, false)
		assertValidGo(t, out)
		assert.NotContains(t, out, "addThread")
		assert.Contains(t, out, "func findSubIndexWord(r rune) bool")
		assert.Contains(t, out, "firstRune")
		assert.Contains(t, out, "!findSubIndexWord(firstRune)")
	})
	t.Run("trailing_wordboundary", func(t *testing.T) {
		out := generateNamedSubmatch(t, `(\w+)\b`, false)
		assertValidGo(t, out)
		assert.NotContains(t, out, "addThread")
		assert.Contains(t, out, "lastRune")
		assert.Contains(t, out, "!findSubIndexWord(lastRune)")
	})
	t.Run("text_anchors_fold", func(t *testing.T) {
		// ^ and $ are always satisfied at the ends of a full match, so no gate
		// code (no word helper, no firstRune/lastRune) is emitted.
		out := generateNamedSubmatch(t, `^(\d+)-(\d+)$`, false)
		assertValidGo(t, out)
		assert.NotContains(t, out, "addThread")
		assert.Contains(t, out, "switch state {")
		assert.NotContains(t, out, "Word(")
		assert.NotContains(t, out, "firstRune")
	})
}

func TestDedupInts(t *testing.T) {
	assert.Equal(t, []int{1, 2, 3}, dedupInts([]int{1, 2, 2, 3, 3}))
	assert.Equal(t, []int{1}, dedupInts([]int{1}))
	assert.Empty(t, dedupInts(nil))
}

// TestGenerateSubmatchTDFA verifies an ambiguous-capture pattern — which the
// one-pass path rejects — compiles to the tagged-DFA register machine, NOT an
// interpreter: no program table, no live-position list, no sync.Pool.
func TestGenerateSubmatchTDFA(t *testing.T) {
	out := generateNamedSubmatch(t, `(a*)(a*)`, false)
	assertValidGo(t, out)

	assert.Contains(t, out, "func FindSubIndex(input string) []int")
	// Register machine markers.
	assert.Contains(t, out, "var reg [")
	assert.Contains(t, out, "switch state {")
	// NO interpreter machinery.
	assert.NotContains(t, out, "addThread")
	assert.NotContains(t, out, "findSubIndexProg")
	assert.NotContains(t, out, "sync.Pool")
	assert.NotContains(t, out, "EmptyOpsAt")
}

// TestInteriorNegWordBoundaryFolds verifies that an always-true interior \B
// (proven safe by dfa.ValidateAssertions) is folded to a no-op by BOTH compiled
// paths, so patterns like (\w+\B\w+) — ambiguous, hence TDFA — and (a\Bb) — a
// literal sequence, hence one-pass — compile to a state machine with NO
// interpreter. These are the exact patterns the interpreter used to serve.
func TestInteriorNegWordBoundaryFolds(t *testing.T) {
	// One-pass: (a\Bb) folds to (ab).
	for _, p := range []string{`(a\Bb)`, `(foo\B)(bar)`} {
		prog, n := compileProg(t, p)
		d, ok := buildCapDFA(prog, n)
		require.True(t, ok, "expected one-pass for %q after folding interior \\B", p)
		require.NotNil(t, d)
	}
	// Ambiguous (adjacent \w+): the one-pass path declines, the TDFA path
	// compiles it after skipping the always-true \B in the closure.
	for _, p := range []string{`(\w+\B\w+)`, `(\w+)\B(\w+)`} {
		prog, n := compileProg(t, p)
		_, okOne := buildCapDFA(prog, n)
		assert.False(t, okOne, "%q is ambiguous; one-pass must decline", p)
		td, okTD := buildTDFA(prog, n)
		require.True(t, okTD, "expected TDFA to compile %q after folding interior \\B", p)
		require.NotEmpty(t, td.states)
	}
	// End to end: no interpreter machinery reaches the emitted code.
	for _, p := range []string{`(a\Bb)`, `(\w+\B\w+)`, `(\w+)\B(\w+)`} {
		out := generateNamedSubmatch(t, p, false)
		assertValidGo(t, out)
		assert.Contains(t, out, "switch state {")
		assert.NotContains(t, out, "addThread")
		assert.NotContains(t, out, "sync.Pool")
		assert.NotContains(t, out, "EmptyOpsAt")
	}
}

// TestBuildSubmatchContextTDFA verifies a pattern the one-pass path rejects
// compiles via the TDFA register machine (not the interpreter): TDFA is set,
// Onepass is not, and no Thompson instruction table is built.
func TestBuildSubmatchContextTDFA(t *testing.T) {
	prog, n := compileProg(t, `(a*)(a*)`)
	ctx, err := buildSubmatchContext(SubmatchOptions{
		FuncName:  "FindSub",
		MatchFunc: "Match",
		Regex:     `(a*)(a*)`,
		Prog:      prog,
		NumGroups: n,
	})
	require.NoError(t, err)
	assert.False(t, ctx.Onepass, "ambiguous pattern is not one-pass")
	assert.True(t, ctx.TDFA, "ambiguous pattern must compile via TDFA")
	assert.Empty(t, ctx.Instructions, "compiled path must not build an instruction table")
	assert.NotEmpty(t, ctx.TDStates, "TDFA path must have automaton states")
}

// TestBuildSubmatchContextInterpreterOracle verifies the interpreter remains
// reachable ONLY via the explicit ForceInterpreter oracle flag (used for
// differential testing), never on the normal generation path.
func TestBuildSubmatchContextInterpreterOracle(t *testing.T) {
	prog, n := compileProg(t, `(a*)(a*)`)
	ctx, err := buildSubmatchContext(SubmatchOptions{
		FuncName:         "FindSub",
		MatchFunc:        "Match",
		Regex:            `(a*)(a*)`,
		Prog:             prog,
		NumGroups:        n,
		ForceInterpreter: true,
	})
	require.NoError(t, err)
	assert.False(t, ctx.Onepass)
	assert.False(t, ctx.TDFA)
	assert.Equal(t, len(prog.Inst), len(ctx.Instructions))
}
