package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
)

var rootCmd = &cobra.Command{
	Use:          "go-regex-compiler",
	Short:        "Compile regular expressions into pure Go functions",
	SilenceUsage: true,
	RunE:         runGenerate,
}

var (
	regex      string
	pkg        string
	funcName   string
	outputPath string
	matchMode  string
	submatch   bool
	submatchFn string
)

func init() {
	rootCmd.Flags().StringVar(&regex, "regex", "", "regular expression to compile (required)")
	rootCmd.Flags().StringVar(&pkg, "package", "", `package name for generated code (default: $GOPACKAGE or "main")`)
	rootCmd.Flags().StringVar(&funcName, "func", "Match", "name of the generated match function")
	rootCmd.Flags().StringVar(&outputPath, "output", "", "output file path (default: stdout)")
	rootCmd.Flags().StringVar(&matchMode, "match", "full", "match mode: full (entire string), prefix (start of string), contains (any substring)")
	rootCmd.Flags().BoolVar(&submatch, "submatch", false, "also generate a FindSubmatch function for capture group extraction")
	rootCmd.Flags().StringVar(&submatchFn, "submatch-func", "FindSubmatch", "name of the generated submatch function")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runGenerate(cmd *cobra.Command, args []string) error {
	if regex == "" {
		return fmt.Errorf("--regex is required")
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
		return fmt.Errorf("invalid --match mode %q: must be full, prefix, or contains", matchMode)
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
			FuncName:    submatchFn,
			MatchFunc:   funcName,
			Regex:       regex,
			Prog:        result.Prog,
			NumGroups:   result.NumGroups,
		}
	}

	var w io.Writer = cmd.OutOrStdout()
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	return codegen.Generate(w, d, opts)
}
