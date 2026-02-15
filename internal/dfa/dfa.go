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
type State struct {
	ID          int
	Accept      bool         // true if this is a match/accepting state
	Transitions []Transition // sorted by Lo
}

// DFA is a deterministic finite automaton.
type DFA struct {
	States []*State
	Start  int
}
