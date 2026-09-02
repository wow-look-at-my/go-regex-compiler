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
	regex            string
	pkg              string
	funcName         string
	outputPath       string
	matchMode        string
	submatch         bool
	submatchFn       string
	submatchNamesFn  string
	submatchStruct   bool
	submatchStructTy string
	submatchStructFn string
)

func init() {
	rootCmd.Flags().StringVar(&regex, "regex", "", "regular expression to compile (required)")
	rootCmd.Flags().StringVar(&pkg, "package", "", `package name for generated code (default: $GOPACKAGE or "main")`)
	rootCmd.Flags().StringVar(&funcName, "func", "Match", "name of the generated match function")
	rootCmd.Flags().StringVar(&outputPath, "output", "", "output file path (default: stdout)")
	rootCmd.Flags().StringVar(&matchMode, "match", "full", "match mode: full (entire string), prefix (start of string), contains (any substring)")
	rootCmd.Flags().BoolVar(&submatch, "submatch", false, "also generate a FindSubmatch function (plus <func>Index and SubexpNames) for capture group extraction")
	rootCmd.Flags().StringVar(&submatchFn, "submatch-func", "FindSubmatch", "name of the generated positional submatch function (also generates <name>Index)")
	rootCmd.Flags().StringVar(&submatchNamesFn, "submatch-names-func", "SubexpNames", "name of the generated group-names accessor function")
	rootCmd.Flags().BoolVar(&submatchStruct, "submatch-struct", false, "also generate a typed capture struct (requires at least one named group)")
	rootCmd.Flags().StringVar(&submatchStructTy, "submatch-struct-type", "Captures", "name of the generated capture struct type")
	rootCmd.Flags().StringVar(&submatchStructFn, "submatch-struct-func", "FindCaptures", "name of the generated capture struct constructor function")
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

	if submatch && mode != codegen.MatchFull {
		// The generated submatch family extracts from a FULL-string match
		// (parity with regexp on an anchored pattern). Combining it with
		// prefix/contains produced a self-contradictory pair: Match(input)
		// could be true while FindSubmatch(input) returned nil.
		return fmt.Errorf("--submatch requires --match full: the submatch functions extract capture groups from a full-string match, which %s mode does not produce", matchMode)
	}

	// Stage 1: Parse regex into NFA (with capture group info)
	result, err := parser.ParseResult(regex)
	if err != nil {
		return err
	}
	if submatch && result.NumGroups == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "note: --submatch ignored: the regex has no capture groups")
	}

	// Reject empty-width assertions the DFA cannot honor in this match mode
	// (previously they were silently ignored, producing wrong matchers).
	anchorStart, anchorEnd := true, true
	switch mode {
	case codegen.MatchPrefix:
		anchorEnd = false
	case codegen.MatchContains:
		anchorStart, anchorEnd = false, false
	}
	// The bool matcher decides \b and \B from the state it carries, so only the
	// text anchors need proving here. --submatch keeps the strict check below,
	// because its compilers do walk an always-true assertion through.
	if err := dfa.ValidateAssertionsForBoolMatcher(result.Prog, anchorStart, anchorEnd); err != nil {
		return err
	}
	if submatch {
		if err := dfa.ValidateAssertions(result.Prog, anchorStart, anchorEnd); err != nil {
			return err
		}
	}

	// Stage 2: Build DFA from NFA. Contains mode uses the unanchored search
	// DFA so the generated matcher scans the input in a single pass.
	build := dfa.Build
	if mode == codegen.MatchContains {
		build = dfa.BuildSearch
	}
	d, err := build(result.Prog)
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
	opts.LiteralPrefix, opts.LiteralComplete = result.Prog.Prefix()

	if submatch && result.NumGroups > 0 {
		structEnabled := submatchStruct
		if submatchStruct && !codegen.HasNamedGroups(result.GroupNames) {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: --submatch-struct ignored: the regex has no named capture groups")
			structEnabled = false
		}
		opts.Submatch = &codegen.SubmatchOptions{
			PackageName:   pkg,
			FuncName:      submatchFn,
			MatchFunc:     funcName,
			Regex:         regex,
			Prog:          result.Prog,
			NumGroups:     result.NumGroups,
			GroupNames:    result.GroupNames,
			NamesFuncName: submatchNamesFn,
			StructEnabled: structEnabled,
			StructType:    submatchStructTy,
			StructFunc:    submatchStructFn,
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
