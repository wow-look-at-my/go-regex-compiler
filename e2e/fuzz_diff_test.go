package e2e

import (
	"math/rand"
	"regexp"
	"regexp/syntax"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
)

// This file drives the deterministic differential fuzz corpus generated into
// gen_fuzz_corpus_test.go (see generate_fixtures.go): every generated matcher
// is compared against stdlib regexp over a fixed, seeded input set. The
// corpus targets the historically broken generator classes -- prefix-mode
// acceptance latching, chain-compression re-entry (including the contains
// restart), (?i) submatch fold orbits, contains-mode empty-match and literal
// fast-path bodies, invalid-UTF-8 parity -- plus 50 seeded random patterns.
// Empty-width assertion placements that dfa.ValidateAssertions rejects are
// absent from the corpus by construction; TestFuzzCorpusMatchesValidator
// pins that split so a silently-miscompiling placement cannot sneak back in.
// Everything is deterministic; no network, runtime well under a second.

// fuzzFixedInputs are shared edge-case inputs applied to every pattern.
// The last row is invalid UTF-8: generated matchers must agree with regexp,
// which decodes each bad byte as one U+FFFD rune and keeps matching.
var fuzzFixedInputs = []string{
	"", "a", "b", "c", "x", "ab", "ba", "abc", "abcd", "abcabc", "aab", "abb",
	"0", "01", "0123", "_", "a_b", "a b", " ", "\t", "\n", "a\n", "\na",
	"a\nb", "ab\ncd", "\n\n", "foo", "bar", "foobar", "foo bar", "xfoox",
	"word", "a word here", "aaab", "aaabaaaba", "aaabaaabaaa", "aaa", "aaaa",
	"aaxaab", "aaxaaab", "hello", "HELLO", "hElLo", "kx", "Kx", "Kx",
	"K", "k", "ſ", "héllo", "HÉLLO", "xhéllox", "error", "xerrorx", "e42",
	"a!", "!a", "!", "a?b", "xyxyxyxy", "zzabzz", "xaby",
	"end.", ".start", strings.Repeat("a", 40), strings.Repeat("ab", 20),
	"\xff", "a\xffb", "\xc3", "\xc3(", "\xed\xa0\x80", "\xf8\x88\x80\x80\x80", "�",
}

// fuzzInputsFor returns the deterministic input set for a pattern: the fixed
// edge cases plus seeded random strings over a pattern-aware alphabet.
func fuzzInputsFor(pattern string) []string {
	alphabet := []rune{'a', 'b', 'c', 'x', 'y', '0', '1', '_', ' ', '\n', '!', 'K', 'k', 'é'}
	for _, r := range pattern {
		if r > ' ' && !strings.ContainsRune(`\()[]{}|*+?^$.`, r) {
			alphabet = append(alphabet, r)
		}
	}
	seed := int64(7919)
	for _, r := range pattern {
		seed = seed*31 + int64(r)
	}
	rng := rand.New(rand.NewSource(seed))
	inputs := append([]string{}, fuzzFixedInputs...)
	for i := 0; i < 40; i++ {
		n := rng.Intn(13)
		var b strings.Builder
		for j := 0; j < n; j++ {
			b.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		inputs = append(inputs, b.String())
	}
	return inputs
}

// fuzzStdlib compiles the stdlib equivalent of a generated matcher: full mode
// is ^(?:p)$, prefix is ^(?:p), contains is unanchored p.
func fuzzStdlib(t *testing.T, pattern, mode string) *regexp.Regexp {
	t.Helper()
	var anchored string
	switch mode {
	case "full":
		anchored = "^(?:" + pattern + ")$"
	case "prefix":
		anchored = "^(?:" + pattern + ")"
	case "contains":
		anchored = pattern
	default:
		t.Fatalf("unknown mode %q", mode)
	}
	re, err := regexp.Compile(anchored)
	require.NoError(t, err, "stdlib compile %q", anchored)
	return re
}

// TestFuzzDifferential compares every generated bool matcher in the fuzz
// corpus against stdlib regexp.MatchString, including on invalid UTF-8.
func TestFuzzDifferential(t *testing.T) {
	require.NotEmpty(t, fuzzCorpus, "fuzz corpus is empty; run `go generate ./e2e/...`")
	comparisons := 0
	for _, c := range fuzzCorpus {
		re := fuzzStdlib(t, c.Pattern, c.Mode)
		for _, in := range fuzzInputsFor(c.Pattern) {
			comparisons++
			want := re.MatchString(in)
			assert.Equal(t, want, c.Fn(in),
				"pattern %q mode %s input %q: generated matcher disagrees with stdlib",
				c.Pattern, c.Mode, in)
		}
	}
	t.Logf("fuzz differential: %d bool comparisons across %d (pattern,mode) cases",
		comparisons, len(fuzzCorpus))
}

// TestFuzzDifferentialSubmatch compares every generated submatch Index
// function in the fuzz corpus against stdlib FindStringSubmatchIndex on the
// anchored (full-match) pattern.
func TestFuzzDifferentialSubmatch(t *testing.T) {
	require.NotEmpty(t, fuzzSubCorpus, "fuzz submatch corpus is empty; run `go generate ./e2e/...`")
	comparisons := 0
	for _, c := range fuzzSubCorpus {
		re := regexp.MustCompile("^(?:" + c.Pattern + ")$")
		for _, in := range fuzzInputsFor(c.Pattern) {
			comparisons++
			want := re.FindStringSubmatchIndex(in)
			assert.Equal(t, want, c.IndexFn(in),
				"pattern %q input %q: generated Index disagrees with stdlib", c.Pattern, in)
		}
	}
	t.Logf("fuzz differential: %d submatch comparisons across %d patterns",
		comparisons, len(fuzzSubCorpus))
}

// TestFuzzCorpusMatchesValidator re-derives, for every corpus pattern and
// mode, whether the generator considers the combination usable (assertions
// validate for the mode's anchoring and the DFA builds within the state
// cap), and asserts the generated corpus contains exactly the usable combos.
// This pins the item-A contract: an assertion placement is either compiled
// and differentially correct, or loudly rejected at generation time -- never
// silently miscompiled and quietly dropped from coverage.
func TestFuzzCorpusMatchesValidator(t *testing.T) {
	require.NotEmpty(t, fuzzPatterns, "fuzz corpus is empty; run `go generate ./e2e/...`")
	inCorpus := make(map[[2]string]bool, len(fuzzCorpus))
	for _, c := range fuzzCorpus {
		inCorpus[[2]string{c.Pattern, c.Mode}] = true
	}

	modes := []struct {
		tag                    string
		anchorStart, anchorEnd bool
		search                 bool
	}{
		{"full", true, true, false},
		{"prefix", true, false, false},
		{"contains", false, false, true},
	}

	const maxStates = 300 // must match fuzzMaxStates in generate_fixtures.go
	accepted, rejected := 0, 0
	for _, p := range fuzzPatterns {
		re, err := syntax.Parse(p, syntax.Perl)
		require.NoError(t, err, "pattern %q", p)
		for _, m := range modes {
			prog, err := syntax.Compile(re.Simplify())
			require.NoError(t, err, "pattern %q", p)
			usable := dfa.ValidateAssertions(prog, m.anchorStart, m.anchorEnd) == nil
			if usable {
				build := dfa.Build
				if m.search {
					build = dfa.BuildSearch
				}
				d, err := build(prog)
				usable = err == nil && len(d.States) <= maxStates
			}
			if usable {
				accepted++
			} else {
				rejected++
			}
			assert.Equal(t, usable, inCorpus[[2]string{p, m.tag}],
				"pattern %q mode %s: usable=%v but corpus presence=%v",
				p, m.tag, usable, inCorpus[[2]string{p, m.tag}])
		}
	}
	assert.Positive(t, accepted, "expected some accepted (pattern, mode) combos")
	assert.Positive(t, rejected, "expected some validator-rejected combos in the directed set")
	t.Logf("fuzz corpus: %d combos compiled and differentially tested, %d rejected at generation",
		accepted, rejected)
}

