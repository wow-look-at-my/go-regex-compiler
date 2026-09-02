package codegen

import (
	"fmt"
	"regexp/syntax"
	"unicode"
	"unicode/utf8"
)

// SubmatchOptions controls generation of the submatch function family.
type SubmatchOptions struct {
	PackageName string
	FuncName    string // e.g. "FindSubmatch" — positional []string API
	MatchFunc   string // name of the bool match function to call
	Regex       string
	Prog        *syntax.Prog
	NumGroups   int      // number of capture groups (not counting group)
	GroupNames  []string // names per group index (len NumGroups+); index == ""

	// NamesFuncName is the name of the generated accessor that returns the
	NamesFuncName string

	// Struct controls generation of the typed capture struct. It is only
	StructEnabled bool
	StructType    string // type name, e.g. "Captures"
	StructFunc    string // constructor func name, e.g. "FindCaptures"
}

// submatchContext holds all data needed by the submatch templates.
type submatchContext struct {
	FuncName      string
	IndexFuncName string // <FuncName>Index — the core
	MatchFunc     string
	Regex         string
	NumSlots      int
	NumGroups     int // numSlots /

	// Onepass selects the COMPILED -pass capture matcher: a straight-line
	Onepass     bool
	OPStart     int
	OPStates    []onepassEmitState
	OPAccepts   []onepassAccept
	OPHasAccept bool
	ASCII       bool // compiled path: byte fast-path vs. rune decoding
	HasRanges   bool // compiled path: any match.InRange call emitted

	// Empty-width (\b/\B) boundary gates for the compiled path. Text anchors
	OPStartWord     int    // none, rune must be a word char, must not
	OPNeedFirstRune bool   // decode the rune for the start gate
	OPNeedLastRune  bool   // decode the last rune for an accept gate
	OPWordFunc      string // emitted word-char helper name ("" if unused)

	// TDFA selects the COMPILED tagged-DFA capture matcher for patterns with
	TDFA        bool
	TDStart     int
	TDRegCount  int      // size of the register file (maxConfigs * numSlots)
	TDStartInit []string // register writes to initialize the start state at pos
	TDStates    []tdEmitState
	TDAccepts   []tdEmitAccept
	TDHasAccept bool
	TDUsesPos   bool // any transition sets a register to the current position (np)

	// Priv is an unexported, per-matcher identifier prefix (the index func name
	Priv string

	// Names accessor.
	NamesFuncName string
	GroupNames    []string // raw names, index = group number

	// Typed struct (emitted only when EmitStruct is true).
	EmitStruct   bool
	StructType   string
	StructFunc   string
	StructFields []structField
}

// structField describes exported field of the typed capture struct.
type structField struct {
	Name  string // exported Go field name
	Group int    // capture group index this field reads from
}

func buildSubmatchContext(opts SubmatchOptions) (submatchContext, error) {
	numSlots := (opts.NumGroups + 1) * 2

	if opts.NamesFuncName == "" {
		opts.NamesFuncName = "SubexpNames"
	}
	if opts.StructType == "" {
		opts.StructType = "Captures"
	}
	if opts.StructFunc == "" {
		opts.StructFunc = "FindCaptures"
	}

	indexFuncName := opts.FuncName + "Index"
	ctx := submatchContext{
		FuncName:      opts.FuncName,
		IndexFuncName: indexFuncName,
		MatchFunc:     opts.MatchFunc,
		Regex:         opts.Regex,
		NumSlots:      numSlots,
		NumGroups:     numSlots / 2,
		Priv:          lowerFirst(indexFuncName),
		NamesFuncName: opts.NamesFuncName,
		GroupNames:    normalizeGroupNames(opts.GroupNames, opts.NumGroups),
	}

	// Every pattern compiles to a straight-line `switch state` machine. There is
	switch {
	case tryOnepass(&ctx, opts):
	case tryTDFA(&ctx, opts):
	default:
		return submatchContext{}, fmt.Errorf("cannot compile submatch for %q to a state machine: the pattern needs an interior text-anchor assertion the DFA cannot evaluate, or exceeds the DFA state budget", opts.Regex)
	}

	// Decide whether to emit the typed struct: opt-in AND at least named
	if opts.StructEnabled {
		fields := deriveStructFields(ctx.GroupNames)
		if len(fields) > 0 {
			ctx.EmitStruct = true
			ctx.StructType = opts.StructType
			ctx.StructFunc = opts.StructFunc
			ctx.StructFields = fields
		}
	}

	return ctx, nil
}

// tryOnepass fills ctx with the -pass compiled matcher if the pattern is
func tryOnepass(ctx *submatchContext, opts SubmatchOptions) bool {
	d, ok := buildCapDFA(opts.Prog, opts.NumGroups)
	if !ok {
		return false
	}
	fillOnepass(ctx, d)
	return true
}

// tryTDFA fills ctx with the compiled tagged-DFA register matcher if the pattern
func tryTDFA(ctx *submatchContext, opts SubmatchOptions) bool {
	d, ok := buildTDFA(opts.Prog, opts.NumGroups)
	if !ok {
		return false
	}
	fillTDFA(ctx, d)
	return true
}

// normalizeGroupNames returns a slice of length numGroups+ (index == "").
func normalizeGroupNames(names []string, numGroups int) []string {
	out := make([]string, numGroups+1)
	copy(out, names)
	out[0] = ""
	return out
}

// HasNamedGroups reports whether any capture group (..N) carries a name.
func HasNamedGroups(names []string) bool {
	for i := 1; i < len(names); i++ {
		if names[i] != "" {
			return true
		}
	}
	return false
}

// deriveStructFields builds struct field per NAMED group.
func deriveStructFields(names []string) []structField {
	var fields []structField
	seen := make(map[string]int) // exported name -> index into fields
	for g := 1; g < len(names); g++ {
		name := names[g]
		if name == "" {
			continue
		}
		exported := exportFieldName(name)
		if idx, ok := seen[exported]; ok {
			// Collision (see KNOWN LIMITATION above): later group wins.
			fields[idx].Group = g
			continue
		}
		seen[exported] = len(fields)
		fields = append(fields, structField{Name: exported, Group: g})
	}
	return fields
}

// exportFieldName converts a regex group name into an exported Go field name:
func exportFieldName(name string) string {
	if name == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(name)
	if !unicode.IsLetter(r) {
		return "X" + name
	}
	return string(unicode.ToUpper(r)) + name[size:]
}

// lowerFirst returns s with its rune lower-cased, yielding an unexported
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[size:]
}
