package parser

import (
	"regexp/syntax"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"literal", "abc", false},
		{"character class", "[a-z]", false},
		{"dot", ".", false},
		{"star", "a*", false},
		{"plus", "a+", false},
		{"question", "a?", false},
		{"alternation", "a|b", false},
		{"grouping", "(ab)+", false},
		{"anchors", "^abc$", false},
		{"digit shorthand", `\d+`, false},
		{"word shorthand", `\w+`, false},
		{"space shorthand", `\s+`, false},
		{"counted repetition", "a{2,5}", false},
		{"case insensitive", "(?i)abc", false},
		{"complex", `[a-z]+@[a-z]+\.[a-z]{2,}`, false},
		{"empty", "", false},
		{"invalid unclosed bracket", "[abc", true},
		{"invalid bad escape", `\`, true},
		{"invalid bad repetition", "a{2,1}", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := Parse(tt.pattern)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse(%q) expected error, got nil", tt.pattern)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.pattern, err)
			}
			if prog == nil {
				t.Fatalf("Parse(%q) returned nil prog", tt.pattern)
			}
			if len(prog.Inst) == 0 {
				t.Errorf("Parse(%q) produced empty instruction list", tt.pattern)
			}
		})
	}
}

func TestParseProducesValidProgram(t *testing.T) {
	prog, err := Parse("[a-z]+")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the program has a valid start instruction
	if prog.Start < 0 || prog.Start >= len(prog.Inst) {
		t.Errorf("invalid Start index: %d (len=%d)", prog.Start, len(prog.Inst))
	}

	// Verify there's at least one match instruction
	hasMatch := false
	for _, inst := range prog.Inst {
		if inst.Op == syntax.InstMatch {
			hasMatch = true
			break
		}
	}
	if !hasMatch {
		t.Error("program has no InstMatch instruction")
	}
}
