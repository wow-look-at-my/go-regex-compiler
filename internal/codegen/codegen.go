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

// latchLoopMask are the AcceptOn bits decidable inside the matching loop
// (i.e. by looking at the upcoming rune); AcceptOnEOT is decided after it.
const latchLoopMask = dfa.AcceptOnOther | dfa.AcceptOnWord | dfa.AcceptOnNL

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
	AcceptIDs          []int // states accepting at end of text
	OtherLatchIDs      []int // states accepting before a non-word, non-newline rune
	EdgeCase           bool  // single start state with no transitions
	EdgeCaseAlwaysTrue bool  // edge case AND the matcher is trivially true
	StartAccepts       bool  // prefix/contains matcher is trivially true
	NumChains          int
	HasRanges          bool
	HasAssertions      bool   // pattern contains empty-width assertions
	ContainsSeed       string // statements seeding `state` per start position (contains mode)
	NeedMatchImport    bool   // body uses match.InRange
	NeedUTF8Import     bool   // body uses utf8.DecodeRuneInString
	LoopHasCases       bool   // the state switch has at least one case (i.e. `c` is used)
}

// templateState mirrors dfa.State for use in templates.
type templateState struct {
	ID            int
	Accept        bool // accepts at end of text
	AcceptOn      dfa.AcceptMask
	LatchStmt     string // acceptance latch emitted before stepping (prefix/contains)
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
	Body string // Go statement emitted when the transition is taken
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
		PackageName:   opts.PackageName,
		FuncName:      opts.FuncName,
		Regex:         opts.Regex,
		ModeComment:   matchModeComment(opts.Mode),
		Mode:          modeStr,
		Start:         d.Start,
		HasAssertions: d.HasAssertions,
	}

	// Prune unreachable states: full/prefix matching starts at d.Start only,
	// while contains matching can start at any per-context start state. The
	// builder always materializes all four start states, so modes that use
	// fewer roots would otherwise emit dead switch cases.
	roots := []int{d.Start}
	if opts.Mode == MatchContains {
		roots = d.StartFor[:]
	}
	reachable := reachableStates(d, roots)

	for _, s := range d.States {
		if !reachable[s.ID] {
			continue
		}
		ts := templateState{ID: s.ID, Accept: s.Accept, AcceptOn: s.AcceptOn}
		for _, tr := range s.Transitions {
			ts.Transitions = append(ts.Transitions, templateTransition{
				Lo: tr.Lo, Hi: tr.Hi, Next: tr.Next,
			})
		}
		ctx.States = append(ctx.States, ts)
		if s.AcceptOn&dfa.AcceptOnEOT != 0 {
			ctx.AcceptIDs = append(ctx.AcceptIDs, s.ID)
		}
		if s.AcceptOn&dfa.AcceptOnOther != 0 {
			ctx.OtherLatchIDs = append(ctx.OtherLatchIDs, s.ID)
		}
	}

	// Decide byte- vs rune-based matching from the states this mode actually
	// renders: pruned start-context subgraphs must not force the rune loop
	// (a graph with no transitions at all must take the byte loop, whose body
	// stays fully reachable).
	ascii := true
	for _, s := range ctx.States {
		for _, tr := range s.Transitions {
			if tr.Hi > unicode.MaxASCII {
				ascii = false
				break
			}
		}
		if !ascii {
			break
		}
	}
	ctx.ASCII = ascii

	if len(d.States) > 0 {
		// StartAccepts: the prefix/contains matcher is trivially true because
		// every relevant start state accepts regardless of context (an empty
		// match exists at every position).
		switch opts.Mode {
		case MatchPrefix:
			ctx.StartAccepts = d.States[d.Start].AcceptOn == dfa.AcceptAlways
		case MatchContains:
			ctx.StartAccepts = true
			for _, id := range d.StartFor {
				if d.States[id].AcceptOn != dfa.AcceptAlways {
					ctx.StartAccepts = false
					break
				}
			}
		}

		// Edge case: a single reachable state with no transitions.
		if len(ctx.States) == 1 && len(ctx.States[0].Transitions) == 0 && ctx.States[0].ID == d.Start {
			mask := ctx.States[0].AcceptOn
			switch {
			case opts.Mode == MatchFull && mask&dfa.AcceptOnEOT != 0:
				ctx.EdgeCase = true // matches exactly the empty string
			case opts.Mode != MatchFull && mask == dfa.AcceptAlways:
				ctx.EdgeCase = true
				ctx.EdgeCaseAlwaysTrue = true
			}
		}
	}

	compressChains(&ctx)
	computeTransitionBodies(&ctx)
	computeLatchStmts(&ctx, ascii)
	if opts.Mode == MatchContains {
		ctx.ContainsSeed = containsSeed(d, ascii)
	}

	// The loop's byte variable must only be declared when some emitted case
	// actually reads it. Transition conditions read it, and so do conditional
	// latches; an unconditional `return true` latch swallows the whole case
	// body (including any transitions, which become unreachable), and a state
	// with neither (e.g. `$`: acceptance decided only at end of text) emits no
	// case at all.
	for _, s := range ctx.States {
		lm := s.AcceptOn & latchLoopMask
		unconditionalLatch := opts.Mode != MatchFull && lm == latchLoopMask
		if len(s.Transitions) > 0 && !unconditionalLatch {
			ctx.LoopHasCases = true
			break
		}
		if opts.Mode != MatchFull && lm != 0 && lm != latchLoopMask {
			ctx.LoopHasCases = true
			break
		}
	}

	// HasRanges considers only transitions that are actually emitted: a state
	// whose case is swallowed by an unconditional `return true` latch never
	// renders its transitions (and thus no match.InRange calls).
	for _, s := range ctx.States {
		if opts.Mode != MatchFull && s.AcceptOn&latchLoopMask == latchLoopMask {
			continue
		}
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

	// Emit imports only when the rendered body actually contains a matching
	// loop. The short-circuit bodies (`return false` for an empty DFA,
	// `return true`/`return len(input) == 0` for the edge case, and the
	// prefix/contains-mode early return when the start state accepts)
	// reference neither match.InRange nor utf8.DecodeRuneInString, so an
	// import would make the generated file fail to compile.
	loopRendered := len(ctx.States) > 0 && !ctx.EdgeCase &&
		!(opts.Mode != MatchFull && ctx.StartAccepts)
	ctx.NeedMatchImport = ctx.HasRanges && loopRendered
	ctx.NeedUTF8Import = !ascii && loopRendered

	return ctx
}

// reachableStates returns the set of state IDs reachable from roots.
func reachableStates(d *dfa.DFA, roots []int) map[int]bool {
	stateByID := make(map[int]*dfa.State, len(d.States))
	for _, s := range d.States {
		stateByID[s.ID] = s
	}
	reach := make(map[int]bool)
	stack := append([]int{}, roots...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reach[id] {
			continue
		}
		reach[id] = true
		s := stateByID[id]
		if s == nil {
			continue
		}
		for _, t := range s.Transitions {
			stack = append(stack, t.Next)
		}
	}
	return reach
}

// wordCond emits a Go condition testing whether v (a byte or rune variable)
// is an ASCII word character, matching regexp's \b semantics.
func wordCond(v string) string {
	return fmt.Sprintf("%s == '_' || ('0' <= %s && %s <= '9') || ('A' <= %s && %s <= 'Z') || ('a' <= %s && %s <= 'z')",
		v, v, v, v, v, v, v)
}

// computeLatchStmts fills the acceptance latch emitted before stepping in the
// prefix/contains loops: the match ending at the current position is accepted
// iff the state's AcceptOn mask covers the class of the upcoming rune.
// Assertion-free accepting states latch unconditionally.
func computeLatchStmts(ctx *templateContext, ascii bool) {
	v := "r"
	if ascii {
		v = "c"
	}
	for i := range ctx.States {
		ctx.States[i].LatchStmt = latchStmt(ctx.States[i].AcceptOn, v)
	}
}

func latchStmt(m dfa.AcceptMask, v string) string {
	const loopMask = dfa.AcceptOnOther | dfa.AcceptOnWord | dfa.AcceptOnNL
	switch m & loopMask {
	case 0:
		return "" // accepts at end of text only (or never); handled after the loop
	case loopMask:
		return "return true"
	case dfa.AcceptOnWord:
		return fmt.Sprintf("if %s { return true }", wordCond(v))
	case dfa.AcceptOnOther | dfa.AcceptOnNL:
		return fmt.Sprintf("if !(%s) { return true }", wordCond(v))
	case dfa.AcceptOnNL:
		return fmt.Sprintf("if %s == '\\n' { return true }", v)
	case dfa.AcceptOnOther:
		return fmt.Sprintf("if %s != '\\n' && !(%s) { return true }", v, wordCond(v))
	case dfa.AcceptOnOther | dfa.AcceptOnWord:
		return fmt.Sprintf("if %s != '\\n' { return true }", v)
	case dfa.AcceptOnWord | dfa.AcceptOnNL:
		return fmt.Sprintf("if %s == '\\n' || %s { return true }", v, wordCond(v))
	}
	return ""
}

// containsSeed emits the statements seeding `state` for a contains-mode start
// position. When the pattern carries assertions, the start state depends on
// the class of the rune immediately before the position.
func containsSeed(d *dfa.DFA, ascii bool) string {
	sf := d.StartFor
	if sf[dfa.ClassOther] == sf[dfa.ClassWord] && sf[dfa.ClassOther] == sf[dfa.ClassNL] &&
		sf[dfa.ClassOther] == sf[dfa.ClassBegin] {
		return fmt.Sprintf("\t\tstate := %d", d.Start)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "\t\tstate := %d\n", sf[dfa.ClassOther])
	fmt.Fprintf(&b, "\t\tif start == 0 {\n")
	fmt.Fprintf(&b, "\t\t\tstate = %d\n", sf[dfa.ClassBegin])
	if ascii {
		fmt.Fprintf(&b, "\t\t} else if p := input[start-1]; %s {\n", wordCond("p"))
	} else {
		fmt.Fprintf(&b, "\t\t} else if p, _ := utf8.DecodeLastRuneInString(input[:start]); %s {\n", wordCond("p"))
	}
	fmt.Fprintf(&b, "\t\t\tstate = %d\n", sf[dfa.ClassWord])
	fmt.Fprintf(&b, "\t\t} else if p == '\\n' {\n")
	fmt.Fprintf(&b, "\t\t\tstate = %d\n", sf[dfa.ClassNL])
	fmt.Fprintf(&b, "\t\t}")
	return b.String()
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

		if ctx.Mode != "full" && s.AcceptOn&latchLoopMask == latchLoopMask {
			// In prefix/contains mode this state's case is an unconditional
			// `return true` that swallows its transitions; compressing a chain
			// through it would only declare a counter that is never read.
			// (Chain members share the head's AcceptOn, so checking candidate
			// heads suffices.)
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
			if cs.AcceptOn != s.AcceptOn {
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

	var newOtherLatchIDs []int
	for _, id := range ctx.OtherLatchIDs {
		if !chainMembers[id] {
			newOtherLatchIDs = append(newOtherLatchIDs, id)
		}
	}
	ctx.OtherLatchIDs = newOtherLatchIDs

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

// computeTransitionBodies fills in the Go statement emitted for every
// transition, including chain-counter bookkeeping. Chain counters are
// function-scoped and a DFA loop may re-enter a compressed chain's head state
// (e.g. `a{3}(?:ba{3})*`), so every transition INTO a chain head from outside
// the chain must reset that chain's counter; otherwise the matcher resumes
// with a stale count and jumps to the chain terminal too early.
func computeTransitionBodies(ctx *templateContext) {
	headChain := make(map[int]int) // chain head state ID -> chain index
	for i := range ctx.States {
		if ctx.States[i].IsChain {
			headChain[ctx.States[i].ID] = ctx.States[i].ChainIndex
		}
	}
	reset := func(target int) string {
		if idx, ok := headChain[target]; ok {
			return fmt.Sprintf("; chainCount%d = 0", idx)
		}
		return ""
	}
	for i := range ctx.States {
		s := &ctx.States[i]
		for j := range s.Transitions {
			t := &s.Transitions[j]
			if s.IsChain {
				// While counting, control stays in the head state; the jump to
				// the terminal is the only transition out of the chain (and the
				// terminal itself may be another chain's head).
				t.Body = fmt.Sprintf("if chainCount%d >= %d { state = %d%s } else { chainCount%d++ }",
					s.ChainIndex, s.ChainMaxCount, s.ChainTerminal, reset(s.ChainTerminal), s.ChainIndex)
				continue
			}
			t.Body = fmt.Sprintf("state = %d%s", t.Next, reset(t.Next))
		}
	}
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
		body := s.Transitions[i].Body
		conds := condFn(s.Transitions[i])
		j := i + 1
		for j < len(s.Transitions) && s.Transitions[j].Body == body {
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
