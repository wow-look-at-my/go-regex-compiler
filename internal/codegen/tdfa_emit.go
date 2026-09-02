package codegen

// This file lowers a tdfaAutomaton (see tdfa.go) into the flat structures the
// TDFA template renders (templates_tdfa.go). Every Go-syntax decision — the
// per-transition register operations, the byte-vs-rune conditions, the accept
// read-out — happens here so the template stays declarative. The emitted code is
// a `switch state` machine over a fixed integer register file with inline
// register writes; there is no interpreter.

import (
	"github.com/wow-look-at-my/go-containers/set"
	"sort"
	"strconv"
	"strings"
)

// tdEmitState is one `case <id>:` of the register machine's state switch.
type tdEmitState struct {
	ID    int
	Cases []tdEmitCase

	// Guard/GuardBody are set for a single-transition state — exactly one
	// transition, whose non-match always returns nil. The template then emits an
	// early-return guard instead of a one-case switch:
	//   if <Guard> { return nil }
	//   <GuardBody>
	// Guard is the negation of the transition condition; GuardBody its reg ops.
	Guard     string
	GuardBody string
}

// tdEmitCase is one `case <cond>: <body>` of a state's inner switch. Body holds
// the register operations plus the `state = <next>` assignment.
type tdEmitCase struct {
	Cond string
	Body string
}

// tdEmitAccept groups accepting states that share the same winning config
// position (and thus read the same register block) into one `case a, b:`.
type tdEmitAccept struct {
	IDs    string // "2, 5"
	ReadLo int    // winner block base + 2 (skip whole-match slots 0/1)
	ReadHi int    // winner block base + numSlots
}

// fillTDFA populates the TDFA fields of ctx from d.
func fillTDFA(ctx *submatchContext, d *tdfaAutomaton) {
	ctx.TDFA = true
	ctx.ASCII = d.ascii
	ctx.TDStart = d.start
	ns := d.numSlots
	ctx.TDRegCount = d.maxConfig * ns
	if ctx.TDRegCount == 0 {
		ctx.TDRegCount = ns
	}

	// Start register initialization at position 0: crossed slots (skip 0/1) get 0.
	for k, f := range d.startFill {
		for t := 2; t < ns; t++ {
			if f.crossed[t] {
				ctx.TDStartInit = append(ctx.TDStartInit, "reg["+strconv.Itoa(k*ns+t)+"] = 0")
			}
		}
	}

	hasRanges := false
	usesPos := false
	posExpr := "np" // post-consume position local set each loop iteration
	for _, st := range d.states {
		if len(st.trans) == 0 {
			continue
		}
		es := tdEmitState{ID: st.id}
		for _, tr := range st.trans {
			body, setsPos := tdTransBody(tr, ns, posExpr)
			usesPos = usesPos || setsPos
			cond, ur := tdCond(tr, d.ascii)
			hasRanges = hasRanges || ur
			es.Cases = append(es.Cases, tdEmitCase{Cond: cond, Body: body})
		}
		// A single transition (its non-match always returns nil) becomes an
		// early-return guard, not a one-case switch.
		if len(es.Cases) == 1 {
			es.Guard = negateConds([]string{es.Cases[0].Cond})
			es.GuardBody = es.Cases[0].Body
		}
		ctx.TDStates = append(ctx.TDStates, es)
	}
	ctx.HasRanges = hasRanges
	ctx.TDUsesPos = usesPos

	// Accept states grouped by winning config position (register block read out).
	byWinner := map[int][]int{}
	var order []int
	for _, st := range d.states {
		if !st.accept {
			continue
		}
		if _, ok := byWinner[st.winner]; !ok {
			order = append(order, st.winner)
		}
		byWinner[st.winner] = append(byWinner[st.winner], st.id)
	}
	sort.Ints(order)
	for _, w := range order {
		ids := byWinner[w]
		sort.Ints(ids)
		strIDs := make([]string, len(ids))
		for i, id := range ids {
			strIDs[i] = strconv.Itoa(id)
		}
		ctx.TDAccepts = append(ctx.TDAccepts, tdEmitAccept{
			IDs:    strings.Join(strIDs, ", "),
			ReadLo: w*ns + 2,
			ReadHi: w*ns + ns,
		})
	}
	ctx.TDHasAccept = len(ctx.TDAccepts) > 0
}

// tdTransBody renders the register operations for one transition plus the state
// assignment. Reads that would be clobbered by a write in the same transition
// are snapshotted into temporaries first, so the ops are hazard-free regardless
// of order. Whole-match slots 0/1 are skipped (they are forced at accept). It
// reports whether it emitted a set-to-position op (so the caller knows the loop
// needs the post-consume position local).
func tdTransBody(tr tdTrans, ns int, posExpr string) (string, bool) {
	type copyOp struct{ dst, src int }
	var sets []int
	var copies []copyOp
	written := set.New[int]()

	for k, f := range tr.fills {
		for t := 2; t < ns; t++ {
			dst := k*ns + t
			if f.crossed[t] {
				sets = append(sets, dst)
				written.Add(dst)
				continue
			}
			src := f.src*ns + t
			if src == dst {
				continue // identity copy: value already in place
			}
			copies = append(copies, copyOp{dst: dst, src: src})
			written.Add(dst)
		}
	}

	// Snapshot any copy source that is also written this transition, so copies
	// read the pre-transition value.
	snap := map[int]string{}
	var snapOrder []int
	for _, c := range copies {
		if written.Contains(c.src) {
			if _, ok := snap[c.src]; !ok {
				snap[c.src] = "t" + strconv.Itoa(len(snapOrder))
				snapOrder = append(snapOrder, c.src)
			}
		}
	}

	var sb strings.Builder
	for _, src := range snapOrder {
		sb.WriteString(snap[src])
		sb.WriteString(" := reg[")
		sb.WriteString(strconv.Itoa(src))
		sb.WriteString("]; ")
	}
	sort.Ints(sets)
	for _, dst := range sets {
		sb.WriteString("reg[")
		sb.WriteString(strconv.Itoa(dst))
		sb.WriteString("] = ")
		sb.WriteString(posExpr)
		sb.WriteString("; ")
	}
	for _, c := range copies {
		sb.WriteString("reg[")
		sb.WriteString(strconv.Itoa(c.dst))
		sb.WriteString("] = ")
		if name, ok := snap[c.src]; ok {
			sb.WriteString(name)
		} else {
			sb.WriteString("reg[")
			sb.WriteString(strconv.Itoa(c.src))
			sb.WriteString("]")
		}
		sb.WriteString("; ")
	}
	sb.WriteString("state = ")
	sb.WriteString(strconv.Itoa(tr.next))
	return sb.String(), len(sets) > 0
}

// tdCond builds the case condition for a transition's rune interval, reusing the
// one-pass range renderer. Reports whether it used match.InRange.
func tdCond(tr tdTrans, ascii bool) (string, bool) {
	return rangeCond(tr.lo, tr.hi, ascii)
}
