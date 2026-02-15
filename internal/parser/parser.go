package parser

import (
	"fmt"
	"regexp/syntax"
)

// Parse takes a regex pattern and returns a compiled NFA program.
// It uses Go's regexp/syntax package to parse, simplify, and compile the pattern.
func Parse(pattern string) (*syntax.Prog, error) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, fmt.Errorf("parsing regex %q: %w", pattern, err)
	}
	re = re.Simplify()
	prog, err := syntax.Compile(re)
	if err != nil {
		return nil, fmt.Errorf("compiling regex %q: %w", pattern, err)
	}
	return prog, nil
}
