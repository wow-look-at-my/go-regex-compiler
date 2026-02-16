package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
)

func main() {
	regex := flag.String("regex", "", "regular expression to compile (required)")
	pkg := flag.String("package", "", "package name for generated code (default: $GOPACKAGE or \"main\")")
	funcName := flag.String("func", "Match", "name of the generated match function")
	output := flag.String("output", "", "output file path (default: stdout)")
	matchMode := flag.String("match", "full", "match mode: full (entire string), prefix (start of string), contains (any substring)")
	submatch := flag.Bool("submatch", false, "also generate a FindSubmatch function for capture group extraction")
	submatchFunc := flag.String("submatch-func", "FindSubmatch", "name of the generated submatch function")
	flag.Parse()

	if err := run(*regex, *pkg, *funcName, *output, *matchMode, *submatch, *submatchFunc); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(regex, pkg, funcName, output, matchMode string, submatch bool, submatchFunc string) error {
	if regex == "" {
		return fmt.Errorf("-regex is required")
	}

	if pkg == "" {
		if envPkg := os.Getenv("GOPACKAGE"); envPkg != "" {
			pkg = envPkg
		} else {
			pkg = "main"
		}
	}

	var mode codegen.MatchMode
	switch matchMode {
	case "full":
		mode = codegen.MatchFull
	case "prefix":
		mode = codegen.MatchPrefix
	case "contains":
		mode = codegen.MatchContains
	default:
		return fmt.Errorf("invalid -match mode %q: must be full, prefix, or contains", matchMode)
	}

	// Stage 1: Parse regex into NFA (with capture group info)
	result, err := parser.ParseResult(regex)
	if err != nil {
		return err
	}

	// Stage 2: Build DFA from NFA
	d, err := dfa.Build(result.Prog)
	if err != nil {
		return err
	}

	// Stage 3: Generate Go code
	opts := codegen.Options{
		PackageName: pkg,
		FuncName:    funcName,
		Regex:       regex,
		Mode:        mode,
	}

	if submatch && result.NumGroups > 0 {
		opts.Submatch = &codegen.SubmatchOptions{
			PackageName: pkg,
			FuncName:    submatchFunc,
			MatchFunc:   funcName,
			Regex:       regex,
			Prog:        result.Prog,
			NumGroups:   result.NumGroups,
		}
	}

	var w io.Writer = os.Stdout
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	return codegen.Generate(w, d, opts)
}
