package codegen

// This file implements onepass capture-automaton construction: turning a
// Thompson NFA program (from regexp/syntax) into a DETERMINISTIC, capture-
// annotated DFA whose transitions carry the capture-slot writes that must
// happen when they are taken. It is the analysis stage behind the *compiled*
// submatch matcher (see templates_onepass.go); the emitted code is a plain
// `switch state` automaton with inline `caps[k] = pos` writes and NO interpreter
// (no prog table, no thread lists, no epsilon-closure loop at run time).
//
// The idea mirrors the standard library's onepass analysis (regexp/onepass.go):
// a pattern is "one-pass" if, at every point, the next input rune uniquely
// selects the next instruction, so no backtracking or parallel NFA threads are
// ever needed. When that holds, the whole match is a single deterministic walk
// and captures can be baked onto the transitions. When it does not hold (real
// capture ambiguity), buildCapDFA returns ok=false and the caller falls back to
// the Thompson interpreter for correctness.

import (
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"
)

// capMaxStates bounds the compiled automaton; past it we give up and fall back
// (mirrors dfa.MaxStates). Pending-capture annotations are part of a state's
// identity, so a pathological pattern could in principle blow up; this caps it.
const capMaxStates = 10000

// capRange is an inclusive [lo,hi] rune range of a consuming instruction's class.
type capRange struct{ lo, hi rune }

// Empty-width assertion bits (mirror regexp/syntax.EmptyOp).
const (
	emptyBeginLine      = int(syntax.EmptyBeginLine)      // 1
	emptyEndLine        = int(syntax.EmptyEndLine)        // 2
	emptyBeginText      = int(syntax.EmptyBeginText)      // 4
	emptyEndText        = int(syntax.EmptyEndText)        // 8
	emptyWordBoundary   = int(syntax.EmptyWordBoundary)   // 16
	emptyNoWordBoundary = int(syntax.EmptyNoWordBoundary) // 32
)

// capConfig is a live NFA position during construction: a consuming (or Match)
// instruction plus the capture slots crossed on the unique epsilon path that
// reached it at the current byte position. pending is sorted and de-duplicated;
// because every slot in one closure is written at the same position, order does
// not matter, so a set is sufficient. pending is part of the enclosing state's
// identity: two occurrences of the same instruction reached with different
// pending captures are distinct states (e.g. the first vs. later iterations of a
// `+` loop, where the group-start capture fires only on the first). gate is the
// OR of empty-width assertion bits crossed on the path (^ $ \b …); the assertion
// must hold at the config's position.
type capConfig struct {
	pc      int
	pending []int
	gate    int
}

// capEdge is one outgoing transition of a capState: the consuming instruction's
// rune class, the capture-slot writes performed when the edge is taken (recorded
// at the current byte offset, BEFORE advancing past the rune), and the target
// state id.
type capEdge struct {
	ranges   []capRange
	anyRune  bool // opRuneAny: matches every rune
	anyNotNL bool // opRuneAnyNotNL: matches every rune except '\n'
	writes   []int
	next     int
}

// capState is one state of the capture automaton.
type capState struct {
	id           int
	edges        []capEdge
	accept       bool  // reachable Match => acceptable at end-of-input
	acceptWrites []int // capture slots written at accept (Match config's pending)
	acceptGate   int   // empty-width bits required at end-of-input to accept

	configs []capConfig // build-time only: the NFA positions this state represents
}

// capDFA is the compiled onepass capture automaton.
type capDFA struct {
	states    []*capState
	start     int
	numSlots  int
	ascii     bool
	startGate int // empty-width bits required at position 0 to begin matching
}

// buildCapDFA constructs the onepass capture automaton for prog. It returns
// ok=false if the pattern is not one-pass (ambiguous transitions), uses a
// fold-case class, carries an empty-width assertion the compiled path cannot
// evaluate (an interior text anchor), or exceeds the state budget — in every
// such case the caller falls back to the TDFA path and then, if that also
// declines, returns an error (there is no interpreter). Leading/trailing text
// anchors and provably-always-true word boundaries are handled here, not
// rejected. numGroups excludes group 0.
func buildCapDFA(prog *syntax.Prog, numGroups int) (*capDFA, bool) {
	numSlots := (numGroups + 1) * 2
	d := &capDFA{numSlots: numSlots}
	stateMap := make(map[string]int)
	var queue []int

	intern := func(configs []capConfig) int {
		sortConfigs(configs)
		key := configKey(configs)
		if id, ok := stateMap[key]; ok {
			return id
		}
		id := len(d.states)
		stateMap[key] = id
		d.states = append(d.states, &capState{id: id, configs: configs})
		queue = append(queue, id)
		return id
	}

	startConfigs, ok := closureFrom(prog, numSlots, prog.Start)
	if !ok {
		return nil, false
	}
	d.start = intern(startConfigs)

	for len(queue) > 0 {
		if len(d.states) > capMaxStates {
			return nil, false
		}
		st := d.states[queue[0]]
		queue = queue[1:]
		isStart := st.id == d.start

		for _, c := range st.configs {
			if prog.Inst[c.pc].Op == syntax.InstMatch {
				st.accept = true
				st.acceptWrites = c.pending
				st.acceptGate = c.gate
				continue
			}
			// A consuming config's gate must hold at that config's position.
			//   - At the start it is folded into d.startGate (checked once at pos 0).
			//   - An interior \b/\B is a no-op we drop: dfa.ValidateAssertions runs
			//     before codegen on every path and admits an interior word-boundary
			//     assertion ONLY where it is provably always satisfied (both sides
			//     known, uniform, and consistent with it), so it never rejects a
			//     real match. Dropping it and building the edge as if the assertion
			//     were absent is exactly how text anchors fold away at the match
			//     edges — e.g. (a\Bb) compiles like (ab).
			//   - An interior text anchor (^ $ \A \z) cannot be evaluated mid-match;
			//     the validator rejects such a placement upstream, so one reaching
			//     here means the pattern was not validated. Bail rather than
			//     miscompile.
			if c.gate != 0 {
				switch {
				case isStart:
					if d.startGate != 0 && d.startGate != c.gate {
						return nil, false // divergent start gates
					}
					d.startGate = c.gate
				case c.gate&^(emptyWordBoundary|emptyNoWordBoundary) != 0:
					return nil, false // interior text anchor: unvalidated
				default:
					// interior word boundary, proven always-true upstream: no-op
				}
			}
			ranges, anyR, anyNL, rok := instRanges(prog, c.pc)
			if !rok {
				return nil, false
			}
			targets, tok := closureFrom(prog, numSlots, int(prog.Inst[c.pc].Out))
			if !tok {
				return nil, false
			}
			st.edges = append(st.edges, capEdge{
				ranges:   ranges,
				anyRune:  anyR,
				anyNotNL: anyNL,
				writes:   c.pending,
				next:     intern(targets),
			})
		}

		if !edgesDisjoint(st.edges) {
			return nil, false // ambiguous: some rune selects two instructions
		}
	}

	// A non-zero start gate is validated once at position 0, so the start state
	// must not be re-entered mid-match (where the gate would no longer hold).
	if d.startGate != 0 {
		for _, st := range d.states {
			for _, e := range st.edges {
				if e.next == d.start {
					return nil, false
				}
			}
		}
	}

	// Only text-anchor (^ $ \A \z, always satisfied at the ends) and word-boundary
	// gates are resolvable by the compiled path; anything else falls back.
	if !gateResolvable(d.startGate &^ (emptyBeginText | emptyBeginLine)) {
		return nil, false
	}
	for _, st := range d.states {
		if st.accept && !gateResolvable(st.acceptGate&^(emptyEndText|emptyEndLine)) {
			return nil, false
		}
	}

	d.ascii = capIsASCII(d)
	return d, true
}

// gateResolvable reports whether the residual gate (after masking the text-edge
// bits that are always satisfied at position 0 / end-of-input) is one the
// compiled path can evaluate: nothing, or a single word-boundary assertion.
func gateResolvable(residual int) bool {
	switch residual {
	case 0, emptyWordBoundary, emptyNoWordBoundary:
		return true
	default:
		return false
	}
}

// closureFrom computes the epsilon-closure of a single seed program counter,
// following alternations (leftmost-first: Out before Arg), Nops, and Captures
// (accumulating the crossed slots), and collecting every reachable consuming or
// Match instruction as a capConfig. It returns ok=false if it meets an
// empty-width assertion (unsupported in the compiled path) or a capture slot
// out of range. The visited set makes the highest-priority path to each
// instruction win, matching leftmost-first NFA semantics.
func closureFrom(prog *syntax.Prog, numSlots, seed int) ([]capConfig, bool) {
	cs := &closer{
		prog:     prog,
		numSlots: numSlots,
		visited:  make([]bool, len(prog.Inst)),
		ok:       true,
	}
	cs.walk(seed)
	if !cs.ok {
		return nil, false
	}
	return cs.configs, true
}

type closer struct {
	prog     *syntax.Prog
	numSlots int
	visited  []bool
	work     []int // current pending-capture stack
	gate     int   // current empty-width bits accumulated on this path
	configs  []capConfig
	ok       bool
}

func (cs *closer) walk(pc int) {
	for {
		if pc < 0 || pc >= len(cs.prog.Inst) || cs.visited[pc] {
			return
		}
		cs.visited[pc] = true
		inst := &cs.prog.Inst[pc]
		switch inst.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			cs.walk(int(inst.Out))
			pc = int(inst.Arg)
		case syntax.InstNop:
			pc = int(inst.Out)
		case syntax.InstCapture:
			slot := int(inst.Arg)
			if slot < cs.numSlots {
				cs.work = append(cs.work, slot)
				cs.walk(int(inst.Out))
				cs.work = cs.work[:len(cs.work)-1]
				return
			}
			pc = int(inst.Out)
		case syntax.InstEmptyWidth:
			// Accumulate the assertion's required bits onto the path; the gate is
			// resolved per-position when the config is placed (buildCapDFA): a
			// start/accept gate becomes an edge word-boundary check, an interior
			// word boundary is dropped as an always-true no-op, and an interior
			// text anchor causes a fall back.
			saved := cs.gate
			cs.gate |= int(inst.Arg)
			cs.walk(int(inst.Out))
			cs.gate = saved
			return
		case syntax.InstRune, syntax.InstRune1, syntax.InstRuneAny, syntax.InstRuneAnyNotNL, syntax.InstMatch:
			cs.emit(pc)
			return
		case syntax.InstFail:
			return
		default:
			cs.ok = false
			return
		}
	}
}

func (cs *closer) emit(pc int) {
	pend := append([]int(nil), cs.work...)
	sort.Ints(pend)
	pend = dedupInts(pend)
	cs.configs = append(cs.configs, capConfig{pc: pc, pending: pend, gate: cs.gate})
}

// instRanges extracts the rune class of a consuming instruction as inclusive
// ranges (or the any/any-not-NL markers). It returns ok=false for fold-case
// classes (unsupported in the compiled path) or non-consuming ops.
func instRanges(prog *syntax.Prog, pc int) (ranges []capRange, anyRune, anyNotNL, ok bool) {
	inst := &prog.Inst[pc]
	switch inst.Op {
	case syntax.InstRune1:
		if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
			return nil, false, false, false
		}
		r := inst.Rune[0]
		return []capRange{{r, r}}, false, false, true
	case syntax.InstRune:
		if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
			return nil, false, false, false
		}
		rs := inst.Rune
		if len(rs) == 1 {
			return []capRange{{rs[0], rs[0]}}, false, false, true
		}
		var out []capRange
		for i := 0; i+1 < len(rs); i += 2 {
			out = append(out, capRange{rs[i], rs[i+1]})
		}
		if len(rs)%2 == 1 {
			last := rs[len(rs)-1]
			out = append(out, capRange{last, last})
		}
		return out, false, false, true
	case syntax.InstRuneAny:
		return nil, true, false, true
	case syntax.InstRuneAnyNotNL:
		return nil, false, true, true
	}
	return nil, false, false, false
}

// edgeSpans expands an edge to the concrete rune ranges it consumes, so
// disjointness across edges can be checked uniformly.
func edgeSpans(e capEdge) []capRange {
	switch {
	case e.anyRune:
		return []capRange{{0, 0x10FFFF}}
	case e.anyNotNL:
		return []capRange{{0, '\n' - 1}, {'\n' + 1, 0x10FFFF}}
	default:
		return e.ranges
	}
}

// edgesDisjoint reports whether the edges' rune classes are pairwise
// non-overlapping — the core one-pass condition. If any rune could select two
// different edges, the pattern is ambiguous and cannot be compiled.
func edgesDisjoint(edges []capEdge) bool {
	type span struct {
		lo, hi rune
		edge   int
	}
	var spans []span
	for i, e := range edges {
		for _, r := range edgeSpans(e) {
			spans = append(spans, span{r.lo, r.hi, i})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].lo < spans[j].lo })
	for i := 1; i < len(spans); i++ {
		if spans[i].lo <= spans[i-1].hi && spans[i].edge != spans[i-1].edge {
			return false
		}
	}
	return true
}

// capIsASCII reports whether every consuming class stays within ASCII, so the
// generated matcher can use the byte fast-path instead of decoding runes.
func capIsASCII(d *capDFA) bool {
	for _, st := range d.states {
		for _, e := range st.edges {
			if e.anyRune || e.anyNotNL {
				return false
			}
			for _, r := range e.ranges {
				if r.hi > 127 {
					return false
				}
			}
		}
	}
	return true
}

func sortConfigs(configs []capConfig) {
	sort.Slice(configs, func(i, j int) bool {
		if configs[i].pc != configs[j].pc {
			return configs[i].pc < configs[j].pc
		}
		if configs[i].gate != configs[j].gate {
			return configs[i].gate < configs[j].gate
		}
		return intsLess(configs[i].pending, configs[j].pending)
	})
}

func configKey(configs []capConfig) string {
	var sb strings.Builder
	for _, c := range configs {
		sb.WriteString(strconv.Itoa(c.pc))
		sb.WriteByte('/')
		sb.WriteString(strconv.Itoa(c.gate))
		sb.WriteByte(':')
		for _, s := range c.pending {
			sb.WriteString(strconv.Itoa(s))
			sb.WriteByte(',')
		}
		sb.WriteByte(';')
	}
	return sb.String()
}

func dedupInts(s []int) []int {
	if len(s) < 2 {
		return s
	}
	out := s[:1]
	for _, v := range s[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

func intsLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
