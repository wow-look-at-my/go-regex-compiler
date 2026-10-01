package e2e

import (
	"math/rand"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// fuzzCase pairs a generated submatch-index matcher with its source pattern and
type fuzzCase struct {
	pattern string
	indexFn func(string) []int
	alpha   string
}

// fuzzCases covers the ambiguous constructs that exercise the TDFA register
func fuzzCases() []fuzzCase {
	return []fuzzCase{
		{`(a*)(a*)`, FindStarStarIndex, "ab"},
		{`(a*)(a*)(a*)`, FindSSSIndex, "ab"},
		{`(a|ab)(a*)`, FindAltStarIndex, "ab"},
		{`(a?)(a*)`, FindOptStarIndex, "ab"},
		{`(a*)*`, FindNestStarIndex, "ab"},
		{`(?i)(abc)`, FindCaseIGIndex, "abcABC"},
		{`(?i)(a)(b)`, FindCaseI2Index, "abAB"},
		{`(?i)(a*)(a*)`, FindCaseISSIndex, "aAb"},
		{`(\d+)(\d*)`, FindDigitsSubIndex, "012"},
		{`(\w+)(\w*)`, FindWordsSubIndex, "a1_ "},

		// Interior always-true \B: folds to a no-op, so parity must hold under
		{`(a\Bb)`, FindNegWBabIndex, "ab"},
		{`(foo\Bbar)`, FindNegWBFoobarIndex, "fobar"},
		{`(foo\B)(bar)`, FindNegWBFooBarIndex, "fobar"},
		{`(foo)(\Bbar)`, FindNegWBFooBar2Index, "fobar"},
		{`(\w+\B\w+)`, FindNegWBWordsIndex, "ab1_ "},
		{`(\w+)\B(\w+)`, FindNegWBTwoWordsIndex, "ab1_ "},
	}
}

// TestSubmatchDifferentialFuzz is the primary safety net: for each pattern it
func TestSubmatchDifferentialFuzz(t *testing.T) {
	for _, c := range fuzzCases() {
		c := c
		t.Run(c.pattern, func(t *testing.T) {
			re := regexp.MustCompile("^(?:" + c.pattern + ")$")
			rng := rand.New(rand.NewSource(0x5EED1234))
			alpha := []rune(c.alpha)

			check := func(in string) {
				want := re.FindStringSubmatchIndex(in)
				got := c.indexFn(in)
				require.Equal(t, want, got, "pattern %q input %q", c.pattern, in)
			}

			// The empty string and every character.
			check("")
			for _, r := range alpha {
				check(string(r))
			}
			// 40k random inputs up to length, biased toward runs that expose
			for iter := 0; iter < 40000; iter++ {
				n := rng.Intn(13)
				b := make([]rune, n)
				for i := range b {
					b[i] = alpha[rng.Intn(len(alpha))]
				}
				check(string(b))
			}
			// Long uniform runs (adversarial for adjacent quantifiers).
			for _, r := range alpha {
				for _, n := range []int{16, 33, 64} {
					s := make([]rune, n)
					for i := range s {
						s[i] = r
					}
					check(string(s))
				}
			}
		})
	}
}
