package e2e

import (
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

type testCase struct {
	input string
	match bool
}

func TestIntegration(t *testing.T) {
	tests := []struct {
		name    string
		matchFn func(string) bool
		cases   []testCase
	}{
		{
			name: "abc", matchFn: MatchLiteral,
			cases: []testCase{
				{"abc", true},
				{"", false},
				{"ab", false},
				{"abcd", false},
				{"ABC", false},
				{"xabc", false},
			},
		},
		{
			name: "[a-z]+", matchFn: MatchCharClass,
			cases: []testCase{
				{"hello", true},
				{"a", true},
				{"abc", true},
				{"", false},
				{"123", false},
				{"Hello", false},
				{"hello world", false},
			},
		},
		{
			name: "a*", matchFn: MatchAStar,
			cases: []testCase{
				{"", true},
				{"a", true},
				{"aaa", true},
				{"b", false},
				{"ab", false},
			},
		},
		{
			name: "a+", matchFn: MatchAPlus,
			cases: []testCase{
				{"a", true},
				{"aaa", true},
				{"", false},
				{"b", false},
				{"ab", false},
			},
		},
		{
			name: "a?", matchFn: MatchAQuestion,
			cases: []testCase{
				{"", true},
				{"a", true},
				{"aa", false},
				{"b", false},
			},
		},
		{
			name: "a|b", matchFn: MatchAOrB,
			cases: []testCase{
				{"a", true},
				{"b", true},
				{"", false},
				{"c", false},
				{"ab", false},
			},
		},
		{
			name: "(a|b)*c", matchFn: MatchAltStarC,
			cases: []testCase{
				{"c", true},
				{"ac", true},
				{"bc", true},
				{"ababc", true},
				{"aabbc", true},
				{"", false},
				{"a", false},
				{"ab", false},
				{"ca", false},
			},
		},
		{
			name: `\d{3}-\d{2}-\d{4}`, matchFn: MatchSSN,
			cases: []testCase{
				{"123-45-6789", true},
				{"000-00-0000", true},
				{"12-45-6789", false},
				{"123-456-789", false},
				{"abc-de-fghi", false},
				{"", false},
			},
		},
		{
			name: `[A-Za-z_][A-Za-z0-9_]*`, matchFn: MatchIdentifier,
			cases: []testCase{
				{"x", true},
				{"_", true},
				{"foo", true},
				{"_bar", true},
				{"camelCase", true},
				{"with123", true},
				{"_under_score", true},
				{"", false},
				{"123abc", false},
				{"-dash", false},
			},
		},
		{
			name: `[0-9]+\.[0-9]+`, matchFn: MatchDottedNumber,
			cases: []testCase{
				{"1.0", true},
				{"123.456", true},
				{"0.0", true},
				{"1.", false},
				{".1", false},
				{"123", false},
				{"abc", false},
			},
		},
		{
			name: "(foo|bar)baz", matchFn: MatchFooBarBaz,
			cases: []testCase{
				{"foobaz", true},
				{"barbaz", true},
				{"baz", false},
				{"fobaz", false},
				{"foobazz", false},
			},
		},
		{
			name: `(https?://)?[a-z]+\.[a-z]{2,}`, matchFn: MatchURL,
			cases: []testCase{
				{"example.com", true},
				{"http://example.com", true},
				{"https://example.com", true},
				{"foo.co", true},
				{"", false},
				{"example", false},
				{"example.c", false},
			},
		},
		{
			name: "empty", matchFn: MatchEmpty,
			cases: []testCase{
				{"", true},
				{"a", false},
			},
		},
		{
			name: ".", matchFn: MatchDot,
			cases: []testCase{
				{"a", true},
				{"1", true},
				{"Z", true},
				{"", false},
				{"\n", false},
				{"ab", false},
			},
		},
		{
			name: "(?i)abc", matchFn: MatchCaseIAbc,
			cases: []testCase{
				{"abc", true},
				{"ABC", true},
				{"Abc", true},
				{"aBc", true},
				{"abC", true},
				{"", false},
				{"ab", false},
				{"abcd", false},
			},
		},
		{
			name: `\w+`, matchFn: MatchWordChars,
			cases: []testCase{
				{"hello", true},
				{"Hello123", true},
				{"_under", true},
				{"a", true},
				{"", false},
				{"hello world", false},
				{"hello!", false},
			},
		},
		{
			name: `\s+`, matchFn: MatchWhitespace,
			cases: []testCase{
				{" ", true},
				{"\t", true},
				{"\n", true},
				{"  \t\n", true},
				{"", false},
				{"a", false},
			},
		},
		{
			// Regression: chain-compression counters must reset when a DFA
			// loop re-enters a compressed chain head (a{3} is compressed, and
			// (?:ba{3})* re-enters it).
			name: `a{3}(?:ba{3})*`, matchFn: MatchChainReentry,
			cases: []testCase{
				{"aaa", true},
				{"aaabaaa", true},
				{"aaabaaabaaa", true}, // stale counter made this a false negative
				{"aaabaaaba", false},  // stale counter made this a false positive
				{"aaabaaab", false},
				{"aaab", false},
				{"aa", false},
				{"aaaa", false},
				{"", false},
			},
		},
		{
			name: `[a-z0-9][a-z0-9._-]{0,127}`, matchFn: MatchContainer,
			cases: []testCase{
				{"a", true},
				{"abc", true},
				{"a123", true},
				{"project.name", true},
				{"my-project", true},
				{"my_project", true},
				{"a.b-c_d", true},
				{"9start", true},
				{strings.Repeat("a", 128), true},
				{"", false},
				{"A", false},
				{"-start", false},
				{".start", false},
				{"_start", false},
				{strings.Repeat("a", 129), false},
				{"has space", false},
				{"UPPER", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, tc := range tt.cases {
				got := tt.matchFn(tc.input)
				assert.Equal(t, tc.match, got)

			}
		})
	}
}

func TestIntegrationPrefix(t *testing.T) {
	tests := []struct {
		name    string
		matchFn func(string) bool
		cases   []testCase
	}{
		{
			name: "[a-z]+", matchFn: MatchPrefixCharClass,
			cases: []testCase{
				{"hello", true},
				{"hello123", true}, // prefix "hello" matches
				{"hello world", true},
				{"", false},
				{"123", false},
			},
		},
		{
			name: `\d{3}-\d{2}`, matchFn: MatchPrefixDigitDash,
			cases: []testCase{
				{"123-45", true},
				{"123-45-6789", true}, // prefix matches
				{"12-45", false},
				{"abc", false},
			},
		},
		{
			name: "a+b", matchFn: MatchPrefixAPlusB,
			cases: []testCase{
				{"ab", true},
				{"aab", true},
				{"aabcdef", true}, // prefix "aab" matches
				{"a", false},      // no prefix matches (b required)
				{"aaac", false},
				{"", false},
			},
		},
		{
			// Regression: prefix mode must latch acceptance per step, not
			// check only the final DFA state. The prefix "a" matches even
			// though the walk continues into the non-accepting "ab" state.
			name: "a|abc", matchFn: MatchPrefixAlt,
			cases: []testCase{
				{"a", true},
				{"ab", true}, // prefix "a" (final state after "ab" is non-accepting)
				{"abc", true},
				{"abcd", true},
				{"ax", true}, // prefix "a"
				{"b", false},
				{"", false},
				{"xa", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, tc := range tt.cases {
				got := tt.matchFn(tc.input)
				assert.Equal(t, tc.match, got)

			}
		})
	}
}

func TestIntegrationContains(t *testing.T) {
	tests := []struct {
		name    string
		matchFn func(string) bool
		cases   []testCase
	}{
		{
			name: "[a-z]+", matchFn: MatchContainsCharClass,
			cases: []testCase{
				{"hello", true},
				{"123hello456", true}, // substring matches
				{"HELLO", false},      // no lowercase substring
				{"123", false},
				{"", false},
				{"test@example.com", true},
			},
		},
		{
			name: `\d{3}-\d{2}-\d{4}`, matchFn: MatchContainsSSN,
			cases: []testCase{
				{"123-45-6789", true},
				{"SSN: 123-45-6789!", true}, // contained
				{"abc", false},
				{"123-45", false},
			},
		},
		{
			name: "error", matchFn: MatchContainsError,
			cases: []testCase{
				{"error", true},
				{"an error occurred", true},
				{"ERROR", false},
				{"", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, tc := range tt.cases {
				got := tt.matchFn(tc.input)
				assert.Equal(t, tc.match, got)

			}
		})
	}
}

// TestIntegrationAssertions covers empty-width assertions (\b \B ^ $ \A \z,
// including (?m)) across match modes; expectations mirror stdlib regexp with
// the mode's anchoring (^(?:p)$ / ^(?:p) / p).
func TestIntegrationAssertions(t *testing.T) {
	tests := []struct {
		name    string
		matchFn func(string) bool
		cases   []testCase
	}{
		{
			// \b between two word runes can never hold.
			name: `full a\bb`, matchFn: MatchABoundaryB,
			cases: []testCase{
				{"ab", false},
				{"a b", false},
				{"a", false},
				{"", false},
			},
		},
		{
			// Full match: $ (?m) may accept before a newline, but full mode
			// still has to consume the whole input.
			name: `full (?m)a$`, matchFn: MatchMLineADollar,
			cases: []testCase{
				{"a", true},
				{"ab", false},
				{"a\n", false}, // the \n is left unconsumed
				{"b", false},
				{"", false},
			},
		},
		{
			// ^(?:$) matches only the empty prefix at end of text.
			name: `prefix $`, matchFn: MatchPrefixDollar,
			cases: []testCase{
				{"", true},
				{"a", false},
				{"\n", false},
			},
		},
		{
			name: `prefix a$`, matchFn: MatchPrefixADollar,
			cases: []testCase{
				{"a", true},
				{"ab", false},
				{"a\n", false},
				{"", false},
			},
		},
		{
			name: `prefix foo\b`, matchFn: MatchPrefixFooB,
			cases: []testCase{
				{"foo", true},
				{"foo bar", true},
				{"foo!", true},
				{"foobar", false},
				{"fo", false},
				{"", false},
			},
		},
		{
			// ^a anchors to the start of text even in contains mode.
			name: `contains ^a`, matchFn: MatchContainsCaretA,
			cases: []testCase{
				{"a", true},
				{"abc", true},
				{"ba", false},
				{"\na", false},
				{"", false},
			},
		},
		{
			name: `contains \bfoo\b`, matchFn: MatchContainsWordB,
			cases: []testCase{
				{"foo", true},
				{"a foo b", true},
				{"foo!", true},
				{"!foo", true},
				{"foobar", false},
				{"xfoo", false},
				{"barfoobaz", false},
				{"", false},
			},
		},
		{
			name: `contains (?m)^b`, matchFn: MatchContainsMLineB,
			cases: []testCase{
				{"b", true},
				{"a\nb", true},
				{"\nb", true},
				{"ab", false},
				{"a b", false},
				{"", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, tc := range tt.cases {
				got := tt.matchFn(tc.input)
				assert.Equal(t, tc.match, got, "input %q", tc.input)
			}
		})
	}
}

type submatchCase struct {
	input  string
	groups []string // nil means no match, else groups[0]=full match, groups[1..]=captures
}

func TestIntegrationSubmatch(t *testing.T) {
	tests := []struct {
		name   string
		findFn func(string) []string
		cases  []submatchCase
	}{
		{
			name: `([a-z]+)@([a-z]+)`, findFn: FindSubEmail,
			cases: []submatchCase{
				{"user@host", []string{"user@host", "user", "host"}},
				{"abc@xyz", []string{"abc@xyz", "abc", "xyz"}},
				{"123", nil},
				{"", nil},
			},
		},
		{
			name: `(\d{3})-(\d{2})-(\d{4})`, findFn: FindSubSSN,
			cases: []submatchCase{
				{"123-45-6789", []string{"123-45-6789", "123", "45", "6789"}},
				{"000-00-0000", []string{"000-00-0000", "000", "00", "0000"}},
				{"abc", nil},
			},
		},
		{
			name: `(a+)(b+)`, findFn: FindSubAB,
			cases: []submatchCase{
				{"ab", []string{"ab", "a", "b"}},
				{"aaabbb", []string{"aaabbb", "aaa", "bbb"}},
				{"a", nil},
				{"b", nil},
			},
		},
		{
			name: `(foo|bar)baz`, findFn: FindSubFooBarBaz,
			cases: []submatchCase{
				{"foobaz", []string{"foobaz", "foo"}},
				{"barbaz", []string{"barbaz", "bar"}},
				{"baz", nil},
			},
		},
		{
			name: `([a-z]+)(\.[a-z]+)*`, findFn: FindSubDotted,
			cases: []submatchCase{
				{"hello", []string{"hello", "hello", ""}},
				{"abc.def", []string{"abc.def", "abc", ".def"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, tc := range tt.cases {
				result := tt.findFn(tc.input)
				if tc.groups == nil {
					assert.Nil(t, result)

				} else if assert.NotNil(t, result, "FindSubmatch(%q)", tc.input) {
					assert.Equal(t, tc.groups, result, "FindSubmatch(%q)", tc.input)
				}
			}
		})
	}
}
