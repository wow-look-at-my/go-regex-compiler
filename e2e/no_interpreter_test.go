package e2e

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

// interpreterMarkers are substrings that only appear in a run-time NFA
// interpreter: an instruction/thread table, an op-dispatch switch, an
// epsilon-closure helper, or a scratch pool. A compiled matcher (one-pass or
// TDFA) contains none of them — it is pure `switch state` control flow.
var interpreterMarkers = []string{
	"sync.Pool",     // scratch/object pool
	"addThread",     // epsilon-closure that walks the program
	"Inst{",         // package-level instruction table literal
	"Inst struct",   // instruction type
	"Thread struct", // live-position list element
	"EmptyOpsAt",    // per-position empty-width evaluation
	"RuneMatch",     // interpreter rune-class dispatch
	"StartPC",       // program-counter seed of a simulation
	".op {",         // switch on an instruction opcode
	".op ==",        // opcode comparison
}

// TestNoInterpreterInFixtures asserts that EVERY generated fixture matcher in
// this package compiles to a state machine with no interpreter. This is the
// mandate: no generated Find function walks a program, a thread list, or a
// pool at run time.
func TestNoInterpreterInFixtures(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.Nil(t, err)

	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "gen_") || !strings.HasSuffix(name, ".go") {
			continue
		}
		data, err := os.ReadFile(name)
		require.Nil(t, err)

		src := string(data)
		scanned++
		for _, m := range interpreterMarkers {
			assert.NotContains(t, src, m)

		}
	}
	require.NotEqual(t, 0, scanned)

	t.Logf("scanned %d generated fixtures; all interpreter-free", scanned)
}
