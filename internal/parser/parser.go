package parser

import (
	"fmt"
	"regexp/syntax"
)

// Result holds the parsed NFA program and metadata about the regex.
type Result struct {
	Prog      *syntax.Prog
	NumGroups int // Number of capture groups (not counting group 0 = whole match)
}

// Parse takes a regex pattern and returns a compiled NFA program.
// It uses Go's regexp/syntax package to parse, simplify, and compile the pattern.
func Parse(pattern string) (*syntax.Prog, error) {
	r, err := ParseResult(pattern)
	if err != nil {
		return nil, err
	}
	return r.Prog, nil
}

// ParseResult takes a regex pattern and returns the NFA program plus metadata
// including the number of capture groups.
func ParseResult(pattern string) (*Result, error) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, fmt.Errorf("parsing regex %q: %w", pattern, err)
	}

	numGroups := countCaptures(re)

	re = re.Simplify()
	prog, err := syntax.Compile(re)
	if err != nil {
		return nil, fmt.Errorf("compiling regex %q: %w", pattern, err)
	}
	return &Result{Prog: prog, NumGroups: numGroups}, nil
}

// countCaptures counts the number of capture groups in the regex AST.
func countCaptures(re *syntax.Regexp) int {
	max := 0
	if re.Op == syntax.OpCapture && re.Cap > max {
		max = re.Cap
	}
	for _, sub := range re.Sub {
		if n := countCaptures(sub); n > max {
			max = n
		}
	}
	return max
}
