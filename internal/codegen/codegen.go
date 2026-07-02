package codegen

import (
	"bytes"
	"fmt"
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
	PackageName string           // Go package name
	FuncName    string           // Name of the generated match function
	Regex       string           // Original regex (for comment)
	Mode        MatchMode        // Match mode (default: MatchFull)
	Submatch    *SubmatchOptions // If non-nil, also generate a FindSubmatch function
}

// templateContext holds all data needed by the top-level templates.
type templateContext struct {
	PackageName        string
	FuncName           string
	Regex              string
	ModeComment        string
	Mode               string // "full", "prefix", "contains"
	ASCII              bool
	Start              int
	States             []templateState
	AcceptIDs          []int
	EdgeCase           bool // single accepting state with no transitions
	EdgeCaseAlwaysTrue bool // edge case AND mode is prefix/contains
	StartAccepts       bool // start state is accepting (for contains early-return)
	NumChains          int
	HasRanges          bool
}

// templateState mirrors dfa.State for use in templates.
type templateState struct {
	ID            int
	Accept        bool
	Transitions   []templateTransition
	IsChain       bool
	ChainMaxCount int
	ChainTerminal int
	ChainIndex    int
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

	_, werr := w.Write(buf.Bytes())
	return werr
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

	compressChains(&ctx)

	for _, s := range ctx.States {
		for _, t := range s.Transitions {
			if t.Lo != t.Hi {
				ctx.HasRanges = true
				break
			}
		}
		if ctx.HasRanges {
			break
		}
	}

	return ctx
}

func transitionShape(s templateState) string {
	if len(s.Transitions) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for i, t := range s.Transitions {
		if i > 0 {
			buf.WriteByte(';')
		}
		fmt.Fprintf(&buf, "%d-%d", t.Lo, t.Hi)
	}
	return buf.String()
}

func compressChains(ctx *templateContext) {
	const minChainLength = 3

	stateByID := make(map[int]*templateState)
	for i := range ctx.States {
		stateByID[ctx.States[i].ID] = &ctx.States[i]
	}

	incomingSources := make(map[int]map[int]bool)
	for _, s := range ctx.States {
		for _, t := range s.Transitions {
			if incomingSources[t.Next] == nil {
				incomingSources[t.Next] = make(map[int]bool)
			}
			incomingSources[t.Next][s.ID] = true
		}
	}

	visited := make(map[int]bool)
	type chainInfo struct {
		stateIDs   []int
		terminalID int
		chainIndex int
	}
	var chains []chainInfo
	chainIdx := 0

	for _, s := range ctx.States {
		if visited[s.ID] {
			continue
		}

		shape := transitionShape(s)
		if shape == "" {
			continue
		}

		target := s.Transitions[0].Next
		uniform := true
		for _, t := range s.Transitions[1:] {
			if t.Next != target {
				uniform = false
				break
			}
		}
		if !uniform {
			continue
		}

		chain := []int{s.ID}
		chainSet := map[int]bool{s.ID: true}
		current := target

		for {
			if chainSet[current] {
				break
			}
			cs := stateByID[current]
			if cs == nil {
				break
			}
			if cs.Accept != s.Accept {
				break
			}
			if transitionShape(*cs) != shape {
				break
			}
			nextTarget := cs.Transitions[0].Next
			nextUniform := true
			for _, t := range cs.Transitions[1:] {
				if t.Next != nextTarget {
					nextUniform = false
					break
				}
			}
			if !nextUniform {
				break
			}
			sources := incomingSources[current]
			if len(sources) != 1 || !sources[chain[len(chain)-1]] {
				break
			}
			chain = append(chain, current)
			chainSet[current] = true
			current = nextTarget
		}

		if len(chain) >= minChainLength {
			for _, id := range chain {
				visited[id] = true
			}
			chains = append(chains, chainInfo{
				stateIDs:   chain,
				terminalID: current,
				chainIndex: chainIdx,
			})
			chainIdx++
		}
	}

	if len(chains) == 0 {
		return
	}

	chainMembers := make(map[int]bool)
	for _, c := range chains {
		for i := range ctx.States {
			if ctx.States[i].ID == c.stateIDs[0] {
				ctx.States[i].IsChain = true
				ctx.States[i].ChainMaxCount = len(c.stateIDs) - 1
				ctx.States[i].ChainTerminal = c.terminalID
				ctx.States[i].ChainIndex = c.chainIndex
				break
			}
		}
		for _, id := range c.stateIDs[1:] {
			chainMembers[id] = true
		}
	}

	var filtered []templateState
	for _, s := range ctx.States {
		if !chainMembers[s.ID] {
			filtered = append(filtered, s)
		}
	}
	ctx.States = filtered

	var newAcceptIDs []int
	for _, id := range ctx.AcceptIDs {
		if !chainMembers[id] {
			newAcceptIDs = append(newAcceptIDs, id)
		}
	}
	ctx.AcceptIDs = newAcceptIDs

	ctx.NumChains = len(chains)
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

// quoteRegex renders the source pattern for use inside a // line comment.
// The readable backtick form is used when safe; a pattern containing control
// characters (a literal newline would terminate the comment and split the
// generated file, and NUL is illegal in Go source) falls back to a
// double-quoted, escaped, single-line Go string literal.
func quoteRegex(s string) string {
	for _, r := range s {
		if r < ' ' || r == 0x7f {
			return strconv.Quote(s)
		}
	}
	return "`" + s + "`"
}

func stateTransition(s templateState, t templateTransition) string {
	if s.IsChain {
		return fmt.Sprintf("if chainCount%d >= %d { state = %d } else { chainCount%d++ }",
			s.ChainIndex, s.ChainMaxCount, s.ChainTerminal, s.ChainIndex)
	}
	return fmt.Sprintf("state = %d", t.Next)
}

type groupedCase struct {
	Cond string
	Body string
}

func groupByteTransitions(s templateState) []groupedCase {
	return groupTransitions(s, func(t templateTransition) string {
		if t.Lo == t.Hi {
			return fmt.Sprintf("c == %s", quoteByte(t.Lo))
		}
		return fmt.Sprintf("match.InRange(c, %s, %s)", quoteByte(t.Lo), quoteByte(t.Hi))
	})
}

func groupRuneTransitions(s templateState) []groupedCase {
	return groupTransitions(s, func(t templateTransition) string {
		if t.Lo == t.Hi {
			return fmt.Sprintf("r == %s", quoteRune(t.Lo))
		}
		return fmt.Sprintf("match.InRange(r, %s, %s)", quoteRune(t.Lo), quoteRune(t.Hi))
	})
}

func groupTransitions(s templateState, condFn func(templateTransition) string) []groupedCase {
	var groups []groupedCase
	i := 0
	for i < len(s.Transitions) {
		body := stateTransition(s, s.Transitions[i])
		conds := condFn(s.Transitions[i])
		j := i + 1
		for j < len(s.Transitions) && stateTransition(s, s.Transitions[j]) == body {
			conds += ", " + condFn(s.Transitions[j])
			j++
		}
		groups = append(groups, groupedCase{Cond: conds, Body: body})
		i = j
	}
	return groups
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
