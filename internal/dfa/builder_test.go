package dfa

import (
	"regexp/syntax"
	"testing"
)

func parse(t *testing.T, pattern string) *syntax.Prog {
	t.Helper()
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		t.Fatalf("syntax.Parse(%q): %v", pattern, err)
	}
	re = re.Simplify()
	prog, err := syntax.Compile(re)
	if err != nil {
		t.Fatalf("syntax.Compile(%q): %v", pattern, err)
	}
	return prog
}

func TestBuildSimpleLiteral(t *testing.T) {
	prog := parse(t, "abc")
	d, err := Build(prog)
	if err != nil {
		t.Fatal(err)
	}

	// Should have states for: start, after 'a', after 'b', after 'c' (accept)
	if len(d.States) < 4 {
		t.Errorf("expected at least 4 states for 'abc', got %d", len(d.States))
	}

	// Exactly one accepting state
	acceptCount := 0
	for _, s := range d.States {
		if s.Accept {
			acceptCount++
		}
	}
	if acceptCount != 1 {
		t.Errorf("expected 1 accepting state, got %d", acceptCount)
	}
}

func TestBuildCharacterClass(t *testing.T) {
	prog := parse(t, "[a-z]")
	d, err := Build(prog)
	if err != nil {
		t.Fatal(err)
	}

	// Should have at least 2 states: start and accept
	if len(d.States) < 2 {
		t.Errorf("expected at least 2 states for '[a-z]', got %d", len(d.States))
	}

	// Start state should have at least one transition
	startState := d.States[d.Start]
	if len(startState.Transitions) == 0 {
		t.Error("start state has no transitions")
	}
}

func TestBuildAlternation(t *testing.T) {
	prog := parse(t, "a|b")
	d, err := Build(prog)
	if err != nil {
		t.Fatal(err)
	}

	// Start state should have at least one transition covering 'a' and 'b'
	// (may be merged into a single range [a,b] by alphabet partitioning)
	startState := d.States[d.Start]
	if len(startState.Transitions) == 0 {
		t.Error("start state has no transitions for 'a|b'")
	}

	// Both 'a' and 'b' should lead to accepting states
	hasAccept := false
	for _, s := range d.States {
		if s.Accept {
			hasAccept = true
			break
		}
	}
	if !hasAccept {
		t.Error("no accepting state found for 'a|b'")
	}
}

func TestBuildStar(t *testing.T) {
	prog := parse(t, "a*")
	d, err := Build(prog)
	if err != nil {
		t.Fatal(err)
	}

	// Start state should be accepting (empty string matches a*)
	startState := d.States[d.Start]
	if !startState.Accept {
		t.Error("start state should be accepting for 'a*'")
	}
}

func TestBuildPlus(t *testing.T) {
	prog := parse(t, "a+")
	d, err := Build(prog)
	if err != nil {
		t.Fatal(err)
	}

	// Start state should NOT be accepting
	startState := d.States[d.Start]
	if startState.Accept {
		t.Error("start state should not be accepting for 'a+'")
	}

	// There should be an accepting state reachable from start
	hasAccept := false
	for _, s := range d.States {
		if s.Accept {
			hasAccept = true
			break
		}
	}
	if !hasAccept {
		t.Error("no accepting state found for 'a+'")
	}
}

func TestBuildEmptyRegex(t *testing.T) {
	prog := parse(t, "")
	d, err := Build(prog)
	if err != nil {
		t.Fatal(err)
	}

	// Start state should be accepting (empty regex matches empty string)
	startState := d.States[d.Start]
	if !startState.Accept {
		t.Error("start state should be accepting for empty regex")
	}
}

func TestBuildDot(t *testing.T) {
	prog := parse(t, ".")
	d, err := Build(prog)
	if err != nil {
		t.Fatal(err)
	}

	// Should have at least 2 states
	if len(d.States) < 2 {
		t.Errorf("expected at least 2 states for '.', got %d", len(d.States))
	}

	// Start state should have transitions (matches any char except newline)
	startState := d.States[d.Start]
	if len(startState.Transitions) == 0 {
		t.Error("start state has no transitions for '.'")
	}
}

func TestBuildComplex(t *testing.T) {
	patterns := []string{
		`[a-z]+@[a-z]+\.[a-z]{2,}`,
		`(foo|bar)baz`,
		`\d{3}-\d{2}-\d{4}`,
		`[A-Za-z_][A-Za-z0-9_]*`,
		`(a|b)*c`,
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			prog := parse(t, pattern)
			d, err := Build(prog)
			if err != nil {
				t.Fatalf("Build(%q): %v", pattern, err)
			}
			if len(d.States) == 0 {
				t.Error("DFA has no states")
			}
		})
	}
}

func TestBuildCaseInsensitive(t *testing.T) {
	prog := parse(t, "(?i)abc")
	d, err := Build(prog)
	if err != nil {
		t.Fatal(err)
	}

	if len(d.States) < 4 {
		t.Errorf("expected at least 4 states for '(?i)abc', got %d", len(d.States))
	}

	// Should have exactly one accepting state
	acceptCount := 0
	for _, s := range d.States {
		if s.Accept {
			acceptCount++
		}
	}
	if acceptCount != 1 {
		t.Errorf("expected 1 accepting state, got %d", acceptCount)
	}
}

func TestEpsilonClosure(t *testing.T) {
	prog := parse(t, "a")
	b := &builder{
		prog:     prog,
		stateMap: make(map[string]int),
	}

	closure := b.epsilonClosure([]int{prog.Start})
	if len(closure) == 0 {
		t.Error("epsilon closure of start state is empty")
	}

	// Should contain the start state
	found := false
	for _, pc := range closure {
		if pc == prog.Start {
			found = true
			break
		}
	}
	if !found {
		t.Error("epsilon closure doesn't contain start state")
	}
}

func TestMergeRuneRanges(t *testing.T) {
	tests := []struct {
		name   string
		input  []rune
		expect []rune
	}{
		{"no overlap", []rune{'a', 'c', 'e', 'g'}, []rune{'a', 'c', 'e', 'g'}},
		{"overlap", []rune{'a', 'd', 'c', 'f'}, []rune{'a', 'f'}},
		{"adjacent", []rune{'a', 'c', 'd', 'f'}, []rune{'a', 'f'}},
		{"single", []rune{'a', 'z'}, []rune{'a', 'z'}},
		{"contained", []rune{'a', 'z', 'c', 'f'}, []rune{'a', 'z'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeRuneRanges(tt.input)
			if len(result) != len(tt.expect) {
				t.Errorf("got %v, want %v", result, tt.expect)
				return
			}
			for i := range result {
				if result[i] != tt.expect[i] {
					t.Errorf("got %v, want %v", result, tt.expect)
					return
				}
			}
		})
	}
}
