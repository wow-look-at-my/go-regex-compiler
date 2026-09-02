package dfa

// RuneRange is an inclusive [Lo, Hi] range of runes.
type RuneRange struct {
	Lo, Hi rune
}

// Transition maps a rune range to a target DFA state.
type Transition struct {
	Lo, Hi rune
	Next   int // target DFA state ID
}

// State is a single DFA state.
//
// Accept and AcceptAtEnd differ only under a word boundary, where whether a
// match ends here depends on the character that follows. Accept answers "a
// match ended on entry to this state", which prefix and contains scanning asks;
// AcceptAtEnd answers "a match ends with the input", which a full match asks,
// counting end-of-text as a non-word character. They are otherwise equal.
type State struct {
	ID          int
	Accept      bool         // a match ended on entry to this state
	AcceptAtEnd bool         // a match ends here when the input does
	Transitions []Transition // sorted by Lo
}

// DFA is a deterministic finite automaton.
type DFA struct {
	States []*State
	Start  int
}
