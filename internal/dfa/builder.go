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

// Build converts an NFA program (from regexp/syntax.Compile) into a DFA that
// matches the pattern anchored at the current position (full/prefix modes).
func Build(prog *syntax.Prog) (*DFA, error) {
	b := &builder{
		prog:     prog,
		stateMap: make(map[string]int),
	}
	return b.build()
}

// BuildSearch converts an NFA program into an unanchored SEARCH DFA: the start
// closure is folded into every state, so a single left-to-right pass tracks
// every possible match start simultaneously (the classic product construction
// for ".*pattern"). An accepting state means "some substring ending here
// matches". Ranges with no recorded transition restart the scan: the correct
// target is exactly the start state, which codegen emits as the switch
// default, so those transitions are omitted here.
func BuildSearch(prog *syntax.Prog) (*DFA, error) {
	b := &builder{
		prog:     prog,
		stateMap: make(map[string]int),
		search:   true,
	}
	return b.build()
}

type builder struct {
	prog     *syntax.Prog
	stateMap map[string]int // serialized NFA state set -> DFA state ID
	nfaSets  [][]int        // DFA state ID -> sorted NFA state set (parallel to dfa.States)
	dfa      DFA
	search   bool  // seed the start closure into every state (unanchored search)
	startSet []int // epsilon closure of prog.Start

	// Word-boundary construction only (see wordboundary.go): states hold the
	// NFA set before closure, plus the class of the character that preceded it.
	startPending []int
	prevWord     []bool

	// Scratch state for epsilonClosure, reused across calls: visited[pc] holds
	// the generation of the last closure that reached pc.
	visited []int
	gen     int
	stack   []int
}

func (b *builder) build() (*DFA, error) {
	if hasWordBoundary(b.prog) {
		return b.buildWordAware()
	}
	b.startSet = b.epsilonClosure([]int{b.prog.Start})
	b.getOrCreateState(b.startSet)
	b.dfa.Start = 0

	// Worklist: process each DFA state
	for i := 0; i < len(b.dfa.States); i++ {
		state := b.dfa.States[i]
		nfaStates := b.nfaStatesForDFA(state.ID)
		alphabet := b.computeAlphabet(nfaStates)

		for _, rr := range alphabet {
			moved := b.move(nfaStates, rr.Lo, rr.Hi)
			nextSet := b.epsilonClosure(moved)
			if b.search {
				nextSet = unionSorted(nextSet, b.startSet)
			}
			if len(nextSet) == 0 {
				continue // dead transition, no need to record
			}
			nextID := b.getOrCreateState(nextSet)
			if len(b.dfa.States) > MaxStates {
				return nil, fmt.Errorf("DFA state limit exceeded (%d states); regex is too complex", MaxStates)
			}
			if b.search && nextID == b.dfa.Start {
				continue // restart transition; codegen's default already does this
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

// unionSorted merges two sorted int slices without duplicates.
func unionSorted(a, b []int) []int {
	result := make([]int, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			result = append(result, a[i])
			i++
		case a[i] > b[j]:
			result = append(result, b[j])
			j++
		default:
			result = append(result, a[i])
			i++
			j++
		}
	}
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)
	return result
}

// nfaStatesForDFA returns the NFA state set for a given DFA state ID.
func (b *builder) nfaStatesForDFA(id int) []int {
	return b.nfaSets[id]
}

// epsilonClosure computes all NFA states reachable from the given states
// via epsilon transitions (InstAlt, InstAltMatch, InstNop, InstCapture, InstEmptyWidth).
func (b *builder) epsilonClosure(states []int) []int {
	if b.visited == nil {
		b.visited = make([]int, len(b.prog.Inst))
	}
	b.gen++
	stack := append(b.stack[:0], states...)

	var result []int
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if pc < 0 || pc >= len(b.prog.Inst) {
			continue
		}
		if b.visited[pc] == b.gen {
			continue
		}
		b.visited[pc] = b.gen
		result = append(result, pc)

		inst := &b.prog.Inst[pc]
		switch inst.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			stack = append(stack, int(inst.Out), int(inst.Arg))
		case syntax.InstNop, syntax.InstCapture:
			stack = append(stack, int(inst.Out))
		case syntax.InstEmptyWidth:
			// For full-match mode, we follow through empty-width assertions.
			// ^/$ are implicitly satisfied since we match the entire string.
			stack = append(stack, int(inst.Out))
		case syntax.InstMatch, syntax.InstFail,
			syntax.InstRune, syntax.InstRune1,
			syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
			// These are terminal for epsilon closure
		}
	}
	b.stack = stack[:0]

	sort.Ints(result)
	return result
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
			// Matches any rune except \n
			if !(lo <= '\n' && '\n' <= hi) {
				result = append(result, int(inst.Out))
			} else if lo < '\n' || hi > '\n' {
				// The range partially overlaps \n. Since we partition cleanly,
				// this case means our partition range doesn't fully contain \n only,
				// so we can include it (the partition range won't contain \n).
				// Actually, if the partition includes \n, we should NOT match.
				// But if the partition is wider, the partitioning should have split
				// at \n boundaries. So if lo <= '\n' && '\n' <= hi, skip it.
				// This case is already handled above.
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
// each unpaired rune is treated as a [r, r] range. Exported for the submatch
// codegen alongside ExpandFoldCase.
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
// that partition the input alphabet for the given NFA state set.
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

// getOrCreateState returns the DFA state ID for the given NFA state set,
// creating a new DFA state if necessary.
func (b *builder) getOrCreateState(nfaStates []int) int {
	key := serializeStateSet(nfaStates)
	if id, ok := b.stateMap[key]; ok {
		return id
	}

	id := len(b.dfa.States)
	b.stateMap[key] = id
	b.nfaSets = append(b.nfaSets, nfaStates)

	accept := false
	for _, pc := range nfaStates {
		if pc >= 0 && pc < len(b.prog.Inst) && b.prog.Inst[pc].Op == syntax.InstMatch {
			accept = true
			break
		}
	}

	b.dfa.States = append(b.dfa.States, &State{
		ID:          id,
		Accept:      accept,
		AcceptAtEnd: accept,
	})
	return id
}

func serializeStateSet(states []int) string {
	var sb strings.Builder
	sb.Grow(len(states) * 4)
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
// expansion at 256 runes per range, losing folds at offsets >= 256, e.g.
// U+212A KELVIN SIGN inside a wide folded range). Exported for the submatch
// codegen, which expands fold orbits into the emitted NFA rune tables.
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
