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

// fallbackPatterns are patterns the compiled path must reject (ok=false), so the
// caller uses the Thompson interpreter.
func TestBuildCapDFAFallback(t *testing.T) {
	patterns := []string{
		`(\babc)`,   // empty-width assertion (\b) at start
		`(\w+)\b`,   // empty-width assertion (\b) at end
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

func TestDedupInts(t *testing.T) {
	assert.Equal(t, []int{1, 2, 3}, dedupInts([]int{1, 2, 2, 3, 3}))
	assert.Equal(t, []int{1}, dedupInts([]int{1}))
	assert.Empty(t, dedupInts(nil))
}

// TestGenerateSubmatchFallback verifies a pattern with an empty-width assertion
// (\b) — which the compiled path does not handle — falls back to the correct
// Thompson interpreter, emitting its program table and empty-width machinery.
func TestGenerateSubmatchFallback(t *testing.T) {
	out := generateNamedSubmatch(t, `(\w+)\b`, false)
	assertValidGo(t, out)

	assert.Contains(t, out, "func FindSubIndex(input string) []int")
	assert.Contains(t, out, "addThread")
	assert.Contains(t, out, "var findSubIndexProg = []findSubIndexInst{")
	assert.Contains(t, out, "EmptyOpsAt")
	assert.Contains(t, out, "EmptyWordBoundary")
	assert.Contains(t, out, "sync.Pool")
}

// TestBuildSubmatchContextFallback verifies the interpreter path still populates
// the instruction table for a pattern the compiled path rejects (empty-width).
func TestBuildSubmatchContextFallback(t *testing.T) {
	prog, n := compileProg(t, `(\w+)\b`)
	ctx := buildSubmatchContext(SubmatchOptions{
		FuncName:  "FindSub",
		MatchFunc: "Match",
		Regex:     `(\w+)\b`,
		Prog:      prog,
		NumGroups: n,
	})
	assert.False(t, ctx.Onepass, "empty-width pattern must fall back")
	assert.Equal(t, len(prog.Inst), len(ctx.Instructions))
}
