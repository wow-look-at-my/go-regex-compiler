package codegen

import (
	"github.com/wow-look-at-my/go-containers/set"
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// tdMaxStates / tdMaxConfigs bound the construction; past them buildTDFA gives
const (
	tdMaxStates  = 20000
	tdMaxConfigs = 64
)

// tdInterval is an inclusive [lo,hi] rune range.
type tdInterval struct{ lo, hi rune }

// tdFill describes how target config's register block is filled on a
type tdFill struct {
	src     int
	crossed []bool // len numSlots
}

// tdTrans is transition: on a rune in [lo,hi], move to state next and fill
type tdTrans struct {
	lo, hi rune
	next   int
	fills  []tdFill
}

// tdState is TDFA state: an ordered (priority) list of NFA pcs plus its
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
	startFill []tdFill // how to initialize the start state's registers at pos
	ascii     bool
}

// tdConfig is a config produced by a closure: an NFA pc plus, relative to the
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

// buildTDFA determinizes prog into a TDFA. numGroups excludes group. It
func buildTDFA(prog *syntax.Prog, numGroups int) (*tdfaAutomaton, bool) {
	// Reject text-anchor assertions; tolerate always-true word boundaries. The
	for pc := range prog.Inst {
		inst := &prog.Inst[pc]
		if inst.Op == syntax.InstEmptyWidth &&
			int(inst.Arg)&^(emptyWordBoundary|emptyNoWordBoundary) != 0 {
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
		case syntax.InstEmptyWidth:
			// Always-true \b/\B (buildTDFA rejected every other assertion): the
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
	edges := set.New[rune]()
	for _, iv := range ivs {
		edges.Add(iv.lo)
		if iv.hi+1 <= 0x10FFFF {
			edges.Add(iv.hi + 1)
		}
	}
	cuts := make([]rune, 0, edges.Len())
	for e := range edges.All() {
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
func tdFoldIntervals(rs []rune) []tdInterval {
	seen := set.New[rune]()
	add := func(r rune) {
		seen.Add(r)
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			seen.Add(f)
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
	all := make([]rune, 0, seen.Len())
	for r := range seen.All() {
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
