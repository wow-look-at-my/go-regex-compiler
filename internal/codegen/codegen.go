package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"io"
	"strconv"
	"unicode"

	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
)

// MatchMode controls how the generated function matches input.
type MatchMode int

const (
	// MatchFull requires the entire input to match (default). Equivalent to ^pattern$.
	MatchFull MatchMode = iota
	// MatchPrefix matches if a prefix of the input matches the pattern. Equivalent to ^pattern.
	MatchPrefix
	// MatchContains matches if any substring of the input matches. Equivalent to unanchored pattern.
	MatchContains
)

// Options controls the generated code.
type Options struct {
	PackageName string         // Go package name
	FuncName    string         // Name of the generated match function
	Regex       string         // Original regex (for comment)
	Mode        MatchMode      // Match mode (default: MatchFull)
	Submatch    *SubmatchOptions // If non-nil, also generate a FindSubmatch function
}

// templateContext holds all data needed by the top-level templates.
type templateContext struct {
	PackageName string
	FuncName    string
	Regex       string
	ModeComment string
	Mode        string // "full", "prefix", "contains"
	ASCII       bool
	Start       int
	States      []templateState
	AcceptIDs   []int
	EdgeCase        bool // single accepting state with no transitions
	EdgeCaseAlwaysTrue bool // edge case AND mode is prefix/contains
	StartAccepts bool // start state is accepting (for contains early-return)
}

// templateState mirrors dfa.State for use in templates.
type templateState struct {
	ID          int
	Accept      bool
	Transitions []templateTransition
}

// templateTransition mirrors dfa.Transition for use in templates.
type templateTransition struct {
	Lo   rune
	Hi   rune
	Next int
}

// Generate writes Go source code implementing a DFA matcher to w.
func Generate(w io.Writer, d *dfa.DFA, opts Options) error {
	ctx := buildContext(d, opts)

	var buf bytes.Buffer

	if err := tmpl.ExecuteTemplate(&buf, "header", ctx); err != nil {
		return fmt.Errorf("executing header template: %w", err)
	}
	if err := tmpl.ExecuteTemplate(&buf, "matchFunc", ctx); err != nil {
		return fmt.Errorf("executing matchFunc template: %w", err)
	}

	if opts.Submatch != nil {
		subCtx := buildSubmatchContext(*opts.Submatch)
		if err := tmpl.ExecuteTemplate(&buf, "submatchFunc", subCtx); err != nil {
			return fmt.Errorf("executing submatchFunc template: %w", err)
		}
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// If formatting fails, write unformatted so user can debug
		_, werr := w.Write(buf.Bytes())
		if werr != nil {
			return werr
		}
		return fmt.Errorf("formatting generated code: %w", err)
	}

	_, err = w.Write(formatted)
	return err
}

func buildContext(d *dfa.DFA, opts Options) templateContext {
	ascii := isASCIIOnly(d)

	var modeStr string
	switch opts.Mode {
	case MatchPrefix:
		modeStr = "prefix"
	case MatchContains:
		modeStr = "contains"
	default:
		modeStr = "full"
	}

	ctx := templateContext{
		PackageName: opts.PackageName,
		FuncName:    opts.FuncName,
		Regex:       opts.Regex,
		ModeComment: matchModeComment(opts.Mode),
		Mode:        modeStr,
		ASCII:       ascii,
		Start:       d.Start,
	}

	for _, s := range d.States {
		ts := templateState{ID: s.ID, Accept: s.Accept}
		for _, tr := range s.Transitions {
			ts.Transitions = append(ts.Transitions, templateTransition{
				Lo: tr.Lo, Hi: tr.Hi, Next: tr.Next,
			})
		}
		ctx.States = append(ctx.States, ts)
		if s.Accept {
			ctx.AcceptIDs = append(ctx.AcceptIDs, s.ID)
		}
	}

	// Edge case: single accepting start state with no transitions (matches only empty string)
	if len(d.States) > 0 {
		startAccepts := d.States[d.Start].Accept
		ctx.StartAccepts = startAccepts
		if len(d.States) == 1 && startAccepts && len(d.States[0].Transitions) == 0 {
			ctx.EdgeCase = true
			ctx.EdgeCaseAlwaysTrue = (opts.Mode == MatchContains || opts.Mode == MatchPrefix)
		}
	}

	return ctx
}

func matchModeComment(mode MatchMode) string {
	switch mode {
	case MatchPrefix:
		return "reports whether a prefix of input matches"
	case MatchContains:
		return "reports whether any substring of input matches"
	default:
		return "reports whether input fully matches"
	}
}

func quoteRune(r rune) string {
	return strconv.QuoteRune(r)
}

func quoteByte(r rune) string {
	return strconv.QuoteRune(rune(byte(r)))
}

func quoteRegex(s string) string {
	return "`" + s + "`"
}

// isASCIIOnly returns true if all DFA transitions only involve runes <= 127.
func isASCIIOnly(d *dfa.DFA) bool {
	for _, state := range d.States {
		for _, tr := range state.Transitions {
			if tr.Hi > unicode.MaxASCII {
				return false
			}
		}
	}
	return true
}
