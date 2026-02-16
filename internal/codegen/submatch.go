package codegen

import (
	"bytes"
	"fmt"
	"regexp/syntax"
)

// SubmatchOptions controls generation of a FindSubmatch function.
type SubmatchOptions struct {
	PackageName string
	FuncName    string // e.g. "FindSubmatch"
	MatchFunc   string // name of the bool match function to call first
	Regex       string
	Prog        *syntax.Prog
	NumGroups   int // number of capture groups (not counting group 0)
}

// submatchContext holds all data needed by the submatch templates.
type submatchContext struct {
	FuncName     string
	MatchFunc    string
	Regex        string
	NumSlots     int
	NumGroups    int // numSlots / 2
	StartPC      int
	Instructions []nfaInstruction
}

type nfaInstruction struct {
	Index  int
	OpName string
	Out    uint32
	Arg    int
	Runes  string // empty if no runes
}

func buildSubmatchContext(opts SubmatchOptions) submatchContext {
	numSlots := (opts.NumGroups + 1) * 2

	ctx := submatchContext{
		FuncName:  opts.FuncName,
		MatchFunc: opts.MatchFunc,
		Regex:     opts.Regex,
		NumSlots:  numSlots,
		NumGroups: numSlots / 2,
		StartPC:   opts.Prog.Start,
	}

	for i, inst := range opts.Prog.Inst {
		ni := nfaInstruction{
			Index:  i,
			OpName: instOpName(inst.Op),
			Out:    inst.Out,
			Arg:    int(inst.Arg),
		}
		if len(inst.Rune) > 0 {
			ni.Runes = formatRunes(inst.Rune)
		}
		ctx.Instructions = append(ctx.Instructions, ni)
	}

	return ctx
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
