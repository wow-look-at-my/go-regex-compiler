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
	flag.Parse()

	if err := run(*regex, *pkg, *funcName, *output); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(regex, pkg, funcName, output string) error {
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

	// Stage 1: Parse regex into NFA
	prog, err := parser.Parse(regex)
	if err != nil {
		return err
	}

	// Stage 2: Build DFA from NFA
	d, err := dfa.Build(prog)
	if err != nil {
		return err
	}

	// Stage 3: Generate Go code
	opts := codegen.Options{
		PackageName: pkg,
		FuncName:    funcName,
		Regex:       regex,
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
