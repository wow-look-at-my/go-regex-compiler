package dfa

import (
	"regexp/syntax"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseForAssert(t *testing.T, pattern string) *syntax.Prog {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	require.NoError(t, err)
	re = re.Simplify()
	prog, err := syntax.Compile(re)
	require.NoError(t, err)
	return prog
}

func TestValidateAssertionsAccepted(t *testing.T) {
	cases := []struct {
		name                   string
		pattern                string
		anchorStart, anchorEnd bool
	}{
		{"no_assertions_full", "abc", true, true},
		{"no_assertions_contains", "a*b", false, false},
		{"leading_caret_full", "^abc", true, true},
		{"trailing_dollar_full", "abc$", true, true},
		{"both_anchors_full", "^abc$", true, true},
		{"begin_text_full", `\Aabc`, true, true},
		{"end_text_full", `abc\z`, true, true},
		{"leading_caret_prefix", "^abc", true, false},
		{"multiline_caret_at_start_full", "(?m)^abc", true, true},
		{"multiline_dollar_at_end_full", "(?m)abc$", true, true},
		// \b provably always satisfied at the crossing point:
		{"leading_wordb_before_word_full", `\babc`, true, true},
		{"trailing_wordb_after_word_full", `(\w+)\b`, true, true},
		{"leading_wordb_before_word_capture", `(\babc)`, true, true},
		{"leading_wordb_prefix", `\babc`, true, false},
		// \B where sides are always word characters:
		{"nonwordb_between_words_full", `a\Bb`, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseForAssert(t, tc.pattern)
			assert.NoError(t, ValidateAssertions(prog, tc.anchorStart, tc.anchorEnd))
		})
	}
}

func TestValidateAssertionsRejected(t *testing.T) {
	cases := []struct {
		name                   string
		pattern                string
		anchorStart, anchorEnd bool
		wantSubstr             string
	}{
		// The DFA used to full-match "foobar" for foo\bbar (stdlib does not).
		{"mid_wordb_full", `foo\bbar`, true, true, `\b`},
		// The DFA used to full-match "ab" for a$b.
		{"mid_dollar_full", `a$b`, true, true, "$"},
		{"mid_caret_full", `a^b`, true, true, "^"},
		// Prefix mode used to treat ab$ like ab (matched "abc").
		{"trailing_dollar_prefix", `ab$`, true, false, "end"},
		// Prefix mode: what follows the match is unknown, so \b at the end
		{"trailing_wordb_prefix", `[a-z]+\b`, true, false, `\b`},
		// Contains mode used to match "xabc" for ^abc.
		{"leading_caret_contains", `^abc`, false, false, "contains"},
		{"trailing_dollar_contains", `abc$`, false, false, "contains"},
		// Contains mode: the character before the match is unknown.
		{"leading_wordb_contains", `\berror`, false, false, `\b`},
		// Contains mode used to substring-match "foobar" for \bfoo\b.
		{"surrounding_wordb_contains", `\bfoo\b`, false, false, `\b`},
		// Prefix mode used to match "a" for the bare $.
		{"bare_dollar_prefix", `$`, true, false, "end"},
		// Mid-pattern multiline anchors depend on neighboring newlines.
		{"mid_multiline_caret_full", "a\n(?m:^b)", true, true, "(?m)^"},
		{"mid_multiline_dollar_full", "(?m:a$)\nb", true, true, "(?m)$"},
		// \b that can never hold (word on sides) is also rejected.
		{"wordb_never_holds", `a\bb`, true, true, `\b`},
		// Interior \b whose following side is a mixed class (.) is conditional,
		{"mid_wordb_before_word_mixed_after", `a\b.`, true, true, `\b`},
		// Interior \B with a mixed class on sides is likewise conditional.
		{"mid_nonwordb_mixed_sides", `.\B.`, true, true, `\B`},
		// Loops make a "leading" anchor reachable mid-match.
		{"caret_in_loop_full", `(^a)+`, true, true, "^"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseForAssert(t, tc.pattern)
			err := ValidateAssertions(prog, tc.anchorStart, tc.anchorEnd)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSubstr)
		})
	}
}
