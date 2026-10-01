package dfa

import (
	"fmt"
	"regexp/syntax"
)

// ValidateAssertions checks every reachable empty-width assertion (^, $, \A,
func ValidateAssertions(prog *syntax.Prog, anchorStart, anchorEnd bool) error {
	v := &validator{prog: prog, anchorStart: anchorStart, anchorEnd: anchorEnd}
	return v.validate()
}

// ValidateAssertionsForBoolMatcher is ValidateAssertions for a caller building
func ValidateAssertionsForBoolMatcher(prog *syntax.Prog, anchorStart, anchorEnd bool) error {
	v := &validator{prog: prog, anchorStart: anchorStart, anchorEnd: anchorEnd, skipWordBoundary: true}
	return v.validate()
}

type validator struct {
	prog             *syntax.Prog
	anchorStart      bool
	anchorEnd        bool
	skipWordBoundary bool // the caller's DFA decides \b itself

	reachable      []bool // reachable from prog.Start (crossing consumes)
	consumedBefore []bool // reachable from prog.Start only after >= consumed rune
	canReachMatch  []bool // can reach an InstMatch (possibly consuming)
	startEps       []bool // epsilon-reachable from prog.Start (no consumption)
}

// charClass describes what the characters on side of a boundary can be.
type charClass struct {
	word    bool // a word character ([-9A-Za-z_]) is possible
	nonword bool // a non-word character (or text edge) is possible
	unknown bool // unanchored text edge: could be anything
}

func (c charClass) empty() bool { return !c.word && !c.nonword && !c.unknown }

// pureWord/pureNonword report that the side is fully known and uniform.
func (c charClass) pureWord() bool    { return c.word && !c.nonword && !c.unknown }
func (c charClass) pureNonword() bool { return c.nonword && !c.word && !c.unknown }

func (v *validator) validate() error {
	n := len(v.prog.Inst)
	v.reachable = v.fullReach([]int{v.prog.Start})
	v.startEps = v.epsReach([]int{v.prog.Start})

	// Everything downstream of a reachable rune instruction has consumed >=.
	var consumeSeeds []int
	for pc := 0; pc < n; pc++ {
		if v.reachable[pc] && isRuneInst(v.prog.Inst[pc].Op) {
			consumeSeeds = append(consumeSeeds, int(v.prog.Inst[pc].Out))
		}
	}
	v.consumedBefore = v.fullReach(consumeSeeds)

	// Reverse reachability to InstMatch.
	v.canReachMatch = make([]bool, n)
	rev := make([][]int, n)
	var matchSeeds []int
	for pc := 0; pc < n; pc++ {
		inst := &v.prog.Inst[pc]
		if inst.Op == syntax.InstMatch {
			matchSeeds = append(matchSeeds, pc)
		}
		for _, out := range instEdges(inst) {
			if out < n {
				rev[out] = append(rev[out], pc)
			}
		}
	}
	for stack := matchSeeds; len(stack) > 0; {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if v.canReachMatch[pc] {
			continue
		}
		v.canReachMatch[pc] = true
		stack = append(stack, rev[pc]...)
	}

	for pc := 0; pc < n; pc++ {
		inst := &v.prog.Inst[pc]
		if inst.Op != syntax.InstEmptyWidth || !v.reachable[pc] {
			continue
		}
		if err := v.checkAssertion(pc, syntax.EmptyOp(inst.Arg)); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) checkAssertion(pc int, op syntax.EmptyOp) error {
	if op&(syntax.EmptyBeginText|syntax.EmptyBeginLine) != 0 {
		name := "^"
		if op&syntax.EmptyBeginText != 0 && op&syntax.EmptyBeginLine == 0 {
			name = `\A or ^`
		} else if op&syntax.EmptyBeginText == 0 {
			name = `(?m)^`
		}
		if !v.anchorStart {
			return fmt.Errorf("regex anchors to the start of the text with %s, which contains mode cannot honor (matches may start anywhere); use --match full or prefix, or drop the anchor", name)
		}
		if v.consumedBefore[pc] {
			return fmt.Errorf("regex uses %s after the pattern can already have consumed input; the generated DFA cannot evaluate start-of-%s assertions mid-match", name, beginKind(op))
		}
	}
	if op&(syntax.EmptyEndText|syntax.EmptyEndLine) != 0 {
		name := "$"
		if op&syntax.EmptyEndText != 0 && op&syntax.EmptyEndLine == 0 {
			name = `\z or $`
		} else if op&syntax.EmptyEndText == 0 {
			name = `(?m)$`
		}
		if v.consumesAfter(pc) {
			return fmt.Errorf("regex uses %s where the pattern can still consume input afterwards; the generated DFA cannot evaluate end-of-%s assertions mid-match", name, endKind(op))
		}
		if !v.anchorEnd {
			return fmt.Errorf("regex anchors to the end of the text with %s, which %s mode cannot honor (a match may end before the input does); use --match full or drop the anchor", name, v.modeName())
		}
	}
	if op&(syntax.EmptyWordBoundary|syntax.EmptyNoWordBoundary) != 0 && !v.skipWordBoundary {
		before := v.beforeClass(pc)
		after := v.afterClass(pc)
		ok := false
		if before.empty() || after.empty() {
			// The assertion can never be crossed on a path that consumes
			ok = true
		} else if op&syntax.EmptyWordBoundary != 0 {
			ok = (before.pureWord() && after.pureNonword()) || (before.pureNonword() && after.pureWord())
		} else {
			ok = (before.pureWord() && after.pureWord()) || (before.pureNonword() && after.pureNonword())
		}
		if !ok {
			name := `\b`
			if op&syntax.EmptyWordBoundary == 0 {
				name = `\B`
			}
			return fmt.Errorf("regex uses %s in a position where the generated DFA cannot verify it; word-boundary assertions are only supported where they always hold, e.g. \\bfoo or foo\\b at the edge of a fully anchored match", name)
		}
	}
	return nil
}

// beforeClass computes the possible characters immediately before the
func (v *validator) beforeClass(pc int) charClass {
	var c charClass
	if v.startEps[pc] {
		// Crossed before consuming anything: the previous "character" is the
		if v.anchorStart {
			c.nonword = true // begin-of-text counts as a non-word character
		} else {
			c.unknown = true // unanchored: any character may precede the match
		}
	}
	for rp := 0; rp < len(v.prog.Inst); rp++ {
		inst := &v.prog.Inst[rp]
		if !v.reachable[rp] || !isRuneInst(inst.Op) {
			continue
		}
		if v.epsReach([]int{int(inst.Out)})[pc] {
			c = c.merge(runeInstClass(inst))
		}
	}
	return c
}

// afterClass computes the possible characters immediately after the boundary
func (v *validator) afterClass(pc int) charClass {
	var c charClass
	eps := v.epsReach([]int{int(v.prog.Inst[pc].Out)})
	for rp := 0; rp < len(v.prog.Inst); rp++ {
		if !eps[rp] {
			continue
		}
		inst := &v.prog.Inst[rp]
		if isRuneInst(inst.Op) {
			c = c.merge(runeInstClass(inst))
		} else if inst.Op == syntax.InstMatch {
			if v.anchorEnd {
				c.nonword = true // end-of-text counts as a non-word character
			} else {
				c.unknown = true // unanchored: any character may follow the match
			}
		}
	}
	return c
}

// consumesAfter reports whether, after crossing the assertion at pc, the
func (v *validator) consumesAfter(pc int) bool {
	reach := v.fullReach([]int{int(v.prog.Inst[pc].Out)})
	for rp := 0; rp < len(v.prog.Inst); rp++ {
		if reach[rp] && isRuneInst(v.prog.Inst[rp].Op) && v.canReachMatch[rp] {
			return true
		}
	}
	return false
}

func (c charClass) merge(o charClass) charClass {
	return charClass{
		word:    c.word || o.word,
		nonword: c.nonword || o.nonword,
		unknown: c.unknown || o.unknown,
	}
}

// runeInstClass classifies which kinds of characters a rune instruction can
func runeInstClass(inst *syntax.Inst) charClass {
	if inst.Op == syntax.InstRuneAny || inst.Op == syntax.InstRuneAnyNotNL {
		return charClass{word: true, nonword: true}
	}
	runes := NormalizeRunePairs(inst.Rune)
	if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
		runes = ExpandFoldCase(runes)
	}
	var c charClass
	for i := 0; i+1 < len(runes); i += 2 {
		c = c.merge(rangeClass(runes[i], runes[i+1]))
	}
	return c
}

// wordRanges are the \b word characters ([-9A-Za-z_]), as inclusive ranges.
var wordRanges = [][2]rune{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}}

// rangeClass reports whether [lo, hi] contains word and/or non-word characters.
func rangeClass(lo, hi rune) charClass {
	var c charClass
	covered := lo // rune not yet known to be word
	for _, wr := range wordRanges {
		if lo <= wr[1] && hi >= wr[0] {
			c.word = true
		}
		if covered < wr[0] && covered <= hi {
			c.nonword = true // a gap below this word range is in [lo, hi]
		}
		if wr[1]+1 > covered {
			covered = wr[1] + 1
		}
	}
	if covered <= hi {
		c.nonword = true // extends past the last word range
	}
	return c
}

// fullReach returns which instructions are reachable from seeds, crossing
func (v *validator) fullReach(seeds []int) []bool {
	return v.reach(seeds, true)
}

// epsReach returns which instructions are reachable from seeds WITHOUT
func (v *validator) epsReach(seeds []int) []bool {
	return v.reach(seeds, false)
}

func (v *validator) reach(seeds []int, crossRunes bool) []bool {
	n := len(v.prog.Inst)
	seen := make([]bool, n)
	stack := append([]int(nil), seeds...)
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if pc < 0 || pc >= n || seen[pc] {
			continue
		}
		seen[pc] = true
		inst := &v.prog.Inst[pc]
		if !crossRunes && isRuneInst(inst.Op) {
			continue
		}
		for _, out := range instEdges(inst) {
			stack = append(stack, out)
		}
	}
	return seen
}

// instEdges returns the graph edges out of an instruction.
func instEdges(inst *syntax.Inst) []int {
	switch inst.Op {
	case syntax.InstAlt, syntax.InstAltMatch:
		return []int{int(inst.Out), int(inst.Arg)}
	case syntax.InstMatch, syntax.InstFail:
		return nil
	default:
		return []int{int(inst.Out)}
	}
}

func isRuneInst(op syntax.InstOp) bool {
	switch op {
	case syntax.InstRune, syntax.InstRune1, syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
		return true
	}
	return false
}

func (v *validator) modeName() string {
	if v.anchorStart {
		return "prefix"
	}
	return "contains"
}

func beginKind(op syntax.EmptyOp) string {
	if op&syntax.EmptyBeginText != 0 {
		return "text"
	}
	return "line"
}

func endKind(op syntax.EmptyOp) string {
	if op&syntax.EmptyEndText != 0 {
		return "text"
	}
	return "line"
}
