package parser

import (
	"fmt"
	"regexp/syntax"
)

// Result holds the parsed NFA program and metadata about the regex.
type Result struct {
	Prog      *syntax.Prog
	NumGroups int // Number of capture groups (not counting group = whole match)
	// GroupNames holds the name of each capture group, indexed by group number.
	GroupNames []string
}

// Parse takes a regex pattern and returns a compiled NFA program.
func Parse(pattern string) (*syntax.Prog, error) {
	r, err := ParseResult(pattern)
	if err != nil {
		return nil, err
	}
	return r.Prog, nil
}

// ParseResult takes a regex pattern and returns the NFA program plus metadata
func ParseResult(pattern string) (*Result, error) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, fmt.Errorf("parsing regex %q: %w", pattern, err)
	}

	numGroups := countCaptures(re)

	// Collect group names from the un-simplified AST (Simplify may rewrite
	names := make([]string, numGroups+1)
	collectGroupNames(re, names)

	re = re.Simplify()
	prog, err := syntax.Compile(re)
	if err != nil {
		return nil, fmt.Errorf("compiling regex %q: %w", pattern, err)
	}
	return &Result{Prog: prog, NumGroups: numGroups, GroupNames: names}, nil
}

// collectGroupNames walks the regex AST and records each capture group's name
func collectGroupNames(re *syntax.Regexp, names []string) {
	if re.Op == syntax.OpCapture && re.Cap >= 0 && re.Cap < len(names) {
		names[re.Cap] = re.Name
	}
	for _, sub := range re.Sub {
		collectGroupNames(sub, names)
	}
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
