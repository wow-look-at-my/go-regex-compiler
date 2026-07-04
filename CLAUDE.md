# go-regex-compiler

Regex-to-Go code generator. Compiles regular expressions into pure Go functions via NFA-to-DFA conversion.

## Build and test

```bash
go-toolchain
```

This runs mod tidy, tests, coverage, and builds. Do not use `go build` or `go test` directly.

## Project structure

- `cmd/go-regex-compiler/` -- CLI entry point (cobra)
- `internal/parser/` -- regex parsing via `regexp/syntax`, extracts NFA + capture group info
- `internal/dfa/` -- NFA-to-DFA subset construction: `Build` (anchored, for
  full/prefix), `BuildSearch` (unanchored search DFA with the start closure
  folded into every state, for contains), and `ValidateAssertions` (rejects
  empty-width assertions the DFA cannot honor for the given anchoring)
- `internal/codegen/` -- Go code generation from DFA, includes templates for all match modes
- `match/` -- runtime library imported by generated code (provides `InRange` helper)
- `e2e/` -- integration and comparison tests (uses pre-generated fixtures like `bench/`)
- `bench/` -- generated-code-vs-regexp benchmarks (run `go generate ./bench/...` to create fixtures)

## Architecture

The pipeline has three stages:

1. **parser.ParseResult** parses the regex string into an NFA program (`regexp/syntax.Compile`)
2. **dfa.Build** (full/prefix) or **dfa.BuildSearch** (contains) converts the
   NFA to a DFA via subset construction, after **dfa.ValidateAssertions**
   rejects `^ $ \b \B` placements the DFA cannot evaluate for the mode
3. **codegen.Generate** emits Go source from the DFA using `text/template`

Match modes (full/prefix/contains) and ASCII vs Unicode are handled by separate templates in `internal/codegen/templates.go`.

Submatch extraction ALWAYS compiles to a straight-line `switch state` machine —
there is NO run-time interpreter in generated code. Two compiled paths are tried
in order (`buildSubmatchContext` in `internal/codegen/submatch.go`); if neither
applies the generator returns an error rather than emit an interpreter:

- **Compiled one-pass path** (`internal/codegen/onepass.go`, `onepass_emit.go`, `templates_onepass.go`): when the pattern is one-pass (every input rune deterministically selects the next step), `buildCapDFA` constructs a capture-annotated DFA — states are sets of NFA `(consuming-inst, pending-captures, empty-width-gate)` configs, transitions carry the capture-slot writes crossed to reach them — and the `<func>Index` core is emitted as a `switch state` automaton with inline `caps[k] = pos` writes. Empty-width assertions are compiled: text anchors (`^ $ \A \z`) fold away as always-satisfied for a full match, a leading/trailing `\b`/`\B` becomes a one-line word-boundary gate on the first/last rune, and a provably-always-true *interior* `\b`/`\B` (one `dfa.ValidateAssertions` has proven always holds, e.g. `\B` between two word chars) folds to a no-op — so `(a\Bb)` compiles exactly like `(ab)`.
- **Compiled TDFA path** (`internal/codegen/tdfa.go`, `tdfa_emit.go`, `templates_tdfa.go`): for patterns with genuine capture AMBIGUITY the one-pass path rejects — adjacent greedy stars (`(a*)(a*)`), overlapping alternation (`(a|ab)(a*)`), nested stars (`(a*)*`), optional-then-star (`(a?)(a*)`), `(?i)` fold classes, and ambiguous bodies carrying an always-true interior `\b`/`\B` such as `(\w+\B\w+)` — `buildTDFA` determinizes the marker-annotated NFA into a tagged DFA. Each live config owns an isolated block of an integer register file; transitions carry fixed set-to-position / copy-from-source register ops, and leftmost-greedy priority (the same Go's regexp uses) resolves every merge at construction time. A provably-always-true interior `\b`/`\B` is walked through as a no-op in the closure; text anchors it cannot evaluate make it decline. The accepting state reads the winning config's register block. Still a pure `switch state` machine — no program table, thread list, epsilon-closure, or `sync.Pool`.

There is NO run-time interpreter anywhere in the generator — the Thompson NFA-simulation template and the `ForceInterpreter` flag have been removed. When neither compiled path applies (an interior *text*-anchor assertion the DFA cannot evaluate, or DFA state explosion), `buildSubmatchContext` returns a clean error rather than emit a walker. Genuinely-conditional empty-width assertions (e.g. `foo\bbar`, `a$b`) are rejected upstream by `dfa.ValidateAssertions`, never interpreted.

All paths are verified byte-for-byte against stdlib in `e2e/submatch_parity_test.go` (parity) and `e2e/submatch_fuzz_test.go` (fixed-seed differential fuzz), and `e2e/no_interpreter_test.go` asserts no generated fixture contains interpreter machinery.

Mode semantics in the emitted code:

- **full** runs the anchored DFA over the whole input and tests the final state.
- **prefix** returns true the moment any accepting state is entered (a match
  that merely passes through an accepting state still counts).
- **contains** scans once, left to right, with the search DFA; the switch
  default restarts at the start state (those transitions are omitted by the
  builder). A complete-literal pattern compiles to `strings.Contains`, and a
  single-byte start alphabet uses a `strings.IndexByte` candidate skip.

## Key details

- CLI uses cobra with double-hyphen flags (`--regex`, `--package`, etc.)
- `--submatch` requires `--match full` (extraction is full-anchored)
- Invalid UTF-8 is matched exactly like `regexp`: each bad byte decodes to one U+FFFD rune
- The TDFA register file is a stack-allocated `[N]int` (N = maxConfigs*numSlots); a
  transition's ops snapshot any clobbered copy-source into a temp first, so they are
  hazard-free. No generated code builds a package-level program table — the emitted
  submatch core is always a straight-line `switch state` machine
- Generated code imports `github.com/wow-look-at-my/go-regex-compiler/match` for the `InRange` helper (only when range conditions are present)
- Generated code includes a `// Code generated by go-regex-compiler. DO NOT EDIT.` header
- Generated code is NOT run through gofmt -- templates produce the final output directly
- The `--package` flag defaults to `$GOPACKAGE` for `go:generate` compatibility
- Both `bench/` and `e2e/` require `go generate` first (generated files are gitignored)
- Pipeline stage benchmarks (parser, dfa, codegen) run without any generate step
