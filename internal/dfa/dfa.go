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

// Boundary classes for the rune on either side of a position, used to
// evaluate empty-width assertions (^ $ \A \z \b \B). For the rune BEFORE a
// position, ClassBegin means "start of text"; on the acceptance side the same
// slot describes the rune AFTER the position, where it means "end of text".
const (
	ClassOther = 0 // neither a word character nor '\n' (also: invalid byte)
	ClassWord  = 1 // ASCII word character [0-9A-Za-z_] (regexp \b semantics)
	ClassNL    = 2 // '\n'
	ClassBegin = 3 // before a position: begin of text; after: end of text
	NumClasses = 4
)

// AcceptMask records, per class of the NEXT rune (or end of text), whether a
// state accepts at the current position. A DFA built from a pattern without
// empty-width assertions has only AcceptAlways / AcceptNever states.
type AcceptMask uint8

const (
	AcceptOnOther AcceptMask = 1 << ClassOther
	AcceptOnWord  AcceptMask = 1 << ClassWord
	AcceptOnNL    AcceptMask = 1 << ClassNL
	AcceptOnEOT   AcceptMask = 1 << ClassBegin
	AcceptAlways  AcceptMask = AcceptOnOther | AcceptOnWord | AcceptOnNL | AcceptOnEOT
	AcceptNever   AcceptMask = 0
)

// State is a single DFA state.
type State struct {
	ID          int
	Accept      bool         // accepts when the input ends here; == AcceptOn&AcceptOnEOT != 0
	AcceptOn    AcceptMask   // acceptance per class of the next rune / end of text
	Transitions []Transition // sorted by Lo
}

// DFA is a deterministic finite automaton.
type DFA struct {
	States []*State
	Start  int // start state when matching begins at the start of the text

	// StartFor holds the start state per class of the rune immediately
	// preceding the match start, for unanchored (contains) matching;
	// StartFor[ClassBegin] == Start. All entries are equal when the pattern
	// has no empty-width assertions.
	StartFor [NumClasses]int

	// HasAssertions reports whether the compiled program contains any
	// empty-width assertion (^ $ \A \z \b \B, including (?m) variants).
	HasAssertions bool
}
