package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"regexp/syntax"
	"strconv"
	"strings"

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
	// Case-folded submatch regressions: the compiled matcher must honor the
	{"gen_sub_casei_a.go", `(?i)(a)bc`, "MatchCaseIA", "FindCaseIA", "NamesCaseIA", "", ""},
	{"gen_sub_casei_hello.go", `(?i)(hello)`, "MatchCaseIHello2", "FindCaseIHello2", "NamesCaseIHello2", "", ""},
	{"gen_sub_casei_k.go", `(?i)(k)x`, "MatchCaseIK", "FindCaseIK", "NamesCaseIK", "", ""},
	// Chain-compression re-entry cascades into submatch via the bool gate.
	{"gen_sub_chain.go", `a{3}(ba{3})*`, "MatchChainSub", "FindChainSub", "NamesChainSub", "", ""},

	// Ambiguous-capture fixtures: the -pass path rejects these (live
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

	// Interior always-true \B fixtures: dfa.ValidateAssertions proves the \B
	{"gen_negwb_ab.go", `(a\Bb)`, "MatchNegWBab", "FindNegWBab", "NamesNegWBab", "", ""},
	{"gen_negwb_foobar.go", `(foo\Bbar)`, "MatchNegWBFoobar", "FindNegWBFoobar", "NamesNegWBFoobar", "", ""},
	{"gen_negwb_foo_bar.go", `(foo\B)(bar)`, "MatchNegWBFooBar", "FindNegWBFooBar", "NamesNegWBFooBar", "", ""},
	{"gen_negwb_foo_bar2.go", `(foo)(\Bbar)`, "MatchNegWBFooBar2", "FindNegWBFooBar2", "NamesNegWBFooBar2", "", ""},
	{"gen_negwb_words.go", `(\w+\B\w+)`, "MatchNegWBWords", "FindNegWBWords", "NamesNegWBWords", "", ""},
	{"gen_negwb_two_words.go", `(\w+)\B(\w+)`, "MatchNegWBTwoWords", "FindNegWBTwoWords", "NamesNegWBTwoWords", "", ""},
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
	{"gen_full_chainreentry.go", `a{3}(?:ba{3})*`, "MatchChainReentry", codegen.MatchFull, false, ""},
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
	// Regression: a shorter alternative must stay latched when a longer
	{"gen_prefix_alt.go", "a|abc", "MatchPrefixAlt", codegen.MatchPrefix, false, ""},
	// Regression: a prefix match that passes THROUGH an accepting state
	{"gen_prefix_optional.go", "a(bc)?", "MatchPrefixOptional", codegen.MatchPrefix, false, ""},
	{"gen_prefix_astar.go", "a*", "MatchPrefixAStar", codegen.MatchPrefix, false, ""},

	// A trailing (?m)$ in full mode is a provable no-op (nothing can follow
	{"gen_full_mline_adollar.go", `(?m)a$`, "MatchMLineADollar", codegen.MatchFull, false, ""},

	// Contains mode
	{"gen_contains_charclass.go", "[a-z]+", "MatchContainsCharClass", codegen.MatchContains, false, ""},
	{"gen_contains_ssn.go", `\d{3}-\d{2}-\d{4}`, "MatchContainsSSN", codegen.MatchContains, false, ""},
	{"gen_contains_error.go", "error", "MatchContainsError", codegen.MatchContains, false, ""},
	// Regression: self-overlapping literal — the search DFA must track a match
	{"gen_contains_overlap.go", "aab", "MatchContainsOverlap", codegen.MatchContains, false, ""},
	// Same overlap shape through the DFA scan loop (non-literal pattern).
	{"gen_contains_overlap_class.go", "aa[bc]", "MatchContainsOverlapClass", codegen.MatchContains, false, ""},
	// Regression: the search DFA's restart default re-enters the
	{"gen_contains_chainrestart.go", "aaa[bc]", "MatchContainsChainRestart", codegen.MatchContains, false, ""},
	// Regression: unbounded backtracking shape that made the old
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
	if err := generateFuzzCorpus(); err != nil {
		fmt.Fprintf(os.Stderr, "gen_fuzz_corpus_test.go: %v\n", err)
		os.Exit(1)
	}
}

// fuzzDirected are patterns targeting previously-broken generator classes:
var fuzzDirected = []string{
	// Prefix latch (audit B).
	`a|abc`, `\D{3,5}`, `a(bc)?`,
	// Chain-compression re-entry (audit C); the capturing variant also
	`a{3}(?:ba{3})*`, `a{3}(ba{3})*`, `aaa[bc]`,
	// Empty-width assertions (audit A). Most (pattern, mode) combos are
	`a\bb`, `a$b`, `\b\s`, `^a`, `\bfoo\b`, `$`, `^`, `(?m)a$`, `(?m)^b`,
	`\Bx`, `a\b`, `\bword\b`, `^a$`, `\A[ab]+\z`, `(?m)^$`, `foo\b`, `a(?:\b)+b`,
	// (?i) submatch fold orbits (audit D; these have capture groups, so the
	`(?i)(a)bc`, `(?i)(hello)`, `(?i)(k)x`,
	// Contains-mode empty-match short-circuits (audit E).
	`\D?`, `x*`, `(a|b)*`, `a?b?`,
	// Contains fast paths: complete literals (ASCII and Unicode; the latter
	`error`, `héllo`, `e[0-9]+`,
	// General parity, including invalid-UTF- handling (matched as U+FFFD,
	`(?i)abc`, `[^a]`, `a.{0,2}b`, `(a+)(b+)`, `(?s).`, `.+`,
	`[a-z0-9][a-z0-9._-]{0,127}`,
}

const (
	fuzzRandomCount = 50
	fuzzSeed        = 20260702 // deterministic: same corpus on every generate
	fuzzMaxStates   = 300      // keep the generated file small
)

// randFuzzPattern builds a random pattern from a constrained grammar covering
func randFuzzPattern(r *rand.Rand, depth int) string {
	atoms := []string{
		"a", "b", "c", "x", "0", "1", "_", " ", `\n`,
		"[a-c]", "[^a]", "[0-9]", `\d`, `\w`, `\s`, `\D`, `\W`, `\S`, ".",
		"[a-cx-z0-3]", "K", "k",
	}
	quant := []string{"", "", "", "?", "*", "+", "{2}", "{1,3}", "{2,}", "??", "*?", "+?"}
	var b strings.Builder
	n := 1 + r.Intn(4)
	for i := 0; i < n; i++ {
		var atom string
		switch {
		case depth > 0 && r.Intn(5) == 0:
			inner := randFuzzPattern(r, depth-1)
			switch r.Intn(3) {
			case 0:
				atom = "(" + inner + ")"
			case 1:
				atom = "(?:" + inner + ")"
			default:
				atom = "(?i:" + inner + ")"
			}
		case depth > 0 && r.Intn(7) == 0:
			atom = "(?:" + randFuzzPattern(r, depth-1) + "|" + randFuzzPattern(r, depth-1) + ")"
		default:
			atom = atoms[r.Intn(len(atoms))]
		}
		b.WriteString(atom)
		b.WriteString(quant[r.Intn(len(quant))])
	}
	p := b.String()
	switch r.Intn(8) {
	case 0:
		p = "^" + p
	case 1:
		p = p + "$"
	case 2:
		p = `\b` + p
	case 3:
		p = p + `\b`
	case 4:
		p = "(?m)" + p
	}
	return p
}

// fuzzModes mirrors the CLI's mode table: template mode, function suffix,
var fuzzModes = []struct {
	mode                   codegen.MatchMode
	suffix, tag            string
	anchorStart, anchorEnd bool
}{
	{codegen.MatchFull, "F", "full", true, true},
	{codegen.MatchPrefix, "P", "prefix", true, false},
	{codegen.MatchContains, "C", "contains", false, false},
}

// fuzzComboUsable reports whether a (pattern, mode) combination enters the
func fuzzComboUsable(prog *syntax.Prog, mode codegen.MatchMode, anchorStart, anchorEnd bool) bool {
	if dfa.ValidateAssertions(prog, anchorStart, anchorEnd) != nil {
		return false
	}
	build := dfa.Build
	if mode == codegen.MatchContains {
		build = dfa.BuildSearch
	}
	d, err := build(prog)
	return err == nil && len(d.States) <= fuzzMaxStates
}

// generateFuzzCorpus emits gen_fuzz_corpus_test.go: bool matcher per
func generateFuzzCorpus() error {
	patterns := append([]string{}, fuzzDirected...)
	r := rand.New(rand.NewSource(fuzzSeed))
	for len(patterns) < len(fuzzDirected)+fuzzRandomCount {
		p := randFuzzPattern(r, 2)
		res, err := parser.ParseResult(p)
		if err != nil {
			continue
		}
		d, err := dfa.Build(res.Prog)
		if err != nil || len(d.States) > fuzzMaxStates {
			continue
		}
		patterns = append(patterns, p)
	}

	var bodies bytes.Buffer
	var boolEntries, subEntries []string
	needMatch, needUTF8, needStrings := false, false, false

	for i, p := range patterns {
		for _, m := range fuzzModes {
			res, err := parser.ParseResult(p)
			if err != nil {
				return fmt.Errorf("parse %q: %w", p, err)
			}
			if !fuzzComboUsable(res.Prog, m.mode, m.anchorStart, m.anchorEnd) {
				continue // rejected assertions or state blowup: no matcher exists
			}
			build := dfa.Build
			if m.mode == codegen.MatchContains {
				build = dfa.BuildSearch
			}
			d, err := build(res.Prog)
			if err != nil {
				return fmt.Errorf("dfa %q mode=%s: %w", p, m.tag, err)
			}
			fn := fmt.Sprintf("fuzzMatch%d%s", i, m.suffix)
			opts := codegen.Options{
				PackageName: "e2e",
				FuncName:    fn,
				Regex:       p,
				Mode:        m.mode,
			}
			opts.LiteralPrefix, opts.LiteralComplete = res.Prog.Prefix()
			withSubmatch := m.mode == codegen.MatchFull && res.NumGroups > 0
			if withSubmatch {
				opts.Submatch = &codegen.SubmatchOptions{
					PackageName:   "e2e",
					FuncName:      fmt.Sprintf("fuzzFind%d", i),
					MatchFunc:     fn,
					Regex:         p,
					Prog:          res.Prog,
					NumGroups:     res.NumGroups,
					GroupNames:    res.GroupNames,
					NamesFuncName: fmt.Sprintf("fuzzNames%d", i),
				}
			}
			var buf bytes.Buffer
			err = codegen.Generate(&buf, d, opts)
			if err != nil && withSubmatch {
				// Submatch always compiles to a state machine (-pass or
				withSubmatch = false
				opts.Submatch = nil
				buf.Reset()
				err = codegen.Generate(&buf, d, opts)
			}
			if err != nil {
				return fmt.Errorf("codegen %q mode=%s: %w", p, m.tag, err)
			}
			if withSubmatch {
				subEntries = append(subEntries,
					fmt.Sprintf("\t{Pattern: %s, IndexFn: fuzzFind%dIndex},", strconv.Quote(p), i))
			}
			body, imports, err := splitGenerated(buf.String())
			if err != nil {
				return fmt.Errorf("split %q mode=%s: %w", p, m.tag, err)
			}
			for _, imp := range imports {
				switch imp {
				case `"github.com/wow-look-at-my/go-regex-compiler/match"`:
					needMatch = true
				case `"unicode/utf8"`:
					needUTF8 = true
				case `"strings"`:
					needStrings = true
				default:
					return fmt.Errorf("unexpected import %s for %q", imp, p)
				}
			}
			bodies.WriteString(body)
			boolEntries = append(boolEntries,
				fmt.Sprintf("\t{Pattern: %s, Mode: %q, Fn: %s},", strconv.Quote(p), m.tag, fn))
		}
	}

	var out bytes.Buffer
	out.WriteString("// Code generated by go-regex-compiler fixtures. DO NOT EDIT.\n")
	out.WriteString("//\n// Differential fuzz corpus: deterministic matchers compared against stdlib\n")
	out.WriteString("// regexp by fuzz_diff_test.go. Regenerate with `go generate ./e2e/...`.\n\n")
	out.WriteString("package e2e\n\n")
	if needMatch {
		out.WriteString("import \"github.com/wow-look-at-my/go-regex-compiler/match\"\n")
	}
	if needStrings {
		out.WriteString("import \"strings\"\n")
	}
	if needUTF8 {
		out.WriteString("import \"unicode/utf8\"\n")
	}
	out.WriteString("\ntype fuzzBoolCase struct {\n\tPattern string\n\tMode    string // \"full\", \"prefix\", \"contains\"\n\tFn      func(string) bool\n}\n\n")
	out.WriteString("type fuzzSubCase struct {\n\tPattern string\n\tIndexFn func(string) []int\n}\n\n")
	fmt.Fprintf(&out, "var fuzzPatterns = %#v\n\n", patterns)
	fmt.Fprintf(&out, "var fuzzCorpus = []fuzzBoolCase{\n%s\n}\n\n", strings.Join(boolEntries, "\n"))
	fmt.Fprintf(&out, "var fuzzSubCorpus = []fuzzSubCase{\n%s\n}\n", strings.Join(subEntries, "\n"))
	out.WriteString(bodies.String())

	return os.WriteFile("gen_fuzz_corpus_test.go", out.Bytes(), 0644)
}

// splitGenerated strips the per-file header from codegen.Generate output,
func splitGenerated(src string) (body string, imports []string, err error) {
	idx := strings.Index(src, "\npackage e2e\n")
	if idx < 0 {
		return "", nil, fmt.Errorf("no package clause found")
	}
	rest := src[idx+len("\npackage e2e\n"):]
	var b strings.Builder
	for _, line := range strings.SplitAfter(rest, "\n") {
		if strings.HasPrefix(line, "import ") {
			imports = append(imports, strings.TrimSpace(strings.TrimPrefix(line, "import ")))
			continue
		}
		b.WriteString(line)
	}
	return b.String(), imports, nil
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
			NamesFuncName: f.subFunc + "Names",
		}
	}
	err = codegen.Generate(&buf, d, opts)
	if err != nil {
		return fmt.Errorf("codegen %q: %w", f.regex, err)
	}
	return os.WriteFile(f.file, buf.Bytes(), 0644)
}
