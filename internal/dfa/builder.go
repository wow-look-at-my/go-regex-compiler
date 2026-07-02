package dfa

import (
	"fmt"
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// MaxStates is the maximum number of DFA states before the builder returns an error.
const MaxStates = 10000

// Build converts an NFA program (from regexp/syntax.Compile) into a DFA.
//
// Empty-width assertions are handled by construction: an InstEmptyWidth is
// never crossed eagerly. Instead it stays PENDING in the subset, each DFA
// state additionally records the boundary class of the rune that led into it
// (word / newline / other / begin-of-text), and the pending assertions are
// resolved at step time against (before-class, class of the consumed rune).
// Acceptance likewise depends on the class of the NEXT rune, captured in the
// per-state AcceptOn mask; the alphabet is split at '\n' and the ASCII
// word-character ranges so every transition range resolves uniformly.
func Build(prog *syntax.Prog) (*DFA, error) {
	b := &builder{
		prog:     prog,
		stateMap: make(map[stateKey]int),
	}
	for i := range prog.Inst {
		if prog.Inst[i].Op == syntax.InstEmptyWidth {
			b.hasEmpty = true
			break
		}
	}
	b.dfa.HasAssertions = b.hasEmpty
	return b.build()
}

// stateKey identifies a DFA state: the serialized pending NFA set plus the
// boundary class before the current position (canonicalized to ClassOther
// when the set holds no pending assertions, so context-independent subgraphs
// dedupe).
type stateKey struct {
	set string
	ctx int
}

type builder struct {
	prog     *syntax.Prog
	hasEmpty bool // prog contains any InstEmptyWidth
	stateMap map[stateKey]int
	sets     [][]int // pending NFA set per DFA state ID
	ctxs     []int   // before-boundary class per DFA state ID
	dfa      DFA
}

func (b *builder) build() (*DFA, error) {
	startSet := b.pendingClosure([]int{b.prog.Start})
	// ClassBegin first so the begin-of-text start state keeps ID 0.
	b.dfa.Start = b.getOrCreateState(startSet, ClassBegin)
	b.dfa.StartFor[ClassBegin] = b.dfa.Start
	for _, cls := range []int{ClassOther, ClassWord, ClassNL} {
		b.dfa.StartFor[cls] = b.getOrCreateState(startSet, cls)
	}

	// Worklist: process each DFA state
	for i := 0; i < len(b.dfa.States); i++ {
		state := b.dfa.States[i]
		set, before := b.sets[i], b.ctxs[i]
		pendingEmpty := setHasEmptyWidth(b.prog, set)
		alphabet := b.computeAlphabet(b.resolveAll(set))

		for _, rr := range alphabet {
			live := set
			if pendingEmpty {
				// Every range is class-uniform (computeAlphabet splits at the
				// class edges whenever assertions exist), so the boundary
				// context of the whole range is that of its low rune.
				live = b.resolve(set, emptyOpsFor(before, classOfRune(rr.Lo)))
			}
			moved := b.move(live, rr.Lo, rr.Hi)
			if len(moved) == 0 {
				continue // dead transition, no need to record
			}
			nextSet := b.pendingClosure(moved)
			if len(nextSet) == 0 {
				continue
			}
			nextID := b.getOrCreateState(nextSet, classOfRune(rr.Lo))
			if len(b.dfa.States) > MaxStates {
				return nil, fmt.Errorf("DFA state limit exceeded (%d states); regex is too complex", MaxStates)
			}
			state.Transitions = append(state.Transitions, Transition{
				Lo:   rr.Lo,
				Hi:   rr.Hi,
				Next: nextID,
			})
		}
	}

	return &b.dfa, nil
}

// pendingClosure computes all NFA states reachable via unconditional epsilon
// transitions (InstAlt, InstAltMatch, InstNop, InstCapture). InstEmptyWidth
// is NOT crossed: it stays pending in the set until the surrounding runes are
// known (see resolve).
func (b *builder) pendingClosure(states []int) []int {
	visited := make(map[int]bool)
	stack := append([]int{}, states...)

	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[pc] {
			continue
		}
		if pc < 0 || pc >= len(b.prog.Inst) {
			continue
		}
		visited[pc] = true

		inst := &b.prog.Inst[pc]
		switch inst.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			stack = append(stack, int(inst.Out), int(inst.Arg))
		case syntax.InstNop, syntax.InstCapture:
			stack = append(stack, int(inst.Out))
		case syntax.InstEmptyWidth, syntax.InstMatch, syntax.InstFail,
			syntax.InstRune, syntax.InstRune1,
			syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
			// Terminal for the pending closure.
		}
	}

	return sortedSet(visited)
}

// resolve expands a pending set at a concrete boundary: an InstEmptyWidth
// whose assertions are all satisfied by ops is crossed, everything else is
// kept as-is.
func (b *builder) resolve(set []int, ops syntax.EmptyOp) []int {
	visited := make(map[int]bool)
	stack := append([]int{}, set...)

	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[pc] {
			continue
		}
		if pc < 0 || pc >= len(b.prog.Inst) {
			continue
		}
		visited[pc] = true

		inst := &b.prog.Inst[pc]
		switch inst.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			stack = append(stack, int(inst.Out), int(inst.Arg))
		case syntax.InstNop, syntax.InstCapture:
			stack = append(stack, int(inst.Out))
		case syntax.InstEmptyWidth:
			if syntax.EmptyOp(inst.Arg)&^ops == 0 {
				stack = append(stack, int(inst.Out))
			}
		}
	}

	return sortedSet(visited)
}

// resolveAll crosses every pending assertion regardless of context. Used to
// compute the alphabet: it over-approximates the reachable rune instructions.
func (b *builder) resolveAll(set []int) []int {
	if !b.hasEmpty {
		return set
	}
	const all = syntax.EmptyBeginLine | syntax.EmptyEndLine |
		syntax.EmptyBeginText | syntax.EmptyEndText |
		syntax.EmptyWordBoundary | syntax.EmptyNoWordBoundary
	return b.resolve(set, all)
}

func sortedSet(visited map[int]bool) []int {
	result := make([]int, 0, len(visited))
	for pc := range visited {
		result = append(result, pc)
	}
	sort.Ints(result)
	return result
}

// classOfRune classifies a rune for assertion evaluation. Word characters
// follow regexp's ASCII-only \b semantics (syntax.IsWordChar).
func classOfRune(r rune) int {
	switch {
	case r == '\n':
		return ClassNL
	case syntax.IsWordChar(r):
		return ClassWord
	default:
		return ClassOther
	}
}

// classRune returns a representative rune for a boundary class, with
// ClassBegin mapping to -1 (the text edge, as syntax.EmptyOpContext expects).
func classRune(class int) rune {
	switch class {
	case ClassWord:
		return 'a'
	case ClassNL:
		return '\n'
	case ClassBegin:
		return -1
	default:
		return ' '
	}
}

// emptyOpsFor returns the set of empty-width assertions satisfied at a
// boundary whose neighbouring runes have the given classes. Delegates to
// regexp/syntax for exact stdlib parity.
func emptyOpsFor(before, after int) syntax.EmptyOp {
	return syntax.EmptyOpContext(classRune(before), classRune(after))
}

func setHasEmptyWidth(prog *syntax.Prog, set []int) bool {
	for _, pc := range set {
		if pc >= 0 && pc < len(prog.Inst) && prog.Inst[pc].Op == syntax.InstEmptyWidth {
			return true
		}
	}
	return false
}

func containsMatch(prog *syntax.Prog, set []int) bool {
	for _, pc := range set {
		if pc >= 0 && pc < len(prog.Inst) && prog.Inst[pc].Op == syntax.InstMatch {
			return true
		}
	}
	return false
}

// move computes which NFA states are reached from the given state set
// by consuming a rune in the range [lo, hi].
func (b *builder) move(states []int, lo, hi rune) []int {
	var result []int
	for _, pc := range states {
		if pc < 0 || pc >= len(b.prog.Inst) {
			continue
		}
		inst := &b.prog.Inst[pc]
		switch inst.Op {
		case syntax.InstRune1:
			r := inst.Rune[0]
			if lo <= r && r <= hi {
				result = append(result, int(inst.Out))
			}
			if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
				for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
					if lo <= f && f <= hi {
						result = append(result, int(inst.Out))
						break
					}
				}
			}
		case syntax.InstRune:
			if b.runeMatchesRange(inst, lo, hi) {
				result = append(result, int(inst.Out))
			}
		case syntax.InstRuneAny:
			result = append(result, int(inst.Out))
		case syntax.InstRuneAnyNotNL:
			// Matches any rune except \n. The alphabet is partitioned at \n
			// boundaries, so a range either is exactly [\n,\n] or excludes it.
			if !(lo <= '\n' && '\n' <= hi) {
				result = append(result, int(inst.Out))
			}
		}
	}
	return result
}

// runeMatchesRange checks if an InstRune instruction can match any rune in [lo, hi].
func (b *builder) runeMatchesRange(inst *syntax.Inst, lo, hi rune) bool {
	runes := NormalizeRunePairs(inst.Rune)
	foldCase := syntax.Flags(inst.Arg)&syntax.FoldCase != 0

	if foldCase {
		runes = ExpandFoldCase(runes)
	}

	for i := 0; i < len(runes); i += 2 {
		rLo, rHi := runes[i], runes[i+1]
		if lo <= rHi && hi >= rLo {
			return true
		}
	}
	return false
}

// NormalizeRunePairs ensures the rune slice is in pair format [lo, hi, ...].
// When Rune has odd length (e.g., a single rune from a literal with FoldCase),
// each unpaired rune is treated as a [r, r] range.
func NormalizeRunePairs(runes []rune) []rune {
	if len(runes)%2 == 0 {
		return runes
	}
	// Single rune: treat as [r, r]
	if len(runes) == 1 {
		return []rune{runes[0], runes[0]}
	}
	// Odd length > 1: shouldn't happen, but handle gracefully
	result := make([]rune, 0, len(runes)+1)
	result = append(result, runes...)
	result = append(result, runes[len(runes)-1])
	return result
}

// computeAlphabet computes the minimal set of non-overlapping rune ranges
// that partition the input alphabet for the given NFA state set. When the
// program contains empty-width assertions, the partition is additionally
// split at '\n' and the ASCII word-character ranges so that assertion
// resolution (and the boundary class recorded on the target state) is uniform
// across each range.
func (b *builder) computeAlphabet(states []int) []RuneRange {
	var boundaries []rune

	for _, pc := range states {
		if pc < 0 || pc >= len(b.prog.Inst) {
			continue
		}
		inst := &b.prog.Inst[pc]
		switch inst.Op {
		case syntax.InstRune1:
			r := inst.Rune[0]
			boundaries = append(boundaries, r, r+1)
			if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
				for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
					boundaries = append(boundaries, f, f+1)
				}
			}
		case syntax.InstRune:
			runes := NormalizeRunePairs(inst.Rune)
			if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
				runes = ExpandFoldCase(runes)
			}
			for i := 0; i < len(runes); i += 2 {
				boundaries = append(boundaries, runes[i], runes[i+1]+1)
			}
		case syntax.InstRuneAny:
			boundaries = append(boundaries, 0, unicode.MaxRune+1)
		case syntax.InstRuneAnyNotNL:
			boundaries = append(boundaries, 0, '\n', '\n'+1, unicode.MaxRune+1)
		}
	}

	if len(boundaries) == 0 {
		return nil
	}

	if b.hasEmpty {
		boundaries = append(boundaries,
			'\n', '\n'+1, '0', '9'+1, 'A', 'Z'+1, '_', '_'+1, 'a', 'z'+1)
	}

	// Sort and deduplicate boundaries
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i] < boundaries[j] })
	deduped := boundaries[:0]
	for i, b := range boundaries {
		if i == 0 || b != boundaries[i-1] {
			deduped = append(deduped, b)
		}
	}
	boundaries = deduped

	// Create ranges between boundaries
	var ranges []RuneRange
	for i := 0; i < len(boundaries)-1; i++ {
		lo := boundaries[i]
		hi := boundaries[i+1] - 1
		if lo > hi {
			continue
		}
		ranges = append(ranges, RuneRange{Lo: lo, Hi: hi})
	}

	return ranges
}

// getOrCreateState returns the DFA state ID for the given pending NFA set and
// before-boundary class, creating a new DFA state if necessary.
func (b *builder) getOrCreateState(nfaStates []int, ctx int) int {
	if !setHasEmptyWidth(b.prog, nfaStates) {
		// The boundary context only influences pending assertions;
		// canonicalize so assertion-free subgraphs dedupe across contexts.
		ctx = ClassOther
	}
	key := stateKey{set: serializeStateSet(nfaStates), ctx: ctx}
	if id, ok := b.stateMap[key]; ok {
		return id
	}

	id := len(b.dfa.States)
	b.stateMap[key] = id
	b.sets = append(b.sets, nfaStates)
	b.ctxs = append(b.ctxs, ctx)

	acceptOn := b.acceptMask(nfaStates, ctx)
	b.dfa.States = append(b.dfa.States, &State{
		ID:       id,
		Accept:   acceptOn&AcceptOnEOT != 0,
		AcceptOn: acceptOn,
	})
	return id
}

// acceptMask computes, per class of the next rune (or end of text), whether
// the pending set reaches InstMatch at the current boundary.
func (b *builder) acceptMask(set []int, ctx int) AcceptMask {
	if !setHasEmptyWidth(b.prog, set) {
		if containsMatch(b.prog, set) {
			return AcceptAlways
		}
		return AcceptNever
	}
	var m AcceptMask
	for _, after := range []int{ClassOther, ClassWord, ClassNL, ClassBegin} {
		if containsMatch(b.prog, b.resolve(set, emptyOpsFor(ctx, after))) {
			m |= 1 << after
		}
	}
	return m
}

func serializeStateSet(states []int) string {
	var sb strings.Builder
	for i, s := range states {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.Itoa(s))
	}
	return sb.String()
}

// Unicode simple case folding only exists inside [minFold, maxFold] (the same
// band regexp/syntax uses); runes outside it fold only to themselves.
const (
	minFold = 0x0041
	maxFold = 0x1e943
)

// ExpandFoldCase expands rune range pairs to include all case-folded
// equivalents. The scan is clamped to the foldable band, which bounds the
// work without dropping any fold (a previous version silently capped
// expansion at 256 runes per range, losing folds at offsets >= 256).
func ExpandFoldCase(runes []rune) []rune {
	var expanded []rune
	expanded = append(expanded, runes...)

	for i := 0; i < len(runes); i += 2 {
		lo, hi := runes[i], runes[i+1]
		if lo < minFold {
			lo = minFold
		}
		if hi > maxFold {
			hi = maxFold
		}
		for r := lo; r <= hi; r++ {
			for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
				expanded = append(expanded, f, f)
			}
		}
	}

	return mergeRuneRanges(expanded)
}

// mergeRuneRanges sorts and merges overlapping rune range pairs.
func mergeRuneRanges(runes []rune) []rune {
	if len(runes) < 2 {
		return runes
	}

	type pair struct{ lo, hi rune }
	pairs := make([]pair, len(runes)/2)
	for i := 0; i < len(runes); i += 2 {
		pairs[i/2] = pair{runes[i], runes[i+1]}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].lo != pairs[j].lo {
			return pairs[i].lo < pairs[j].lo
		}
		return pairs[i].hi < pairs[j].hi
	})

	merged := []pair{pairs[0]}
	for _, p := range pairs[1:] {
		last := &merged[len(merged)-1]
		if p.lo <= last.hi+1 {
			if p.hi > last.hi {
				last.hi = p.hi
			}
		} else {
			merged = append(merged, p)
		}
	}

	result := make([]rune, len(merged)*2)
	for i, p := range merged {
		result[i*2] = p.lo
		result[i*2+1] = p.hi
	}
	return result
}
