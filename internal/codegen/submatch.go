package codegen

import (
	"bytes"
	"fmt"
	"regexp/syntax"
	"unicode"
	"unicode/utf8"

	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
)

// isRuneOp reports whether op is a rune-consuming instruction whose Rune slice
// is subject to case folding.
func isRuneOp(op syntax.InstOp) bool {
	return op == syntax.InstRune || op == syntax.InstRune1
}

// SubmatchOptions controls generation of the submatch function family.
type SubmatchOptions struct {
	PackageName string
	FuncName    string // e.g. "FindSubmatch" — positional []string API
	MatchFunc   string // name of the bool match function to call first
	Regex       string
	Prog        *syntax.Prog
	NumGroups   int      // number of capture groups (not counting group 0)
	GroupNames  []string // names per group index (len NumGroups+1); index 0 == ""

	// NamesFuncName is the name of the generated accessor that returns the
	// group-name slice (parity with regexp.Regexp.SubexpNames). Always emitted.
	NamesFuncName string

	// Struct controls generation of the typed capture struct. It is only
	// emitted when StructEnabled is true AND at least one named group exists.
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
	NumGroups     int // numSlots / 2
	StartPC       int
	Instructions  []nfaInstruction

	// Names accessor.
	NamesFuncName string
	GroupNames    []string // raw names, index = group number

	// Typed struct (emitted only when EmitStruct is true).
	EmitStruct   bool
	StructType   string
	StructFunc   string
	StructFields []structField
}

// structField describes one exported field of the typed capture struct.
type structField struct {
	Name  string // exported Go field name
	Group int    // capture group index this field reads from
}

type nfaInstruction struct {
	Index  int
	OpName string // human-readable op, emitted as a comment
	OpNum  int    // numeric op emitted into the package-level table
	Out    uint32
	// Arg holds the instruction's argument. For opCapture it is the capture
	// slot; for opEmpty it is the syntax.EmptyOp assertion bitmask (the
	// generated sim ANDs it against the satisfied bits at each position).
	Arg   int
	Runes string // empty if no runes
}

func buildSubmatchContext(opts SubmatchOptions) submatchContext {
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

	ctx := submatchContext{
		FuncName:      opts.FuncName,
		IndexFuncName: opts.FuncName + "Index",
		MatchFunc:     opts.MatchFunc,
		Regex:         opts.Regex,
		NumSlots:      numSlots,
		NumGroups:     numSlots / 2,
		StartPC:       opts.Prog.Start,
		NamesFuncName: opts.NamesFuncName,
		GroupNames:    normalizeGroupNames(opts.GroupNames, opts.NumGroups),
	}

	for i, inst := range opts.Prog.Inst {
		ni := nfaInstruction{
			Index:  i,
			OpName: instOpName(inst.Op),
			OpNum:  instOpNum(inst.Op),
			Out:    inst.Out,
			Arg:    int(inst.Arg),
		}
		if len(inst.Rune) > 0 {
			runes := inst.Rune
			// The generated runeMatch never consults the FoldCase flag, so a
			// case-folded instruction must have its full fold orbit expanded
			// into the emitted rune set. This matters doubly for literals:
			// regexp/syntax stores a folded literal as its min-fold rune (e.g.
			// (?i)a compiles to runes [65] = 'A'), so without expansion the
			// pattern fails even on lower-case input.
			if isRuneOp(inst.Op) && syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
				runes = dfa.ExpandFoldCase(dfa.NormalizeRunePairs(runes))
				if inst.Op == syntax.InstRune1 {
					// The expansion produced [lo,hi] range pairs; opRune1's
					// single-rune equality check no longer applies. Emit the
					// instruction as opRune: both the numeric op the generated
					// sim dispatches on and the legend comment must change.
					ni.OpName = instOpName(syntax.InstRune)
					ni.OpNum = instOpNum(syntax.InstRune)
				}
			}
			ni.Runes = formatRunes(runes)
		}
		ctx.Instructions = append(ctx.Instructions, ni)
	}

	// Decide whether to emit the typed struct: opt-in AND at least one named
	// group. The caller (main.go) is responsible for the stderr note when the
	// flag is set but no named group exists; here we just gate emission.
	if opts.StructEnabled {
		fields := deriveStructFields(ctx.GroupNames)
		if len(fields) > 0 {
			ctx.EmitStruct = true
			ctx.StructType = opts.StructType
			ctx.StructFunc = opts.StructFunc
			ctx.StructFields = fields
		}
	}

	return ctx
}

// normalizeGroupNames returns a slice of length numGroups+1 (index 0 == "").
func normalizeGroupNames(names []string, numGroups int) []string {
	out := make([]string, numGroups+1)
	copy(out, names)
	out[0] = ""
	return out
}

// HasNamedGroups reports whether any capture group (1..N) carries a name.
func HasNamedGroups(names []string) bool {
	for i := 1; i < len(names); i++ {
		if names[i] != "" {
			return true
		}
	}
	return false
}

// deriveStructFields builds one struct field per NAMED group.
//
// KNOWN LIMITATION: two distinct group names that differ only by the case of
// their first rune (e.g. "ip" and "Ip") both export to the same field name and
// collide into a single field. We do not attempt to fully disambiguate this;
// see the README TODO. The last named group with a colliding exported name
// wins the field's group index.
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
// the first rune is upper-cased; if the first rune is not a letter, the name is
// prefixed with "X" (e.g. "1st" -> "X1st", "ip" -> "Ip").
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

func instOpName(op syntax.InstOp) string {
	switch op {
	case syntax.InstRune:
		return "opRune"
	case syntax.InstRune1:
		return "opRune1"
	case syntax.InstRuneAny:
		return "opRuneAny"
	case syntax.InstRuneAnyNotNL:
		return "opRuneAnyNL"
	case syntax.InstAlt, syntax.InstAltMatch:
		return "opAlt"
	case syntax.InstCapture:
		return "opCapture"
	case syntax.InstMatch:
		return "opMatch"
	case syntax.InstNop:
		return "opNop"
	case syntax.InstFail:
		return "opFail"
	case syntax.InstEmptyWidth:
		return "opEmpty"
	default:
		return fmt.Sprintf("%d", op)
	}
}

// instOpNum maps a syntax.InstOp to the numeric op values used by the
// generated table and simulation (the const legend in the nfaSim template
// must stay in sync).
func instOpNum(op syntax.InstOp) int {
	switch op {
	case syntax.InstRune:
		return 0
	case syntax.InstRune1:
		return 1
	case syntax.InstRuneAny:
		return 2
	case syntax.InstRuneAnyNotNL:
		return 3
	case syntax.InstAlt, syntax.InstAltMatch:
		return 4
	case syntax.InstCapture:
		return 5
	case syntax.InstMatch:
		return 6
	case syntax.InstNop:
		return 7
	case syntax.InstFail:
		return 8
	case syntax.InstEmptyWidth:
		return 9
	default:
		return 8 // treat unknown ops as opFail: they can never match
	}
}

func formatRunes(runes []rune) string {
	var buf bytes.Buffer
	buf.WriteString("[]rune{")
	for i, r := range runes {
		if i > 0 {
			buf.WriteString(", ")
		}
		fmt.Fprintf(&buf, "%d", r)
	}
	buf.WriteString("}")
	return buf.String()
}
