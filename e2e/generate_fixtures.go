//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
)

type fixture struct {
	file     string
	regex    string
	funcName string
	mode     codegen.MatchMode
	submatch bool
	subFunc  string
}

// namedFixture drives generation of named-capture submatch fixtures: the
// positional family (Find/FindIndex), the SubexpNames accessor, and the typed
// capture struct. Used by the differential parity test in submatch_parity_test.go.
type namedFixture struct {
	file       string
	regex      string
	funcName   string // bool matcher name
	subFunc    string // positional submatch func (also generates <subFunc>Index)
	namesFunc  string // SubexpNames accessor
	structType string // typed capture struct type ("" => no struct)
	structFunc string // typed capture struct constructor
}

// namedFixtures is the corpus exercised by the parity test. It MUST include
// named groups, optional, alternation, nested, last-iteration-wins repetition,
// non-capturing, zero-width, and realistic log patterns (apache access line,
// RFC3339 timestamp, logfmt field).
var namedFixtures = []namedFixture{
	{"gen_named_ym.go", `(?P<y>\d{2})(?P<m>\d{2})`, "MatchYM", "FindYM", "NamesYM", "YM", "FindYMStruct"},
	{"gen_named_optional.go", `(a)?b`, "MatchOpt", "FindOpt", "NamesOpt", "", ""},
	{"gen_named_alt.go", `(a|b)c`, "MatchAlt2", "FindAlt2", "NamesAlt2", "", ""},
	{"gen_named_nested.go", `((a)(b))`, "MatchNest", "FindNest", "NamesNest", "", ""},
	{"gen_named_lastwins.go", `(ab)+`, "MatchLast", "FindLast", "NamesLast", "", ""},
	{"gen_named_noncap.go", `(?:ab)(c)`, "MatchNoncap", "FindNoncap", "NamesNoncap", "", ""},
	{"gen_named_zerowidth.go", `(\babc)`, "MatchZW", "FindZW", "NamesZW", "", ""},
	{"gen_named_wordb.go", `(\w+)\b`, "MatchWB", "FindWB", "NamesWB", "", ""},
	{"gen_named_apache.go",
		`(?P<ip>\d+\.\d+\.\d+\.\d+) - - \[(?P<ts>[^\]]+)\] "(?P<method>[A-Z]+) (?P<path>[^ ]+) (?P<proto>[^"]+)" (?P<status>\d{3}) (?P<size>\d+)`,
		"MatchApache", "FindApache", "NamesApache", "ApacheLine", "FindApacheLine"},
	{"gen_named_rfc3339.go",
		`(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})T(?P<hour>\d{2}):(?P<min>\d{2}):(?P<sec>\d{2})Z`,
		"MatchRFC", "FindRFC", "NamesRFC", "RFC3339", "FindRFC3339"},
	{"gen_named_logfmt.go", `(?P<key>\w+)="(?P<val>[^"]*)"`, "MatchLogfmt", "FindLogfmt", "NamesLogfmt", "LogfmtField", "FindLogfmtField"},
	{"gen_named_opt2.go", `(?P<a>x)?(?P<b>y)`, "MatchOpt2", "FindOpt2", "NamesOpt2", "Opt2", "FindOpt2Struct"},

	// Ambiguous-capture fixtures: the one-pass path rejects these (two live
	// consuming instructions can match the same rune, or a (?i) fold class), so
	// they compile via the TDFA register machine. Exercised by the parity and
	// differential-fuzz tests. NONE emit an interpreter.
	{"gen_amb_starstar.go", `(a*)(a*)`, "MatchStarStar", "FindStarStar", "NamesStarStar", "", ""},
	{"gen_amb_sss.go", `(a*)(a*)(a*)`, "MatchSSS", "FindSSS", "NamesSSS", "", ""},
	{"gen_amb_altstar.go", `(a|ab)(a*)`, "MatchAltStar", "FindAltStar", "NamesAltStar", "", ""},
	{"gen_amb_optstar.go", `(a?)(a*)`, "MatchOptStar", "FindOptStar", "NamesOptStar", "", ""},
	{"gen_amb_neststar.go", `(a*)*`, "MatchNestStar", "FindNestStar", "NamesNestStar", "", ""},
	{"gen_amb_casei.go", `(?i)(abc)`, "MatchCaseIG", "FindCaseIG", "NamesCaseIG", "", ""},
	{"gen_amb_casei2.go", `(?i)(a)(b)`, "MatchCaseI2", "FindCaseI2", "NamesCaseI2", "", ""},
	{"gen_amb_ci_ss.go", `(?i)(a*)(a*)`, "MatchCaseISS", "FindCaseISS", "NamesCaseISS", "", ""},
	{"gen_amb_digits.go", `(\d+)(\d*)`, "MatchDigitsSub", "FindDigitsSub", "NamesDigitsSub", "", ""},
	{"gen_amb_words.go", `(\w+)(\w*)`, "MatchWordsSub", "FindWordsSub", "NamesWordsSub", "", ""},
}

var fixtures = []fixture{
	// Full mode — testPatterns (used by correctness, benchmark, and size tests)
	{"gen_full_literal.go", "abc", "MatchLiteral", codegen.MatchFull, false, ""},
	{"gen_full_charclass.go", "[a-z]+", "MatchCharClass", codegen.MatchFull, false, ""},
	{"gen_full_alternation.go", "a|b|c", "MatchAlternation", codegen.MatchFull, false, ""},
	{"gen_full_starb.go", "a*b", "MatchStarB", codegen.MatchFull, false, ""},
	{"gen_full_digits.go", "[0-9]+", "MatchDigits", codegen.MatchFull, false, ""},
	{"gen_full_colour.go", "colou?r", "MatchColour", codegen.MatchFull, false, ""},
	{"gen_full_grouped_alt_qux.go", "(foo|bar|baz)qux", "MatchGroupedAltQux", codegen.MatchFull, false, ""},
	{"gen_full_ssn.go", `\d{3}-\d{2}-\d{4}`, "MatchSSN", codegen.MatchFull, false, ""},
	{"gen_full_identifier.go", `[A-Za-z_][A-Za-z0-9_]*`, "MatchIdentifier", codegen.MatchFull, false, ""},
	{"gen_full_dotted_number.go", `[0-9]+\.[0-9]+`, "MatchDottedNumber", codegen.MatchFull, false, ""},
	{"gen_full_url.go", `(https?://)?[a-z]+\.[a-z]{2,}`, "MatchURL", codegen.MatchFull, false, ""},
	{"gen_full_complex_alt.go", "(a|b)*abb", "MatchComplexAlt", codegen.MatchFull, false, ""},
	{"gen_full_casei_hello.go", "(?i)hello", "MatchCaseIHello", codegen.MatchFull, false, ""},
	{"gen_full_whitespace.go", `\s+`, "MatchWhitespace", codegen.MatchFull, false, ""},
	{"gen_full_wordchars.go", `\w+`, "MatchWordChars", codegen.MatchFull, false, ""},
	{"gen_full_dotplus.go", ".+", "MatchDotPlus", codegen.MatchFull, false, ""},
	{"gen_full_empty.go", "", "MatchEmpty", codegen.MatchFull, false, ""},
	{"gen_full_nested_quant.go", "(ab?c)+", "MatchNestedQuant", codegen.MatchFull, false, ""},
	{"gen_full_email.go", `[a-z]+@[a-z]+\.[a-z]{2,}`, "MatchEmail", codegen.MatchFull, false, ""},
	{"gen_full_hexcolor.go", `#[0-9a-f]{6}`, "MatchHexColor", codegen.MatchFull, false, ""},

	// Full mode — integration-only patterns
	{"gen_full_a_star.go", "a*", "MatchAStar", codegen.MatchFull, false, ""},
	{"gen_full_a_plus.go", "a+", "MatchAPlus", codegen.MatchFull, false, ""},
	{"gen_full_a_question.go", "a?", "MatchAQuestion", codegen.MatchFull, false, ""},
	{"gen_full_a_or_b.go", "a|b", "MatchAOrB", codegen.MatchFull, false, ""},
	{"gen_full_alt_star_c.go", "(a|b)*c", "MatchAltStarC", codegen.MatchFull, false, ""},
	{"gen_full_foobarbaz.go", "(foo|bar)baz", "MatchFooBarBaz", codegen.MatchFull, false, ""},
	{"gen_full_dot.go", ".", "MatchDot", codegen.MatchFull, false, ""},
	{"gen_full_casei_abc.go", "(?i)abc", "MatchCaseIAbc", codegen.MatchFull, false, ""},
	{"gen_full_container.go", `[a-z0-9][a-z0-9._-]{0,127}`, "MatchContainer", codegen.MatchFull, false, ""},

	// Prefix mode
	{"gen_prefix_charclass.go", "[a-z]+", "MatchPrefixCharClass", codegen.MatchPrefix, false, ""},
	{"gen_prefix_digitdash.go", `\d{3}-\d{2}`, "MatchPrefixDigitDash", codegen.MatchPrefix, false, ""},
	{"gen_prefix_aplusb.go", "a+b", "MatchPrefixAPlusB", codegen.MatchPrefix, false, ""},
	// Regression: a prefix match that passes THROUGH an accepting state
	// ("a" accepts, then the DFA keeps going for the optional "bc").
	{"gen_prefix_optional.go", "a(bc)?", "MatchPrefixOptional", codegen.MatchPrefix, false, ""},
	{"gen_prefix_astar.go", "a*", "MatchPrefixAStar", codegen.MatchPrefix, false, ""},

	// Contains mode
	{"gen_contains_charclass.go", "[a-z]+", "MatchContainsCharClass", codegen.MatchContains, false, ""},
	{"gen_contains_ssn.go", `\d{3}-\d{2}-\d{4}`, "MatchContainsSSN", codegen.MatchContains, false, ""},
	{"gen_contains_error.go", "error", "MatchContainsError", codegen.MatchContains, false, ""},
	// Regression: self-overlapping literal — the search DFA must track a match
	// attempt that starts INSIDE a failed earlier attempt ("aaab" contains "aab").
	// (A complete literal compiles to strings.Contains; this guards that path.)
	{"gen_contains_overlap.go", "aab", "MatchContainsOverlap", codegen.MatchContains, false, ""},
	// Same overlap shape through the DFA scan loop (non-literal pattern).
	{"gen_contains_overlap_class.go", "aa[bc]", "MatchContainsOverlapClass", codegen.MatchContains, false, ""},
	// Regression: unbounded backtracking shape that made the old
	// restart-at-every-position loop O(n^2) on all-'a' inputs.
	{"gen_contains_astarb.go", "a*b", "MatchContainsAStarB", codegen.MatchContains, false, ""},
	// Unicode contains: exercises the rune-loop search DFA.
	{"gen_contains_unicode.go", `[\x{00C0}-\x{00FF}]+`, "MatchContainsUnicode", codegen.MatchContains, false, ""},

	// Submatch
	{"gen_sub_email.go", `([a-z]+)@([a-z]+)`, "MatchSubEmail", codegen.MatchFull, true, "FindSubEmail"},
	{"gen_sub_ssn.go", `(\d{3})-(\d{2})-(\d{4})`, "MatchSubSSN", codegen.MatchFull, true, "FindSubSSN"},
	{"gen_sub_ab.go", `(a+)(b+)`, "MatchSubAB", codegen.MatchFull, true, "FindSubAB"},
	{"gen_sub_foobarbaz.go", `(foo|bar)baz`, "MatchSubFooBarBaz", codegen.MatchFull, true, "FindSubFooBarBaz"},
	{"gen_sub_dotted.go", `([a-z]+)(\.[a-z]+)*`, "MatchSubDotted", codegen.MatchFull, true, "FindSubDotted"},
}

func main() {
	for _, f := range fixtures {
		if err := generate(f); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f.file, err)
			os.Exit(1)
		}
	}
	for _, f := range namedFixtures {
		if err := generateNamed(f); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f.file, err)
			os.Exit(1)
		}
	}
}

func generateNamed(f namedFixture) error {
	result, err := parser.ParseResult(f.regex)
	if err != nil {
		return fmt.Errorf("parse %q: %w", f.regex, err)
	}
	if err := dfa.ValidateAssertions(result.Prog, true, true); err != nil {
		return fmt.Errorf("assertions %q: %w", f.regex, err)
	}
	d, err := dfa.Build(result.Prog)
	if err != nil {
		return fmt.Errorf("dfa %q: %w", f.regex, err)
	}
	sub := &codegen.SubmatchOptions{
		PackageName:   "e2e",
		FuncName:      f.subFunc,
		MatchFunc:     f.funcName,
		Regex:         f.regex,
		Prog:          result.Prog,
		NumGroups:     result.NumGroups,
		GroupNames:    result.GroupNames,
		NamesFuncName: f.namesFunc,
	}
	if f.structType != "" {
		sub.StructEnabled = true
		sub.StructType = f.structType
		sub.StructFunc = f.structFunc
	}
	var buf bytes.Buffer
	opts := codegen.Options{
		PackageName: "e2e",
		FuncName:    f.funcName,
		Regex:       f.regex,
		Mode:        codegen.MatchFull,
		Submatch:    sub,
	}
	if err := codegen.Generate(&buf, d, opts); err != nil {
		return fmt.Errorf("codegen %q: %w", f.regex, err)
	}
	return os.WriteFile(f.file, buf.Bytes(), 0644)
}

func generate(f fixture) error {
	if f.submatch {
		return generateSubmatch(f)
	}
	return generateMatch(f)
}

func generateMatch(f fixture) error {
	prog, err := parser.Parse(f.regex)
	if err != nil {
		return fmt.Errorf("parse %q: %w", f.regex, err)
	}
	anchorStart, anchorEnd := true, true
	switch f.mode {
	case codegen.MatchPrefix:
		anchorEnd = false
	case codegen.MatchContains:
		anchorStart, anchorEnd = false, false
	}
	if err := dfa.ValidateAssertions(prog, anchorStart, anchorEnd); err != nil {
		return fmt.Errorf("assertions %q: %w", f.regex, err)
	}
	build := dfa.Build
	if f.mode == codegen.MatchContains {
		build = dfa.BuildSearch
	}
	d, err := build(prog)
	if err != nil {
		return fmt.Errorf("dfa %q: %w", f.regex, err)
	}
	var buf bytes.Buffer
	opts := codegen.Options{
		PackageName: "e2e",
		FuncName:    f.funcName,
		Regex:       f.regex,
		Mode:        f.mode,
	}
	opts.LiteralPrefix, opts.LiteralComplete = prog.Prefix()
	err = codegen.Generate(&buf, d, opts)
	if err != nil {
		return fmt.Errorf("codegen %q: %w", f.regex, err)
	}
	return os.WriteFile(f.file, buf.Bytes(), 0644)
}

func generateSubmatch(f fixture) error {
	result, err := parser.ParseResult(f.regex)
	if err != nil {
		return fmt.Errorf("parse %q: %w", f.regex, err)
	}
	if err := dfa.ValidateAssertions(result.Prog, true, true); err != nil {
		return fmt.Errorf("assertions %q: %w", f.regex, err)
	}
	d, err := dfa.Build(result.Prog)
	if err != nil {
		return fmt.Errorf("dfa %q: %w", f.regex, err)
	}
	var buf bytes.Buffer
	opts := codegen.Options{
		PackageName: "e2e",
		FuncName:    f.funcName,
		Regex:       f.regex,
		Mode:        codegen.MatchFull,
	}
	if result.NumGroups > 0 {
		opts.Submatch = &codegen.SubmatchOptions{
			PackageName: "e2e",
			FuncName:    f.subFunc,
			MatchFunc:   f.funcName,
			Regex:       f.regex,
			Prog:        result.Prog,
			NumGroups:   result.NumGroups,
			GroupNames:  result.GroupNames,
			// Unique per-fixture names accessor to avoid SubexpNames collisions
			// across fixtures sharing the e2e package.
			NamesFuncName: f.subFunc + "Names",
		}
	}
	err = codegen.Generate(&buf, d, opts)
	if err != nil {
		return fmt.Errorf("codegen %q: %w", f.regex, err)
	}
	return os.WriteFile(f.file, buf.Bytes(), 0644)
}
