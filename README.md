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
- **contains** -- matches if any substring matches the pattern

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

The `--submatch` functions evaluate all assertions exactly (their Thompson
simulation checks them per position), but they share the pattern with the
DFA matcher that gates them, so the same validation applies.

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
mirror the `regexp` API and share a single Thompson NFA core:

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

For ASCII-only patterns, the generated code operates on bytes directly. For Unicode patterns, it uses `utf8.DecodeRuneInString`. Submatch extraction uses a Thompson NFA simulation with capture tracking, gated behind the DFA match for fast rejection. The simulation preserves thread priority (leftmost-first, matching Go's default `regexp` engine — not POSIX leftmost-longest) and evaluates empty-width assertions (`^`, `$`, `\b`, `\B`, `\A`, `\z`) inline against the surrounding runes, so internal assertions affect capture positions exactly as stdlib does. The submatch functions are verified byte-for-byte against `regexp.FindStringSubmatch`/`FindStringSubmatchIndex` over a differential test corpus (see `e2e/submatch_parity_test.go`).

## Benchmarks

Run all benchmarks (pipeline stages + generated code vs regexp):

```bash
go generate ./bench/...
go-toolchain
```

The pipeline stage benchmarks (parser, DFA builder, codegen) run without `go generate`. The `bench/` package benchmarks compare generated matchers against compiled and uncompiled `regexp.MatchString`.

## License

[MIT](LICENSE)
