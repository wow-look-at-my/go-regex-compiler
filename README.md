# go-regex-compiler

A Go code generator that compiles regular expressions into pure Go functions. Instead of interpreting regex at runtime, it builds a DFA (Deterministic Finite Automaton) from your pattern and emits Go source code with a switch-based state machine -- no `regexp` package needed at runtime.

## Install

```bash
go install github.com/wow-look-at-my/go-regex-compiler/cmd/go-regex-compiler@latest
```

## Usage

```bash
go-regex-compiler --regex 'pattern' [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--regex` | *(required)* | Regular expression to compile |
| `--package` | `$GOPACKAGE` or `"main"` | Package name for generated code |
| `--func` | `Match` | Name of the generated match function |
| `--output` | stdout | Output file path |
| `--match` | `full` | Match mode: `full`, `prefix`, or `contains` |
| `--submatch` | `false` | Generate the submatch family (`FindSubmatch`, `FindSubmatchIndex`, `SubexpNames`) for capture groups |
| `--submatch-func` | `FindSubmatch` | Name of the positional submatch function (also generates `<name>Index`) |
| `--submatch-names-func` | `SubexpNames` | Name of the generated group-names accessor function |
| `--submatch-struct` | `false` | Also generate a typed capture struct (requires at least one named group) |
| `--submatch-struct-type` | `Captures` | Name of the generated capture struct type |
| `--submatch-struct-func` | `FindCaptures` | Name of the generated capture struct constructor |

### Match modes

- **full** -- matches the entire string against the pattern
- **prefix** -- matches if the string starts with the pattern
- **contains** -- matches if any substring matches the pattern. Compiled as a
  single-pass unanchored search DFA (O(n) regardless of pattern shape); a
  pattern that is one exact literal compiles to a `strings.Contains` call.

### Empty-width assertions

A DFA cannot inspect the characters around a boundary, so `^`, `$`, `\A`,
`\z`, `\b`, and `\B` are only accepted where they are provably **always
satisfied** for the chosen `--match` mode; anything else is rejected with an
error explaining the construct (older versions silently ignored assertions
and generated wrong matchers, e.g. `foo\bbar` full-matched `"foobar"`):

- `^` / `\A` (and `(?m)^`) -- allowed at the start of the pattern in `full`
  and `prefix` modes, which are start-anchored anyway. Rejected mid-pattern
  and in `contains` mode.
- `$` / `\z` (and `(?m)$`) -- allowed at the end of the pattern in `full`
  mode. Rejected mid-pattern and in `prefix`/`contains` modes (a match may
  end before the input does).
- `\b` / `\B` -- allowed only where every possible neighboring-character
  combination satisfies the assertion, e.g. `\bfoo...` or `...foo\b` in
  `full` mode (text edge on one side, word characters on the other).
  Rejected otherwise (`foo\bbar`, or `\berror` in `contains` mode, where the
  character before the match is unknown).

The `--submatch` functions honor the same assertion validation as the bool
matcher: a provably-always-true `\b`/`\B` (including an interior one, like `\B`
between two word chars) folds away to a no-op, and any assertion the compiled
matcher cannot evaluate is rejected rather than silently mishandled.

## Examples

Generate a function that matches email-like patterns:

```bash
go-regex-compiler --regex '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}' \
  --func IsEmail --package validators --output email.go
```

Use with `go generate`:

```go
//go:generate go-regex-compiler --regex "^[0-9]{3}-[0-9]{4}$" --func MatchPhone --output phone_match.go
```

Generate with capture group extraction:

```bash
go-regex-compiler --regex '([0-9]{4})-([0-9]{2})-([0-9]{2})' \
  --func MatchDate --submatch --submatch-func ExtractDate \
  --package main --output date.go
```

The generated `ExtractDate` function returns `[]string` where index 0 is the full match and indices 1..N are the capture groups, or `nil` if the input doesn't match.

### Capture groups

When `--submatch` is set, the generator emits a small family of functions that
mirror the `regexp` API. The core `<func>Index` is **compiled directly to a
`switch state` automaton** whenever the pattern is *one-pass* (the next input
rune always determines the next step unambiguously): capture-group byte offsets
are written inline (`caps[k] = pos`) on the transitions that cross a group
boundary, with **no** instruction table, thread list, or epsilon-closure at run
time — the automaton *is* the Go control flow, allocating only the result slice
and beating stdlib `regexp` on the match path. Text anchors (`^ $ \A \z`) and
word boundaries (`\b \B`) compile too — start/end ones as cheap boundary gates,
and a provably-always-true interior `\b`/`\B` folds to a no-op. The minority of
patterns with genuine capture ambiguity compile instead to a tagged-DFA register
machine — still a straight-line `switch state` machine, with no interpreter.

`--submatch` requires `--match full`: the submatch functions extract groups from
a full-string match, so combining them with prefix/contains matching would be
self-contradictory (the bool matcher could say true while the extractor returns
`nil`). If the regex has no capture groups, `--submatch` is ignored and a
one-line note is printed to stderr.

Case-insensitive patterns work throughout: the full Unicode simple-fold orbit
of every case-folded literal and class is honored (`(?i)k` matches `k`, `K`,
and `U+212A KELVIN SIGN`), in the bool matcher and the compiled submatch
automata alike.

The family:

- **`<func>(input string) []string`** (default `FindSubmatch`) — positional
  extraction. Index 0 is the whole match, indices 1..N are the groups. A group
  that did not participate in the match yields `""` (exactly like
  `regexp.Regexp.FindStringSubmatch`). Returns `nil` on no match.
- **`<func>Index(input string) []int`** (e.g. `FindSubmatchIndex`) — the core,
  returning the submatch **index** slice of length `2*(N+1)`: pair `(2*g, 2*g+1)`
  holds the absolute byte offsets `[start, end)` of group `g`, and a
  non-participating group has the pair `(-1, -1)`. This is parity with
  `regexp.Regexp.FindStringSubmatchIndex`, and is the way to tell an **absent**
  optional group (offset `-1`) apart from one that matched an **empty** string —
  the `[]string`/struct APIs collapse both to `""`.
- **`SubexpNames() []string`** (configurable via `--submatch-names-func`) —
  returns the capture-group names, one entry per group index. Index 0 (the
  whole match) is always `""`, and an unnamed group is `""`. Parity with
  `regexp.Regexp.SubexpNames`.

#### Named groups and the typed struct

Use Go's named-capture syntax `(?P<name>...)` and pass `--submatch-struct` to
also generate a typed struct with one exported field per **named** group plus a
`Matched bool`:

```bash
go-regex-compiler \
  --regex '(?P<ip>\d+\.\d+\.\d+\.\d+) - - \[(?P<ts>[^\]]+)\] "(?P<method>[A-Z]+) (?P<path>[^ ]+) (?P<proto>[^"]+)" (?P<status>\d{3}) (?P<size>\d+)' \
  --func MatchAccessLog --submatch --submatch-func FindAccessLog \
  --submatch-struct --submatch-struct-type AccessLog --submatch-struct-func ParseAccessLog \
  --package logs --output access_log.go
```

```go
entry := logs.ParseAccessLog(line)
if entry.Matched {
    fmt.Println(entry.Ip, entry.Method, entry.Path, entry.Status)
}
```

`ParseAccessLog` returns the zero struct (`Matched: false`, all fields `""`) on
no match, and otherwise fills each field from its group (an unmatched optional
named group yields `""`).

**Field-name derivation.** A field name is the group's name with its first rune
upper-cased; if the first rune is not a letter it is prefixed with `X`
(so `1st` becomes `X1st`).

**Known limitation (case collision).** Two distinct group names that differ only
by the case of their first rune — e.g. `ip` and `Ip` — both export to the same
Go field name and collide into a single field (the last such group wins). The
generator does not attempt to disambiguate this. *TODO:* a future version could
suffix colliding fields with their group index.

`--submatch-struct` only emits the struct when the regex actually has at least
one named group; if it is set on a pattern with no named groups, the struct is
skipped and a one-line note is printed to stderr.

## How it works

1. **Parse** -- the regex is parsed into an NFA using Go's `regexp/syntax` package
2. **Build DFA** -- the NFA is converted to a DFA via subset construction
3. **Generate code** -- the DFA is emitted as a Go function with a switch-based state machine

For ASCII-only patterns, the generated code operates on bytes directly. For Unicode patterns, it uses `utf8.DecodeRuneInString`; invalid UTF-8 is handled exactly like `regexp` (each bad byte is matched as one U+FFFD rune).

Submatch extraction has two compiled paths and **no run-time interpreter**. **One-pass patterns are compiled**: the `<func>Index` core is the same `switch state` DFA the bool matcher emits, extended so each transition that crosses a capture boundary writes the current byte offset into a register (`caps[k] = pos`), and the accept state builds the `[]int` result straight from those registers. There is no NFA program, no thread list, no epsilon-closure, and no `sync.Pool` — a call is a single left-to-right pass with one allocation (the result), so it runs faster than stdlib's onepass interpreter. Empty-width assertions are handled inline: text anchors (`^`, `$`, `\A`, `\z`) fold away as always-satisfied for a full match, a leading/trailing word boundary (`\b`, `\B`) becomes a one-line gate on the first/last rune, and a provably-always-true *interior* `\b`/`\B` (one the validator has proven always holds, e.g. `\B` between two word chars) folds to a no-op — so `(a\Bb)` compiles exactly like `(ab)`. Patterns that are **not** one-pass — genuine capture ambiguity like adjacent stars `(a*)(a*)` or overlapping alternation `(a|ab)(a*)`, `(?i)` fold classes, and ambiguous bodies carrying an always-true interior `\b`/`\B` like `(\w+\B\w+)` — compile instead to a **tagged-DFA register machine**: the marker-annotated NFA is determinized so each live thread owns an isolated block of an integer register file, transitions carry fixed set/copy register ops, and leftmost-greedy priority (matching Go's default `regexp` engine — not POSIX leftmost-longest) resolves every ambiguity at construction time. It too is a straight-line `switch state` machine — no program table, thread list, epsilon-closure, or `sync.Pool`. When neither path can compile a pattern (an interior *text*-anchor assertion, or DFA state explosion), the generator returns a clean error rather than emit a walker. Both paths are verified byte-for-byte against `regexp.FindStringSubmatch`/`FindStringSubmatchIndex` over a differential parity + fuzz corpus (see `e2e/submatch_parity_test.go`, `e2e/submatch_fuzz_test.go`).

Correctness is enforced differentially: alongside the hand-written e2e cases,
a deterministic fuzz corpus (`e2e/fuzz_diff_test.go`, generated by
`go generate ./e2e/...`) compares every generated matcher — 40 directed
patterns targeting historical bug classes plus 50 fixed-seed random patterns,
in all three match modes — against stdlib `regexp` over thousands of inputs,
including invalid UTF-8. Assertion placements the validator rejects are pinned
as rejected: a pattern either compiles and agrees with `regexp`, or fails
generation with a descriptive error — never a silently wrong matcher.

## Benchmarks

Run all benchmarks (pipeline stages + generated code vs regexp):

```bash
go generate ./bench/...
go-toolchain
```

The pipeline stage benchmarks (parser, DFA builder, codegen) run without `go generate`. The `bench/` package benchmarks compare generated matchers against compiled and uncompiled `regexp.MatchString`.

## License

[MIT](LICENSE)
