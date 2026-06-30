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

var fixtures = []struct {
	file     string
	regex    string
	funcName string
}{
	{"gen_literal.go", "abc", "MatchLiteral"},
	{"gen_charclass.go", "[a-z]+", "MatchCharClass"},
	{"gen_ssn.go", `\d{3}-\d{2}-\d{4}`, "MatchSSN"},
	{"gen_email.go", `[a-z]+@[a-z]+\.[a-z]{2,}`, "MatchEmail"},
	{"gen_identifier.go", `[A-Za-z_][A-Za-z0-9_]*`, "MatchIdentifier"},
	{"gen_url.go", `(https?://)?[a-z]+\.[a-z]{2,}`, "MatchURL"},
	{"gen_casei.go", "(?i)hello", "MatchCaseInsensitive"},
	{"gen_hexcolor.go", `#[0-9a-f]{6}`, "MatchHexColor"},
}

func main() {
	for _, f := range fixtures {
		prog, err := parser.Parse(f.regex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse %q: %v\n", f.regex, err)
			os.Exit(1)
		}
		d, err := dfa.Build(prog)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dfa %q: %v\n", f.regex, err)
			os.Exit(1)
		}
		var buf bytes.Buffer
		err = codegen.Generate(&buf, d, codegen.Options{
			PackageName: "bench",
			FuncName:    f.funcName,
			Regex:       f.regex,
		})
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
