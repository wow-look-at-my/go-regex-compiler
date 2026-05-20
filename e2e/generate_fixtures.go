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

	// Contains mode
	{"gen_contains_charclass.go", "[a-z]+", "MatchContainsCharClass", codegen.MatchContains, false, ""},
	{"gen_contains_ssn.go", `\d{3}-\d{2}-\d{4}`, "MatchContainsSSN", codegen.MatchContains, false, ""},
	{"gen_contains_error.go", "error", "MatchContainsError", codegen.MatchContains, false, ""},

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
	d, err := dfa.Build(prog)
	if err != nil {
		return fmt.Errorf("dfa %q: %w", f.regex, err)
	}
	var buf bytes.Buffer
	err = codegen.Generate(&buf, d, codegen.Options{
		PackageName: "e2e",
		FuncName:    f.funcName,
		Regex:       f.regex,
		Mode:        f.mode,
	})
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
		}
	}
	err = codegen.Generate(&buf, d, opts)
	if err != nil {
		return fmt.Errorf("codegen %q: %w", f.regex, err)
	}
	return os.WriteFile(f.file, buf.Bytes(), 0644)
}
