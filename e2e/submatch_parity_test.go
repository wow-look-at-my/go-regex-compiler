package e2e

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// submatchParityCase pairs a generated matcher family with its source pattern
// and a set of inputs to compare byte-for-byte against stdlib regexp.
type submatchParityCase struct {
	name    string
	pattern string
	findFn  func(string) []string
	indexFn func(string) []int
	namesFn func() []string
	inputs  []string
}

// sharedInputs are applied to every parity case in addition to the
// pattern-specific inputs, to broaden coverage cheaply.
var sharedInputs = []string{
	"", " ", "a", "ab", "abc", "abcd", "x", "xy", "y", "c", "ac", "bc",
	"1234", "12", "0000", "99999", "no match here", "ab ", " ab",
	"foo", "foo bar", "key=value", `key="value"`, "123-45",
}

func parityCases() []submatchParityCase {
	return []submatchParityCase{
		{
			name: "named_ym", pattern: `(?P<y>\d{2})(?P<m>\d{2})`,
			findFn: FindYM, indexFn: FindYMIndex, namesFn: NamesYM,
			inputs: []string{"1234", "0000", "99", "abcd", "123", "12345"},
		},
		{
			name: "optional", pattern: `(a)?b`,
			findFn: FindOpt, indexFn: FindOptIndex, namesFn: NamesOpt,
			inputs: []string{"b", "ab", "a", "abb", "bb"},
		},
		{
			name: "alternation", pattern: `(a|b)c`,
			findFn: FindAlt2, indexFn: FindAlt2Index, namesFn: NamesAlt2,
			inputs: []string{"ac", "bc", "c", "abc", "a"},
		},
		{
			name: "nested", pattern: `((a)(b))`,
			findFn: FindNest, indexFn: FindNestIndex, namesFn: NamesNest,
			inputs: []string{"ab", "a", "b", "abab"},
		},
		{
			name: "last_wins", pattern: `(ab)+`,
			findFn: FindLast, indexFn: FindLastIndex, namesFn: NamesLast,
			inputs: []string{"ab", "abab", "ababab", "a", "aba"},
		},
		{
			name: "noncapture", pattern: `(?:ab)(c)`,
			findFn: FindNoncap, indexFn: FindNoncapIndex, namesFn: NamesNoncap,
			inputs: []string{"abc", "ab", "c", "abcc"},
		},
		{
			name: "zerowidth_anchor", pattern: `(\babc)`,
			findFn: FindZW, indexFn: FindZWIndex, namesFn: NamesZW,
			inputs: []string{"abc", " abc", "xabc", "abc "},
		},
		{
			name: "word_boundary", pattern: `(\w+)\b`,
			findFn: FindWB, indexFn: FindWBIndex, namesFn: NamesWB,
			inputs: []string{"foo", "foo bar", "123", "a"},
		},
		{
			name:    "apache",
			pattern: `(?P<ip>\d+\.\d+\.\d+\.\d+) - - \[(?P<ts>[^\]]+)\] "(?P<method>[A-Z]+) (?P<path>[^ ]+) (?P<proto>[^"]+)" (?P<status>\d{3}) (?P<size>\d+)`,
			findFn:  FindApache, indexFn: FindApacheIndex, namesFn: NamesApache,
			inputs: []string{
				`127.0.0.1 - - [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`,
				`10.0.0.1 - - [01/Jan/2020:00:00:00 +0000] "POST /api HTTP/1.1" 404 13`,
				`bad line`,
			},
		},
		{
			name:    "rfc3339",
			pattern: `(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})T(?P<hour>\d{2}):(?P<min>\d{2}):(?P<sec>\d{2})Z`,
			findFn:  FindRFC, indexFn: FindRFCIndex, namesFn: NamesRFC,
			inputs: []string{"2020-01-02T03:04:05Z", "1999-12-31T23:59:59Z", "not a date", "2020-01-02"},
		},
		{
			name: "logfmt", pattern: `(?P<key>\w+)="(?P<val>[^"]*)"`,
			findFn: FindLogfmt, indexFn: FindLogfmtIndex, namesFn: NamesLogfmt,
			inputs: []string{`key="value"`, `name="quoted value"`, `empty=""`, `noquote=value`, `x="a b c"`},
		},
		{
			name: "named_optional2", pattern: `(?P<a>x)?(?P<b>y)`,
			findFn: FindOpt2, indexFn: FindOpt2Index, namesFn: NamesOpt2,
			inputs: []string{"y", "xy", "x", "yy"},
		},

		// Ambiguous-capture cases: these compile via the TDFA register machine
		// (the one-pass path rejects them). Adjacent greedy stars, overlapping
		// alternation, optional-then-star, nested star, and (?i) fold classes —
		// the exact constructs that used to require the interpreter.
		{
			name: "amb_starstar", pattern: `(a*)(a*)`,
			findFn: FindStarStar, indexFn: FindStarStarIndex, namesFn: NamesStarStar,
			inputs: []string{"", "a", "aa", "aaa", "aaaa", "aaaaaa", "b", "ba"},
		},
		{
			name: "amb_sss", pattern: `(a*)(a*)(a*)`,
			findFn: FindSSS, indexFn: FindSSSIndex, namesFn: NamesSSS,
			inputs: []string{"", "a", "aa", "aaa", "aaaaa", "b"},
		},
		{
			name: "amb_altstar", pattern: `(a|ab)(a*)`,
			findFn: FindAltStar, indexFn: FindAltStarIndex, namesFn: NamesAltStar,
			inputs: []string{"a", "ab", "aa", "aab", "aba", "abab", "aaa", "b"},
		},
		{
			name: "amb_optstar", pattern: `(a?)(a*)`,
			findFn: FindOptStar, indexFn: FindOptStarIndex, namesFn: NamesOptStar,
			inputs: []string{"", "a", "aa", "aaa", "b"},
		},
		{
			name: "amb_neststar", pattern: `(a*)*`,
			findFn: FindNestStar, indexFn: FindNestStarIndex, namesFn: NamesNestStar,
			inputs: []string{"", "a", "aa", "aaa", "b"},
		},
		{
			name: "amb_casei_group", pattern: `(?i)(abc)`,
			findFn: FindCaseIG, indexFn: FindCaseIGIndex, namesFn: NamesCaseIG,
			inputs: []string{"abc", "ABC", "AbC", "aBc", "abd", "ab", "abcd"},
		},
		{
			name: "amb_casei2", pattern: `(?i)(a)(b)`,
			findFn: FindCaseI2, indexFn: FindCaseI2Index, namesFn: NamesCaseI2,
			inputs: []string{"ab", "AB", "aB", "Ab", "a", "b", "abc"},
		},
		{
			name: "amb_casei_starstar", pattern: `(?i)(a*)(a*)`,
			findFn: FindCaseISS, indexFn: FindCaseISSIndex, namesFn: NamesCaseISS,
			inputs: []string{"", "a", "A", "aA", "AaA", "aaa", "b"},
		},
		{
			name: "amb_digits", pattern: `(\d+)(\d*)`,
			findFn: FindDigitsSub, indexFn: FindDigitsSubIndex, namesFn: NamesDigitsSub,
			inputs: []string{"1", "12", "123", "1234", "a", ""},
		},
		{
			name: "amb_words", pattern: `(\w+)(\w*)`,
			findFn: FindWordsSub, indexFn: FindWordsSubIndex, namesFn: NamesWordsSub,
			inputs: []string{"a", "ab", "abc", "a_1", "A9z", " "},
		},

		// Interior always-true \B cases: the \B sits between two word characters,
		// where "no boundary" always holds, so it folds to a no-op. The literal
		// sequences compile one-pass; the adjacent-\w+ pair compiles via TDFA.
		// These are the exact patterns that used to require the interpreter.
		{
			name: "negwb_ab", pattern: `(a\Bb)`,
			findFn: FindNegWBab, indexFn: FindNegWBabIndex, namesFn: NamesNegWBab,
			inputs: []string{"ab", "a", "b", "abc", "aab", "ba", ""},
		},
		{
			name: "negwb_foobar", pattern: `(foo\Bbar)`,
			findFn: FindNegWBFoobar, indexFn: FindNegWBFoobarIndex, namesFn: NamesNegWBFoobar,
			inputs: []string{"foobar", "foo", "bar", "foobarbaz", "fobar", "fooba"},
		},
		{
			name: "negwb_foo_bar", pattern: `(foo\B)(bar)`,
			findFn: FindNegWBFooBar, indexFn: FindNegWBFooBarIndex, namesFn: NamesNegWBFooBar,
			inputs: []string{"foobar", "foo", "bar", "foobarx", "fooba"},
		},
		{
			name: "negwb_foo_bar2", pattern: `(foo)(\Bbar)`,
			findFn: FindNegWBFooBar2, indexFn: FindNegWBFooBar2Index, namesFn: NamesNegWBFooBar2,
			inputs: []string{"foobar", "foo", "bar", "xfoobar", "fooba"},
		},
		{
			name: "negwb_words", pattern: `(\w+\B\w+)`,
			findFn: FindNegWBWords, indexFn: FindNegWBWordsIndex, namesFn: NamesNegWBWords,
			inputs: []string{"ab", "abc", "a", "a1_", "A9z", " ", "hello", "a b"},
		},
		{
			name: "negwb_two_words", pattern: `(\w+)\B(\w+)`,
			findFn: FindNegWBTwoWords, indexFn: FindNegWBTwoWordsIndex, namesFn: NamesNegWBTwoWords,
			inputs: []string{"ab", "abc", "a", "a1_", "A9z", " ", "hello", "a b"},
		},
	}
}

// TestSubmatchParity asserts the generated FindSub*/FindSub*Index functions are
// byte-for-byte equal to stdlib regexp.FindStringSubmatch /
// FindStringSubmatchIndex over the anchored pattern, for every input.
func TestSubmatchParity(t *testing.T) {
	for _, c := range parityCases() {
		t.Run(c.name, func(t *testing.T) {
			re := regexp.MustCompile("^(?:" + c.pattern + ")$")
			inputs := append(append([]string{}, sharedInputs...), c.inputs...)
			for _, in := range inputs {
				wantStr := re.FindStringSubmatch(in)
				gotStr := c.findFn(in)
				assert.Equal(t, wantStr, gotStr, "FindStringSubmatch mismatch for %q (input=%q)", c.pattern, in)

				wantIdx := re.FindStringSubmatchIndex(in)
				gotIdx := c.indexFn(in)
				assert.Equal(t, wantIdx, gotIdx, "FindStringSubmatchIndex mismatch for %q (input=%q)", c.pattern, in)

				// No-match contract: both APIs return nil.
				if wantStr == nil {
					assert.Nil(t, gotStr, "expected nil submatch for non-match %q", in)
					assert.Nil(t, gotIdx, "expected nil index for non-match %q", in)
				}
			}
		})
	}
}

// TestSubexpNamesParity asserts the generated SubexpNames accessors equal the
// stdlib SubexpNames for the same pattern.
func TestSubexpNamesParity(t *testing.T) {
	for _, c := range parityCases() {
		t.Run(c.name, func(t *testing.T) {
			want := regexp.MustCompile(c.pattern).SubexpNames()
			assert.Equal(t, want, c.namesFn())
		})
	}
}

// TestTypedStructFields verifies the typed capture struct exposes the right
// fields with the right values on representative inputs, and Matched=false on a
// non-match. Covers the RFC3339, Apache, logfmt, and optional-named structs.
func TestTypedStructFields(t *testing.T) {
	t.Run("rfc3339", func(t *testing.T) {
		got := FindRFC3339("2020-01-02T03:04:05Z")
		require.True(t, got.Matched)
		assert.Equal(t, "2020", got.Year)
		assert.Equal(t, "01", got.Month)
		assert.Equal(t, "02", got.Day)
		assert.Equal(t, "03", got.Hour)
		assert.Equal(t, "04", got.Min)
		assert.Equal(t, "05", got.Sec)

		miss := FindRFC3339("not a date")
		assert.False(t, miss.Matched)
		assert.Equal(t, "", miss.Year)
		assert.Equal(t, "", miss.Sec)
	})

	t.Run("apache", func(t *testing.T) {
		line := `127.0.0.1 - - [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`
		got := FindApacheLine(line)
		require.True(t, got.Matched)
		assert.Equal(t, "127.0.0.1", got.Ip)
		assert.Equal(t, "10/Oct/2000:13:55:36 -0700", got.Ts)
		assert.Equal(t, "GET", got.Method)
		assert.Equal(t, "/apache_pb.gif", got.Path)
		assert.Equal(t, "HTTP/1.0", got.Proto)
		assert.Equal(t, "200", got.Status)
		assert.Equal(t, "2326", got.Size)
	})

	t.Run("logfmt", func(t *testing.T) {
		got := FindLogfmtField(`name="quoted value"`)
		require.True(t, got.Matched)
		assert.Equal(t, "name", got.Key)
		assert.Equal(t, "quoted value", got.Val)
	})

	t.Run("optional_named_unmatched_is_empty", func(t *testing.T) {
		// (?P<a>x)?(?P<b>y) on "y": group a does not participate -> "".
		got := FindOpt2Struct("y")
		require.True(t, got.Matched)
		assert.Equal(t, "", got.A, "unmatched optional named group must yield empty field")
		assert.Equal(t, "y", got.B)

		full := FindOpt2Struct("xy")
		require.True(t, full.Matched)
		assert.Equal(t, "x", full.A)
		assert.Equal(t, "y", full.B)
	})

	t.Run("ym_struct", func(t *testing.T) {
		got := FindYMStruct("1234")
		require.True(t, got.Matched)
		assert.Equal(t, "12", got.Y)
		assert.Equal(t, "34", got.M)

		assert.False(t, FindYMStruct("abcd").Matched)
	})
}
