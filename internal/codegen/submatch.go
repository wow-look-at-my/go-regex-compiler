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

// GenerateSubmatch writes a FindSubmatch function that uses the DFA for fast
// rejection and an embedded NFA simulator for capture group extraction.
func GenerateSubmatch(buf *bytes.Buffer, opts SubmatchOptions) {
	numSlots := (opts.NumGroups + 1) * 2 // 2 slots per group (start, end), +1 for group 0

	fmt.Fprintf(buf, "// %s returns captured groups for the regex %s.\n", opts.FuncName, quoteRegex(opts.Regex))
	fmt.Fprintf(buf, "// Returns nil if the input does not match.\n")
	fmt.Fprintf(buf, "// Index 0 is the entire match, indices 1..N are capture groups.\n")
	fmt.Fprintf(buf, "func %s(input string) []string {\n", opts.FuncName)

	// Fast DFA rejection
	fmt.Fprintf(buf, "\tif !%s(input) {\n", opts.MatchFunc)
	fmt.Fprintf(buf, "\t\treturn nil\n")
	fmt.Fprintf(buf, "\t}\n")

	// Generate the NFA instruction table
	writeNFATable(buf, opts.Prog)

	// Generate the NFA simulation
	writeNFASimulation(buf, opts.Prog, numSlots)

	fmt.Fprintf(buf, "}\n")
}

// writeNFATable embeds the NFA program as a slice of instruction structs.
func writeNFATable(buf *bytes.Buffer, prog *syntax.Prog) {
	fmt.Fprintf(buf, "\t// NFA instruction types\n")
	fmt.Fprintf(buf, "\tconst (\n")
	fmt.Fprintf(buf, "\t\topRune      = 0\n")
	fmt.Fprintf(buf, "\t\topRune1     = 1\n")
	fmt.Fprintf(buf, "\t\topRuneAny   = 2\n")
	fmt.Fprintf(buf, "\t\topRuneAnyNL = 3\n")
	fmt.Fprintf(buf, "\t\topAlt       = 4\n")
	fmt.Fprintf(buf, "\t\topCapture   = 5\n")
	fmt.Fprintf(buf, "\t\topMatch     = 6\n")
	fmt.Fprintf(buf, "\t\topNop       = 7\n")
	fmt.Fprintf(buf, "\t\topFail      = 8\n")
	fmt.Fprintf(buf, "\t\topEmpty     = 9\n")
	fmt.Fprintf(buf, "\t)\n\n")

	fmt.Fprintf(buf, "\ttype nfaInst struct {\n")
	fmt.Fprintf(buf, "\t\top   int\n")
	fmt.Fprintf(buf, "\t\tout  int\n")
	fmt.Fprintf(buf, "\t\targ  int\n")
	fmt.Fprintf(buf, "\t\trunes []rune\n")
	fmt.Fprintf(buf, "\t}\n\n")

	fmt.Fprintf(buf, "\tprog := []nfaInst{\n")
	for i, inst := range prog.Inst {
		op := instOpName(inst.Op)
		runes := ""
		if len(inst.Rune) > 0 {
			runes = fmt.Sprintf(", runes: %s", formatRunes(inst.Rune))
		}
		fmt.Fprintf(buf, "\t\t/* %d */ {op: %s, out: %d, arg: %d%s},\n",
			i, op, inst.Out, inst.Arg, runes)
	}
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\tstartPC := %d\n\n", prog.Start)
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

// writeNFASimulation generates a Thompson NFA simulation with capture tracking.
func writeNFASimulation(buf *bytes.Buffer, prog *syntax.Prog, numSlots int) {
	fmt.Fprintf(buf, "\t// Thompson NFA simulation with capture tracking\n")
	fmt.Fprintf(buf, "\ttype thread struct {\n")
	fmt.Fprintf(buf, "\t\tpc   int\n")
	fmt.Fprintf(buf, "\t\tcaps [%d]int\n", numSlots)
	fmt.Fprintf(buf, "\t}\n\n")

	// addThread follows epsilon transitions and adds consuming threads
	fmt.Fprintf(buf, "\taddThread := func(list []thread, pc int, caps [%d]int, pos int) []thread {\n", numSlots)
	fmt.Fprintf(buf, "\t\tvisited := make(map[int]bool)\n")
	fmt.Fprintf(buf, "\t\tvar stack []thread\n")
	fmt.Fprintf(buf, "\t\tstack = append(stack, thread{pc: pc, caps: caps})\n")
	fmt.Fprintf(buf, "\t\tfor len(stack) > 0 {\n")
	fmt.Fprintf(buf, "\t\t\tt := stack[len(stack)-1]\n")
	fmt.Fprintf(buf, "\t\t\tstack = stack[:len(stack)-1]\n")
	fmt.Fprintf(buf, "\t\t\tif t.pc < 0 || t.pc >= len(prog) || visited[t.pc] {\n")
	fmt.Fprintf(buf, "\t\t\t\tcontinue\n")
	fmt.Fprintf(buf, "\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t\tvisited[t.pc] = true\n")
	fmt.Fprintf(buf, "\t\t\tinst := &prog[t.pc]\n")
	fmt.Fprintf(buf, "\t\t\tswitch inst.op {\n")
	fmt.Fprintf(buf, "\t\t\tcase opAlt:\n")
	fmt.Fprintf(buf, "\t\t\t\tstack = append(stack, thread{pc: inst.arg, caps: t.caps})\n")
	fmt.Fprintf(buf, "\t\t\t\tstack = append(stack, thread{pc: int(inst.out), caps: t.caps})\n")
	fmt.Fprintf(buf, "\t\t\tcase opNop, opEmpty:\n")
	fmt.Fprintf(buf, "\t\t\t\tstack = append(stack, thread{pc: int(inst.out), caps: t.caps})\n")
	fmt.Fprintf(buf, "\t\t\tcase opCapture:\n")
	fmt.Fprintf(buf, "\t\t\t\tnewCaps := t.caps\n")
	fmt.Fprintf(buf, "\t\t\t\tif inst.arg < %d {\n", numSlots)
	fmt.Fprintf(buf, "\t\t\t\t\tnewCaps[inst.arg] = pos\n")
	fmt.Fprintf(buf, "\t\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t\t\tstack = append(stack, thread{pc: int(inst.out), caps: newCaps})\n")
	fmt.Fprintf(buf, "\t\t\tdefault:\n")
	fmt.Fprintf(buf, "\t\t\t\tlist = append(list, t)\n")
	fmt.Fprintf(buf, "\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t}\n")
	fmt.Fprintf(buf, "\t\treturn list\n")
	fmt.Fprintf(buf, "\t}\n\n")

	// runeMatch helper
	fmt.Fprintf(buf, "\truneMatch := func(inst *nfaInst, r rune) bool {\n")
	fmt.Fprintf(buf, "\t\tswitch inst.op {\n")
	fmt.Fprintf(buf, "\t\tcase opRuneAny:\n")
	fmt.Fprintf(buf, "\t\t\treturn true\n")
	fmt.Fprintf(buf, "\t\tcase opRuneAnyNL:\n")
	fmt.Fprintf(buf, "\t\t\treturn r != '\\n'\n")
	fmt.Fprintf(buf, "\t\tcase opRune1:\n")
	fmt.Fprintf(buf, "\t\t\tif len(inst.runes) > 0 && inst.runes[0] == r {\n")
	fmt.Fprintf(buf, "\t\t\t\treturn true\n")
	fmt.Fprintf(buf, "\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t\treturn false\n")
	fmt.Fprintf(buf, "\t\tcase opRune:\n")
	fmt.Fprintf(buf, "\t\t\tfor i := 0; i < len(inst.runes)-1; i += 2 {\n")
	fmt.Fprintf(buf, "\t\t\t\tif r >= inst.runes[i] && r <= inst.runes[i+1] {\n")
	fmt.Fprintf(buf, "\t\t\t\t\treturn true\n")
	fmt.Fprintf(buf, "\t\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t\tif len(inst.runes)%%2 == 1 {\n")
	fmt.Fprintf(buf, "\t\t\t\tif r == inst.runes[len(inst.runes)-1] {\n")
	fmt.Fprintf(buf, "\t\t\t\t\treturn true\n")
	fmt.Fprintf(buf, "\t\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t\treturn false\n")
	fmt.Fprintf(buf, "\t\t}\n")
	fmt.Fprintf(buf, "\t\treturn false\n")
	fmt.Fprintf(buf, "\t}\n\n")

	// Main NFA simulation loop
	fmt.Fprintf(buf, "\tvar initCaps [%d]int\n", numSlots)
	fmt.Fprintf(buf, "\tfor i := range initCaps {\n")
	fmt.Fprintf(buf, "\t\tinitCaps[i] = -1\n")
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\tinitCaps[0] = 0 // group 0 start = beginning of input\n\n")

	fmt.Fprintf(buf, "\tcurrent := addThread(nil, startPC, initCaps, 0)\n\n")

	fmt.Fprintf(buf, "\tfor i, r := range input {\n")
	fmt.Fprintf(buf, "\t\tvar next []thread\n")
	fmt.Fprintf(buf, "\t\tfor _, t := range current {\n")
	fmt.Fprintf(buf, "\t\t\tinst := &prog[t.pc]\n")
	fmt.Fprintf(buf, "\t\t\tif runeMatch(inst, r) {\n")
	fmt.Fprintf(buf, "\t\t\t\tnext = addThread(next, int(inst.out), t.caps, i+len(string(r)))\n")
	fmt.Fprintf(buf, "\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t}\n")
	fmt.Fprintf(buf, "\t\tcurrent = next\n")
	fmt.Fprintf(buf, "\t}\n\n")

	// Find matching thread
	fmt.Fprintf(buf, "\tfor _, t := range current {\n")
	fmt.Fprintf(buf, "\t\tif t.pc < len(prog) && prog[t.pc].op == opMatch {\n")
	fmt.Fprintf(buf, "\t\t\tt.caps[1] = len(input) // group 0 end\n")
	fmt.Fprintf(buf, "\t\t\tresult := make([]string, %d)\n", numSlots/2)
	fmt.Fprintf(buf, "\t\t\tfor g := 0; g < %d; g++ {\n", numSlots/2)
	fmt.Fprintf(buf, "\t\t\t\ts, e := t.caps[g*2], t.caps[g*2+1]\n")
	fmt.Fprintf(buf, "\t\t\t\tif s >= 0 && e >= 0 && s <= len(input) && e <= len(input) {\n")
	fmt.Fprintf(buf, "\t\t\t\t\tresult[g] = input[s:e]\n")
	fmt.Fprintf(buf, "\t\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t\t}\n")
	fmt.Fprintf(buf, "\t\t\treturn result\n")
	fmt.Fprintf(buf, "\t\t}\n")
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\treturn nil\n")
}
