# go-regex-compiler

A Go code generator that compiles regular expressions into pure Go functions. Instead of interpreting regex at runtime, it builds a DFA (Deterministic Finite Automaton) from your pattern and emits Go source code with a switch-based state machine -- no `regexp` package needed at runtime.

## Install

```bash
go install github.com/wow-look-at-my/go-regex-compiler/cmd/regex-gen@latest
```

## Usage

```bash
regex-gen -regex 'pattern' [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-regex` | *(required)* | Regular expression to compile |
| `-package` | `$GOPACKAGE` or `"main"` | Package name for generated code |
| `-func` | `Match` | Name of the generated match function |
| `-output` | stdout | Output file path |
| `-match` | `full` | Match mode: `full`, `prefix`, or `contains` |
| `-submatch` | `false` | Generate a `FindSubmatch` function for capture groups |
| `-submatch-func` | `FindSubmatch` | Name of the generated submatch function |

### Match modes

- **full** -- matches the entire string against the pattern
- **prefix** -- matches if the string starts with the pattern
- **contains** -- matches if any substring matches the pattern

## Examples

Generate a function that matches email-like patterns:

```bash
regex-gen -regex '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}' \
  -func IsEmail -package validators -output email.go
```

Use with `go generate`:

```go
//go:generate regex-gen -regex "^[0-9]{3}-[0-9]{4}$" -func MatchPhone -output phone_match.go
```

Generate with capture group extraction:

```bash
regex-gen -regex '([0-9]{4})-([0-9]{2})-([0-9]{2})' \
  -func MatchDate -submatch -submatch-func ExtractDate \
  -package main -output date.go
```

The generated `ExtractDate` function returns `[]string` where index 0 is the full match and indices 1..N are the capture groups, or `nil` if the input doesn't match.

## How it works

1. **Parse** -- the regex is parsed into an NFA using Go's `regexp/syntax` package
2. **Build DFA** -- the NFA is converted to a DFA via subset construction
3. **Generate code** -- the DFA is emitted as a Go function with a switch-based state machine

For ASCII-only patterns, the generated code operates on bytes directly. For Unicode patterns, it uses `utf8.DecodeRuneInString`. Submatch extraction uses a Thompson NFA simulation with capture tracking, gated behind the DFA match for fast rejection.

## Benchmarks

Run all benchmarks (pipeline stages + generated code vs regexp):

```bash
go generate ./bench/...
go-toolchain
```

The pipeline stage benchmarks (parser, DFA builder, codegen) run without `go generate`. The `bench/` package benchmarks compare generated matchers against compiled and uncompiled `regexp.MatchString`.

## License

[MIT](LICENSE)
