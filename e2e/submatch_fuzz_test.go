package e2e

import (
	"math/rand"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// fuzzCase pairs a generated submatch-index matcher with its source pattern and
// the alphabet random inputs are drawn from.
type fuzzCase struct {
	pattern string
	indexFn func(string) []int
	alpha   string
}

// fuzzCases covers the ambiguous constructs that exercise the TDFA register
// machine's disambiguation and register copies, plus a couple of realistic
// patterns. The alphabet is kept tight per pattern so random inputs actually
// stress the interesting boundaries (adjacent stars, overlapping alternation).
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
	}
}

// TestSubmatchDifferentialFuzz is the primary safety net: for each pattern it
// compares the generated FindXxxIndex against stdlib
// regexp.FindStringSubmatchIndex byte-for-byte over many random inputs
// (adversarial repetition, the empty string, and no-match inputs included). The
// seed is fixed so CI is deterministic.
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

			// The empty string and every single character.
			check("")
			for _, r := range alpha {
				check(string(r))
			}
			// 40k random inputs up to length 12, biased toward runs that expose
			// star/alternation ambiguity.
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
