package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
)

var fixtures = []struct {
	file     string
	regex    string
	funcName string
	mode     codegen.MatchMode
}{
	{"gen_literal.go", "abc", "MatchLiteral", codegen.MatchFull},
	{"gen_charclass.go", "[a-z]+", "MatchCharClass", codegen.MatchFull},
	{"gen_ssn.go", `\d{3}-\d{2}-\d{4}`, "MatchSSN", codegen.MatchFull},
	{"gen_email.go", `[a-z]+@[a-z]+\.[a-z]{2,}`, "MatchEmail", codegen.MatchFull},
	{"gen_identifier.go", `[A-Za-z_][A-Za-z0-9_]*`, "MatchIdentifier", codegen.MatchFull},
	{"gen_url.go", `(https?://)?[a-z]+\.[a-z]{2,}`, "MatchURL", codegen.MatchFull},
	{"gen_casei.go", "(?i)hello", "MatchCaseInsensitive", codegen.MatchFull},
	{"gen_hexcolor.go", `#[0-9a-f]{6}`, "MatchHexColor", codegen.MatchFull},
	{"gen_contains_literal.go", "error", "ContainsError", codegen.MatchContains},
	{"gen_contains_ssn.go", `\d{3}-\d{2}-\d{4}`, "ContainsSSN", codegen.MatchContains},
	{"gen_contains_astarb.go", "a*b", "ContainsAStarB", codegen.MatchContains},
}

func main() {
	for _, f := range fixtures {
		prog, err := parser.Parse(f.regex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse %q: %v\n", f.regex, err)
			os.Exit(1)
		}
		anchorStart, anchorEnd := true, true
		if f.mode == codegen.MatchContains {
			anchorStart, anchorEnd = false, false
		}
		if err := dfa.ValidateAssertions(prog, anchorStart, anchorEnd); err != nil {
			fmt.Fprintf(os.Stderr, "assertions %q: %v\n", f.regex, err)
			os.Exit(1)
		}
		build := dfa.Build
		if f.mode == codegen.MatchContains {
			build = dfa.BuildSearch
		}
		d, err := build(prog)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dfa %q: %v\n", f.regex, err)
			os.Exit(1)
		}
		var buf bytes.Buffer
		opts := codegen.Options{
			PackageName: "bench",
			FuncName:    f.funcName,
			Regex:       f.regex,
			Mode:        f.mode,
		}
		opts.LiteralPrefix, opts.LiteralComplete = prog.Prefix()
		err = codegen.Generate(&buf, d, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "codegen %q: %v\n", f.regex, err)
			os.Exit(1)
		}
		if err := os.WriteFile(f.file, buf.Bytes(), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", f.file, err)
			os.Exit(1)
		}
	}
}
