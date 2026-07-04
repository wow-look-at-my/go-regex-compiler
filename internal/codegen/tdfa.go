package codegen

// This file builds a tagged DFA (TDFA) capture automaton from a Thompson NFA
// program. It is the generalization of the one-pass builder (onepass.go): where
// one-pass requires every input rune to select a unique next instruction, the
// TDFA determinizes patterns with genuine capture AMBIGUITY (e.g. `(a*)(a*)`,
// `(a|ab)(a*)`, `(?i)abc`) into a plain `switch state` machine with fixed
// register operations on every transition and NO run-time interpreter (no
// program table, no live-position list, no object pool).
//
// Mechanics (this construction is a tagged-DFA / TDFA in the literature):
//
//   - A "capture slot" is an int holding a group boundary's byte offset (slot
//     2*g is group g's start, 2*g+1 its end). Slots 0/1 are the whole match.
//   - Each DFA state is an ordered list of NFA positions ("configs"). The order
//     is leftmost-greedy priority: exactly the thread priority Go's regexp uses
//     (earlier alternative first, greedy repeat prefers to continue). Two paths
//     reaching the same NFA position merge at construction time — the higher
//     priority one wins — so no disambiguation happens at run time.
//   - Registers: a flat file of ints. Config at position k in a state owns the
//     register block [k*numSlots, k*numSlots+numSlots); register (k,slot) holds
//     that config's byte offset for the slot, or -1 if the slot is unset on the
//     config's path. Isolating each config's slots in its own block is what lets
//     a losing branch keep provisional offsets without corrupting the winner and
//     lets a non-participating group stay -1.
//   - On a transition, each target config either copies a slot from its source
//     config's block or, when a group boundary is crossed on the epsilon path,
//     sets it to the post-consume byte position. These are fixed per transition.
//   - At end of input the accepting state's highest-priority match config is the
//     winner; its register block is read out as the capture offsets.

import (
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// tdMaxStates / tdMaxConfigs bound the construction; past them buildTDFA gives
// up (the caller then errors, never falling back to an interpreter).
const (
	tdMaxStates  = 20000
	tdMaxConfigs = 64
)

// tdInterval is an inclusive [lo,hi] rune range.
type tdInterval struct{ lo, hi rune }

// tdFill describes how one target config's register block is filled on a
// transition: crossed[slot] true => set the slot to the post-consume position;
// false => copy the slot from source-config block src.
type tdFill struct {
	src     int
	crossed []bool // len numSlots
}

// tdTrans is one transition: on a rune in [lo,hi], move to state next and fill
// its register blocks per fills (one tdFill per target config).
type tdTrans struct {
	lo, hi rune
	next   int
	fills  []tdFill
}

// tdState is one TDFA state: an ordered (priority) list of NFA pcs plus its
// transitions. accept/winner are set if the state contains a match config; the
// winner is the position of the highest-priority match config, whose register
// block holds the capture offsets to read at accept.
type tdState struct {
	id     int
	pcs    []int
	accept bool
	winner int
	trans  []tdTrans
}

// tdfaAutomaton is the built TDFA.
type tdfaAutomaton struct {
	states    []*tdState
	start     int
	numSlots  int
	maxConfig int
	startFill []tdFill // how to initialize the start state's registers at pos 0
	ascii     bool
}

// tdConfig is a config produced by a closure: an NFA pc plus, relative to the
// seed set, which slots were crossed on the epsilon path and which source
// config it descends from.
type tdConfig struct {
	pc      int
	crossed []bool
	src     int
}

// tdSeed is an entry into a closure with an associated source-config index.
type tdSeed struct {
	pc  int
	src int
}

// buildTDFA determinizes prog into a TDFA. numGroups excludes group 0. It
// returns ok=false when the pattern uses an empty-width assertion (Phase A does
// not compile interior/edge boundary conditions through the register machine —
// those go to the one-pass path or error) or exceeds the state/config budget.
func buildTDFA(prog *syntax.Prog, numGroups int) (*tdfaAutomaton, bool) {
	// Reject any reachable empty-width assertion: the register construction below
	// evaluates epsilon paths with a fixed boundary context, which is only sound
	// when there are no ^ $ \A \z \b \B to satisfy. (The one-pass path handles
	// the edge-anchored cases; interior assertions error rather than miscompile.)
	for pc := range prog.Inst {
		if prog.Inst[pc].Op == syntax.InstEmptyWidth {
			return nil, false
		}
	}

	numSlots := (numGroups + 1) * 2
	d := &tdfaAutomaton{numSlots: numSlots}
	stateMap := map[string]int{}
	var queue []int

	intern := func(cfgs []tdConfig) int {
		key := tdStateKey(cfgs)
		if id, ok := stateMap[key]; ok {
			return id
		}
		id := len(d.states)
		st := &tdState{id: id, winner: -1}
		for _, c := range cfgs {
			st.pcs = append(st.pcs, c.pc)
		}
		stateMap[key] = id
		d.states = append(d.states, st)
		queue = append(queue, id)
		return id
	}

	startCfgs := tdClosure(prog, numSlots, []tdSeed{{pc: prog.Start, src: 0}})
	d.start = intern(startCfgs)
	d.startFill = tdFillsFrom(startCfgs)
	if len(startCfgs) > d.maxConfig {
		d.maxConfig = len(startCfgs)
	}

	for len(queue) > 0 {
		if len(d.states) > tdMaxStates || d.maxConfig > tdMaxConfigs {
			return nil, false
		}
		st := d.states[queue[0]]
		queue = queue[1:]

		for pos, pc := range st.pcs {
			if prog.Inst[pc].Op == syntax.InstMatch {
				st.accept = true
				if st.winner < 0 {
					st.winner = pos
				}
			}
		}

		for _, iv := range tdCollectBounds(prog, st.pcs) {
			var seeds []tdSeed
			for pos, pc := range st.pcs {
				inst := &prog.Inst[pc]
				if inst.Op == syntax.InstMatch {
					continue
				}
				if tdRuneMatch(inst, iv.lo) {
					seeds = append(seeds, tdSeed{pc: int(inst.Out), src: pos})
				}
			}
			if len(seeds) == 0 {
				continue
			}
			targetCfgs := tdClosure(prog, numSlots, seeds)
			if len(targetCfgs) > d.maxConfig {
				d.maxConfig = len(targetCfgs)
			}
			next := intern(targetCfgs)
			st.trans = append(st.trans, tdTrans{lo: iv.lo, hi: iv.hi, next: next, fills: tdFillsFrom(targetCfgs)})
		}
	}
	if d.maxConfig > tdMaxConfigs {
		return nil, false
	}
	d.ascii = tdIsASCII(d, prog)
	return d, true
}

func tdFillsFrom(cfgs []tdConfig) []tdFill {
	out := make([]tdFill, len(cfgs))
	for i, c := range cfgs {
		out[i] = tdFill{src: c.src, crossed: c.crossed}
	}
	return out
}

// tdClosure computes the priority-ordered, pc-deduped epsilon-closure over
// multiple seeds. Seeds are processed in order (leftmost-greedy: earlier seed is
// higher priority); within a seed, an alternation explores its high-priority
// branch (Out) before its low-priority branch (Arg). The first path to reach a
// pc wins its crossed-slot set and its source-config index, exactly matching
// Go's leftmost-first NFA thread priority.
func tdClosure(prog *syntax.Prog, numSlots int, seeds []tdSeed) []tdConfig {
	c := &tdCloser{
		prog:     prog,
		numSlots: numSlots,
		visited:  make([]bool, len(prog.Inst)),
	}
	for _, s := range seeds {
		c.src = s.src
		c.work = make([]bool, numSlots)
		c.walk(s.pc)
	}
	return c.out
}

type tdCloser struct {
	prog     *syntax.Prog
	numSlots int
	visited  []bool
	work     []bool // slots crossed on the current path
	src      int
	out      []tdConfig
}

func (c *tdCloser) walk(pc int) {
	for {
		if pc < 0 || pc >= len(c.prog.Inst) || c.visited[pc] {
			return
		}
		c.visited[pc] = true
		inst := &c.prog.Inst[pc]
		switch inst.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			saved := append([]bool(nil), c.work...)
			c.walk(int(inst.Out))
			copy(c.work, saved)
			pc = int(inst.Arg)
		case syntax.InstNop:
			pc = int(inst.Out)
		case syntax.InstCapture:
			slot := int(inst.Arg)
			if slot < c.numSlots {
				old := c.work[slot]
				c.work[slot] = true
				c.walk(int(inst.Out))
				c.work[slot] = old
				return
			}
			pc = int(inst.Out)
		case syntax.InstRune, syntax.InstRune1, syntax.InstRuneAny, syntax.InstRuneAnyNotNL, syntax.InstMatch:
			c.out = append(c.out, tdConfig{pc: pc, crossed: append([]bool(nil), c.work...), src: c.src})
			return
		case syntax.InstFail:
			return
		default:
			return
		}
	}
}

func tdStateKey(cfgs []tdConfig) string {
	var sb strings.Builder
	for _, c := range cfgs {
		sb.WriteString(strconv.Itoa(c.pc))
		sb.WriteByte(',')
	}
	return sb.String()
}

// tdCollectBounds partitions the union of all consuming configs' rune classes
// into disjoint intervals, so each interval selects a constant set of firing
// configs (and thus one deterministic transition).
func tdCollectBounds(prog *syntax.Prog, pcs []int) []tdInterval {
	var ivs []tdInterval
	for _, pc := range pcs {
		if prog.Inst[pc].Op == syntax.InstMatch {
			continue
		}
		ivs = append(ivs, tdClassIntervals(prog, pc)...)
	}
	if len(ivs) == 0 {
		return nil
	}
	edges := map[rune]bool{}
	for _, iv := range ivs {
		edges[iv.lo] = true
		if iv.hi+1 <= 0x10FFFF {
			edges[iv.hi+1] = true
		}
	}
	cuts := make([]rune, 0, len(edges))
	for e := range edges {
		cuts = append(cuts, e)
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i] < cuts[j] })
	var out []tdInterval
	for i := 0; i < len(cuts); i++ {
		lo := cuts[i]
		var hi rune = 0x10FFFF
		if i+1 < len(cuts) {
			hi = cuts[i+1] - 1
		}
		covered := false
		for _, iv := range ivs {
			if lo >= iv.lo && lo <= iv.hi {
				covered = true
				break
			}
		}
		if covered && lo <= hi {
			out = append(out, tdInterval{lo, hi})
		}
	}
	return out
}

func tdClassIntervals(prog *syntax.Prog, pc int) []tdInterval {
	inst := &prog.Inst[pc]
	switch inst.Op {
	case syntax.InstRuneAny:
		return []tdInterval{{0, 0x10FFFF}}
	case syntax.InstRuneAnyNotNL:
		return []tdInterval{{0, '\n' - 1}, {'\n' + 1, 0x10FFFF}}
	case syntax.InstRune1:
		r := inst.Rune[0]
		if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
			return tdFoldIntervals([]rune{r, r})
		}
		return []tdInterval{{r, r}}
	case syntax.InstRune:
		rs := inst.Rune
		if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
			return tdFoldIntervals(rs)
		}
		var out []tdInterval
		for i := 0; i+1 < len(rs); i += 2 {
			out = append(out, tdInterval{rs[i], rs[i+1]})
		}
		if len(rs)%2 == 1 {
			out = append(out, tdInterval{rs[len(rs)-1], rs[len(rs)-1]})
		}
		return out
	}
	return nil
}

// tdFoldIntervals expands a rune class under Unicode simple case folding into
// concrete intervals, exactly as regexp does for (?i): every base rune plus each
// of its SimpleFold orbit members. Merges adjacent runes into ranges.
func tdFoldIntervals(rs []rune) []tdInterval {
	seen := map[rune]bool{}
	add := func(r rune) {
		seen[r] = true
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			seen[f] = true
		}
	}
	for i := 0; i+1 < len(rs); i += 2 {
		for x := rs[i]; x <= rs[i+1]; x++ {
			add(x)
		}
	}
	if len(rs)%2 == 1 {
		add(rs[len(rs)-1])
	}
	all := make([]rune, 0, len(seen))
	for r := range seen {
		all = append(all, r)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	var out []tdInterval
	for i := 0; i < len(all); i++ {
		lo := all[i]
		hi := lo
		for i+1 < len(all) && all[i+1] == hi+1 {
			hi = all[i+1]
			i++
		}
		out = append(out, tdInterval{lo, hi})
	}
	return out
}

// tdRuneMatch reports whether consuming instruction inst matches rune r
// (expanding case folds for FoldCase classes).
func tdRuneMatch(inst *syntax.Inst, r rune) bool {
	switch inst.Op {
	case syntax.InstRuneAny:
		return true
	case syntax.InstRuneAnyNotNL:
		return r != '\n'
	case syntax.InstRune1:
		if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
			return tdRuneInFold(inst.Rune, r)
		}
		return inst.Rune[0] == r
	case syntax.InstRune:
		if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
			return tdRuneInFold(inst.Rune, r)
		}
		rs := inst.Rune
		for i := 0; i+1 < len(rs); i += 2 {
			if r >= rs[i] && r <= rs[i+1] {
				return true
			}
		}
		if len(rs)%2 == 1 {
			return r == rs[len(rs)-1]
		}
		return false
	}
	return false
}

func tdRuneInFold(rs []rune, r rune) bool {
	inBase := func(x rune) bool {
		for i := 0; i+1 < len(rs); i += 2 {
			if x >= rs[i] && x <= rs[i+1] {
				return true
			}
		}
		if len(rs)%2 == 1 {
			return x == rs[len(rs)-1]
		}
		return false
	}
	if inBase(r) {
		return true
	}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if inBase(f) {
			return true
		}
	}
	return false
}

// tdIsASCII reports whether every transition's rune interval stays within ASCII,
// so the generated matcher can use the byte fast path instead of decoding runes.
func tdIsASCII(d *tdfaAutomaton, prog *syntax.Prog) bool {
	for _, st := range d.states {
		for _, tr := range st.trans {
			if tr.hi > unicode.MaxASCII {
				return false
			}
		}
	}
	return true
}
