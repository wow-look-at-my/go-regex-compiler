package dfa

import (
	"fmt"
	"regexp/syntax"
	"sort"
	"unicode"
)

// hasWordBoundary reports whether prog contains a \b or \B. Without either, the
func hasWordBoundary(prog *syntax.Prog) bool {
	for i := range prog.Inst {
		inst := &prog.Inst[i]
		if inst.Op != syntax.InstEmptyWidth {
			continue
		}
		if syntax.EmptyOp(inst.Arg)&(syntax.EmptyWordBoundary|syntax.EmptyNoWordBoundary) != 0 {
			return true
		}
	}
	return false
}

// isWordRune reports whether r is of [-9A-Za-z_], the class \b compares.
func isWordRune(r rune) bool {
	return r == '_' ||
		(r >= '0' && r <= '9') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z')
}

// buildWordAware is the subset construction for a pattern with a word boundary.
func (b *builder) buildWordAware() (*DFA, error) {
	b.startPending = []int{b.prog.Start}
	start := b.getOrCreateWordState(b.startPending, false,
		b.closureHasMatch(b.startPending, false, true) &&
			b.closureHasMatch(b.startPending, false, false),
		b.closureHasMatch(b.startPending, false, false))
	b.dfa.Start = start

	for i := 0; i < len(b.dfa.States); i++ {
		state := b.dfa.States[i]
		pending := b.nfaSets[state.ID]
		prevWord := b.prevWord[state.ID]

		// The alphabet is needed before the closure that depends on it, so it
		both := unionSorted(
			b.closureWB(pending, prevWord, true),
			b.closureWB(pending, prevWord, false))
		alphabet := b.wordSplitAlphabet(b.computeAlphabet(both))

		for _, rr := range alphabet {
			curWord := isWordRune(rr.Lo)
			closed := b.closureWB(pending, prevWord, curWord)
			moved := sortUnique(b.move(closed, rr.Lo, rr.Hi))
			if b.search {
				moved = unionSorted(moved, b.startPending)
			}
			// Acceptance on entry comes from either of these. A match gated on a trailing
			accept := containsMatch(b.prog, closed) ||
				(b.closureHasMatch(moved, curWord, true) && b.closureHasMatch(moved, curWord, false))
			if len(moved) == 0 && !accept {
				continue
			}
			next := b.getOrCreateWordState(moved, curWord, accept,
				b.closureHasMatch(moved, curWord, false))
			if len(b.dfa.States) > MaxStates {
				return nil, fmt.Errorf("DFA state limit exceeded (%d states); regex is too complex", MaxStates)
			}
			state.Transitions = append(state.Transitions, Transition{Lo: rr.Lo, Hi: rr.Hi, Next: next})
		}
		sort.Slice(state.Transitions, func(i, j int) bool {
			return state.Transitions[i].Lo < state.Transitions[j].Lo
		})
	}
	return &b.dfa, nil
}

// closureWB is the epsilon closure with neighbours known, so a
func (b *builder) closureWB(pending []int, prevWord, nextWord bool) []int {
	atBoundary := prevWord != nextWord
	if b.visited == nil {
		b.visited = make([]int, len(b.prog.Inst))
	}
	b.gen++
	stack := append(b.stack[:0], pending...)

	var result []int
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if pc < 0 || pc >= len(b.prog.Inst) || b.visited[pc] == b.gen {
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
			op := syntax.EmptyOp(inst.Arg)
			if op&syntax.EmptyWordBoundary != 0 && !atBoundary {
				continue
			}
			if op&syntax.EmptyNoWordBoundary != 0 && atBoundary {
				continue
			}
			stack = append(stack, int(inst.Out))
		}
	}
	b.stack = stack[:0]
	sort.Ints(result)
	return result
}

func (b *builder) closureHasMatch(pending []int, prevWord, nextWord bool) bool {
	return containsMatch(b.prog, b.closureWB(pending, prevWord, nextWord))
}

func containsMatch(prog *syntax.Prog, set []int) bool {
	for _, pc := range set {
		if pc >= 0 && pc < len(prog.Inst) && prog.Inst[pc].Op == syntax.InstMatch {
			return true
		}
	}
	return false
}

// wordSplitAlphabet cuts ranges at the word-class edges so each range is
func (b *builder) wordSplitAlphabet(consuming []RuneRange) []RuneRange {
	bounds := []rune{'0', '9' + 1, 'A', 'Z' + 1, '_', '_' + 1, 'a', 'z' + 1}
	cuts := []rune{0, unicode.MaxRune + 1}
	for _, rr := range consuming {
		cuts = append(cuts, rr.Lo, rr.Hi+1)
	}
	for _, c := range bounds {
		cuts = append(cuts, c)
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i] < cuts[j] })

	// Neighbouring ranges of the same word class answer \b identically, so
	consumed := func(r rune) bool {
		for _, rr := range consuming {
			if rr.Lo <= r && r <= rr.Hi {
				return true
			}
		}
		return false
	}

	var out []RuneRange
	for i := 0; i+1 < len(cuts); i++ {
		lo, hi := cuts[i], cuts[i+1]-1
		if lo > hi {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Hi+1 == lo &&
			isWordRune(out[n-1].Lo) == isWordRune(lo) &&
			!consumed(out[n-1].Lo) && !consumed(lo) {
			out[n-1].Hi = hi
			continue
		}
		out = append(out, RuneRange{Lo: lo, Hi: hi})
	}
	return out
}

func sortUnique(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	sort.Ints(in)
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// getOrCreateWordState keys a state on everything that decides its behavior.
func (b *builder) getOrCreateWordState(pending []int, prevWord, accept, acceptAtEnd bool) int {
	key := serializeStateSet(pending) + "|" +
		boolKey(prevWord) + boolKey(accept) + boolKey(acceptAtEnd)
	if id, ok := b.stateMap[key]; ok {
		return id
	}
	id := len(b.dfa.States)
	b.stateMap[key] = id
	b.nfaSets = append(b.nfaSets, pending)
	b.prevWord = append(b.prevWord, prevWord)
	b.dfa.States = append(b.dfa.States, &State{ID: id, Accept: accept, AcceptAtEnd: acceptAtEnd})
	return id
}

func boolKey(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
