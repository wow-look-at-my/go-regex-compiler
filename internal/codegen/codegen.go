package codegen

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"unicode"

	"github.com/wow-look-at-my/go-containers/set"
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

	// LiteralPrefix/LiteralComplete mirror syntax.Prog.Prefix() for the
	// pattern: when LiteralComplete is true the pattern matches exactly the
	// literal LiteralPrefix, and contains mode compiles to a single
	// strings.Contains call instead of a DFA scan.
	LiteralPrefix   string
	LiteralComplete bool
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
	StartAccepts       bool // start state is accepting (for prefix/contains early-return)
	NumChains          int
	HasRanges          bool
	HasSubmatch        bool // a submatch family is generated

	// Import need-flags. buildContext seeds them with the bool matcher's needs,
	// gated on the rendered body actually containing a matching loop: the
	// short-circuit bodies (`return false` for an empty DFA, the empty-string
	// edge case, the prefix/contains early return when the start state accepts,
	// and the strings.Contains literal fast path) reference neither
	// match.InRange nor utf8.DecodeRuneInString, so an unconditional import
	// would make the generated file fail to compile ("imported and not used").
	// Generate then ORs in the submatch path's needs (one-pass vs. TDFA) once
	// that path is known, since it decides which packages the submatch core uses.
	NeedMatch bool // github.com/wow-look-at-my/go-regex-compiler/match
	NeedUTF8  bool // unicode/utf8

	// EarlyAccept is set for modes where reaching ANY accepting state proves a
	// match (prefix: some prefix matched). Transitions into an accepting state
	// are rendered as "return true" and accepting states are dropped from the
	// state machine (they become unreachable).
	EarlyAccept bool
	acceptSet   map[int]bool // IDs of accepting states (for EarlyAccept rendering)
	chainHeads  map[int]int  // chain head state ID -> chain index (for counter resets)

	// RestartBody is the statement the contains-mode search loop runs when no
	// transition matches: restart at the start state, resetting its chain
	// counter when the start state is itself a compressed chain head.
	RestartBody string

	// SkipToByte enables the strings.IndexByte fast path in the contains-mode
	// scan loop: when the search DFA sits in its start state and can only
	// leave it on one specific byte, memchr to that byte instead of stepping.
	SkipToByte bool
	SkipByte   rune

	// LiteralContains replaces the contains-mode body with a single
	// strings.Contains(input, Literal) call (pattern is one exact literal).
	LiteralContains bool
	Literal         string
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
	ctx.HasSubmatch = opts.Submatch != nil

	var subCtx submatchContext
	if opts.Submatch != nil {
		var err error
		subCtx, err = buildSubmatchContext(*opts.Submatch)
		if err != nil {
			return err
		}
	}

	// Fold the submatch path's import needs into the bool matcher's (seeded by
	// buildContext, gated on a matching loop actually being rendered). The
	// compiled submatch paths (one-pass and TDFA) add match.InRange/utf8 as
	// they need them. There is no interpreter path, so no generated code ever
	// imports sync.
	ctx.NeedMatch = ctx.NeedMatch || (ctx.HasSubmatch && subCtx.HasRanges)
	ctx.NeedUTF8 = ctx.NeedUTF8 || (ctx.HasSubmatch && !subCtx.ASCII)

	var buf bytes.Buffer

	if err := tmpl.ExecuteTemplate(&buf, "header", ctx); err != nil {
		return fmt.Errorf("executing header template: %w", err)
	}
	if err := tmpl.ExecuteTemplate(&buf, "matchFunc", ctx); err != nil {
		return fmt.Errorf("executing matchFunc template: %w", err)
	}

	if opts.Submatch != nil {
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

	// A full match asks whether a match ends WITH THE INPUT; prefix and contains
	// ask whether one ended on the way into this state. These differ only under
	// a word boundary, where a trailing \b is decided by the following
	// character.
	accepts := func(s *dfa.State) bool {
		if opts.Mode == MatchFull {
			return s.AcceptAtEnd
		}
		return s.Accept
	}

	ctx.acceptSet = make(map[int]bool)
	for _, s := range d.States {
		ts := templateState{ID: s.ID, Accept: accepts(s)}
		for _, tr := range s.Transitions {
			ts.Transitions = append(ts.Transitions, templateTransition{
				Lo: tr.Lo, Hi: tr.Hi, Next: tr.Next,
			})
		}
		ctx.States = append(ctx.States, ts)
		if accepts(s) {
			ctx.AcceptIDs = append(ctx.AcceptIDs, s.ID)
			ctx.acceptSet[s.ID] = true
		}
	}

	// Edge case: single accepting start state with no transitions (matches only empty string)
	if len(d.States) > 0 {
		startAccepts := accepts(d.States[d.Start])
		ctx.StartAccepts = startAccepts
		if len(d.States) == 1 && startAccepts && len(d.States[0].Transitions) == 0 {
			ctx.EdgeCase = true
			ctx.EdgeCaseAlwaysTrue = (opts.Mode == MatchContains || opts.Mode == MatchPrefix)
		}
	}

	// Prefix: some prefix matched. Contains: the DFA is a search DFA (built
	// with dfa.BuildSearch), so an accepting state means some substring
	// ending at the current position matched.
	if opts.Mode == MatchPrefix || opts.Mode == MatchContains {
		ctx.EarlyAccept = true
	}
	if ctx.EarlyAccept && !ctx.StartAccepts {
		// Accepting states are never entered (transitions into them return
		// true), so drop them from the emitted state machine.
		var kept []templateState
		for _, s := range ctx.States {
			if !s.Accept {
				kept = append(kept, s)
			}
		}
		ctx.States = kept
	}

	compressChains(&ctx)

	// Chain counters are function-scoped and a DFA loop (or the contains-mode
	// restart) may re-enter a compressed chain's head state, so every entry
	// into a chain head from outside the chain must reset that chain's
	// counter; otherwise the matcher resumes with a stale count and jumps to
	// the chain terminal too early (e.g. `a{3}(?:ba{3})*`, or a contains-mode
	// restart into a chain-compressed start state).
	ctx.chainHeads = make(map[int]int)
	for _, s := range ctx.States {
		if s.IsChain {
			ctx.chainHeads[s.ID] = s.ChainIndex
		}
	}
	ctx.RestartBody = ctx.enterState(ctx.Start)

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

	// Contains fast paths.
	if opts.Mode == MatchContains && !ctx.StartAccepts && !ctx.EdgeCase {
		// The whole pattern is one literal: contains IS strings.Contains.
		if opts.LiteralComplete && opts.LiteralPrefix != "" {
			ctx.LiteralContains = true
			ctx.Literal = opts.LiteralPrefix
		}
		// Otherwise, if the scan can only leave the start state on one
		// specific byte, use strings.IndexByte (memchr) to jump to the next
		// candidate instead of stepping the DFA byte-by-byte. Chain-compressed
		// start states are excluded: for those, state == Start does not mean
		// "at scan start" (the chain counter carries progress).
		if !ctx.LiteralContains && ctx.ASCII {
			for _, s := range ctx.States {
				if s.ID != ctx.Start {
					continue
				}
				if !s.IsChain && len(s.Transitions) == 1 && s.Transitions[0].Lo == s.Transitions[0].Hi {
					ctx.SkipToByte = true
					ctx.SkipByte = s.Transitions[0].Lo
				}
				break
			}
		}
	}

	// Seed the match/utf8 import needs from the bool matcher: emit them only
	// when the rendered body actually contains a matching loop (see the field
	// comment on NeedMatch). Generate ORs in the submatch path's needs later.
	loopRendered := len(ctx.States) > 0 && !ctx.EdgeCase &&
		!(ctx.EarlyAccept && ctx.StartAccepts) && !ctx.LiteralContains
	ctx.NeedMatch = ctx.HasRanges && loopRendered
	ctx.NeedUTF8 = !ascii && loopRendered

	return ctx
}

// enterState renders the statement for entering state id from outside it:
// under EarlyAccept an accepting target proves the match, and a chain-head
// target must have its chain counter reset before counting starts over.
func (ctx *templateContext) enterState(id int) string {
	if ctx.EarlyAccept && ctx.acceptSet[id] {
		return "return true"
	}
	if idx, ok := ctx.chainHeads[id]; ok {
		return fmt.Sprintf("state = %d; chainCount%d = 0", id, idx)
	}
	return fmt.Sprintf("state = %d", id)
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

	visited := set.New[int]()
	type chainInfo struct {
		stateIDs   []int
		terminalID int
		chainIndex int
	}
	var chains []chainInfo
	chainIdx := 0

	for _, s := range ctx.States {
		if visited.Contains(s.ID) {
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
		chainSet := set.Of[int](s.ID)
		current := target

		for {
			if chainSet.Contains(current) {
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
			chainSet.Add(current)
			current = nextTarget
		}

		if len(chain) >= minChainLength {
			for _, id := range chain {
				visited.Add(id)
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

	chainMembers := set.New[int]()
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
			chainMembers.Add(id)
		}
	}

	var filtered []templateState
	for _, s := range ctx.States {
		if !chainMembers.Contains(s.ID) {
			filtered = append(filtered, s)
		}
	}
	ctx.States = filtered

	var newAcceptIDs []int
	for _, id := range ctx.AcceptIDs {
		if !chainMembers.Contains(id) {
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

func stateTransition(ctx templateContext, s templateState, t templateTransition) string {
	if s.IsChain {
		// While counting, control stays in the head state; the jump to the
		// terminal is the only transition out of the chain (and the terminal
		// itself may be another chain's head, or this chain's own head when
		// the DFA loops straight back -- enterState resets the counter).
		return fmt.Sprintf("if chainCount%d >= %d { %s } else { chainCount%d++ }",
			s.ChainIndex, s.ChainMaxCount, ctx.enterState(s.ChainTerminal), s.ChainIndex)
	}
	return ctx.enterState(t.Next)
}

type groupedCase struct {
	Cond string
	Body string
}

func groupByteTransitions(ctx templateContext, s templateState) []groupedCase {
	return groupTransitions(ctx, s, func(t templateTransition) string {
		if t.Lo == t.Hi {
			return fmt.Sprintf("c == %s", quoteByte(t.Lo))
		}
		return fmt.Sprintf("match.InRange(c, %s, %s)", quoteByte(t.Lo), quoteByte(t.Hi))
	})
}

func groupRuneTransitions(ctx templateContext, s templateState) []groupedCase {
	return groupTransitions(ctx, s, func(t templateTransition) string {
		if t.Lo == t.Hi {
			return fmt.Sprintf("r == %s", quoteRune(t.Lo))
		}
		return fmt.Sprintf("match.InRange(r, %s, %s)", quoteRune(t.Lo), quoteRune(t.Hi))
	})
}

func groupTransitions(ctx templateContext, s templateState, condFn func(templateTransition) string) []groupedCase {
	var groups []groupedCase
	i := 0
	for i < len(s.Transitions) {
		body := stateTransition(ctx, s, s.Transitions[i])
		conds := condFn(s.Transitions[i])
		j := i + 1
		for j < len(s.Transitions) && stateTransition(ctx, s, s.Transitions[j]) == body {
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
