package codegen

import (
	"sort"
	"strconv"
	"strings"
)

// onepassEmitState is `case <id>:` of the compiled matcher's state switch.
type onepassEmitState struct {
	ID      int
	Cases   []onepassEmitCase
	Default string // default clause: "return nil", or the write+goto for an any-rune edge

	// Guard/GuardBody are set for a -transition state — exactly
	Guard     string
	GuardBody string
}

// onepassEmitCase is `case <cond>: <body>` of a state's inner switch.
type onepassEmitCase struct {
	Cond string
	Body string
}

// onepassAccept groups accepting states that share the same end-of-input capture
type onepassAccept struct {
	IDs  string // ", "
	Body string // "caps[] = len(input)", possibly empty
	Word int    // none, last rune must be a word char (\b), must not (\B)
}

// fillOnepass populates the compiled-path fields of ctx from d.
func fillOnepass(ctx *submatchContext, d *capDFA) {
	ctx.Onepass = true
	ctx.OPStart = d.start
	ctx.ASCII = d.ascii

	hasRanges := false
	for _, st := range d.states {
		if len(st.edges) == 0 {
			continue
		}
		es := onepassEmitState{ID: st.id, Default: "return nil"}
		hasAnyRune := false
		var lastConds []string // alternatives of the sole conditional edge (for the guard)
		for _, e := range st.edges {
			body := onepassEdgeBody(e.writes, e.next)
			if e.anyRune {
				es.Default = body // any rune takes this edge
				hasAnyRune = true
				continue
			}
			conds, usedRange := onepassEdgeConds(e, d.ascii)
			hasRanges = hasRanges || usedRange
			es.Cases = append(es.Cases, onepassEmitCase{Cond: strings.Join(conds, ", "), Body: body})
			lastConds = conds
		}
		// A conditional edge with a plain "return nil" default (no any-rune
		if len(es.Cases) == 1 && !hasAnyRune {
			es.Guard = negateConds(lastConds)
			es.GuardBody = es.Cases[0].Body
		}
		ctx.OPStates = append(ctx.OPStates, es)
	}
	ctx.HasRanges = hasRanges

	// Start gate: a word-boundary assertion (\b/\B) required at position. Text
	ctx.OPStartWord = wordReq(d.startGate)
	needFirst := ctx.OPStartWord != 0

	// Group accepting states by their end-of-input write body AND word-boundary
	type acceptKey struct {
		body string
		word int
	}
	byKey := make(map[acceptKey][]int)
	var order []acceptKey
	needLast := false
	for _, st := range d.states {
		if !st.accept {
			continue
		}
		k := acceptKey{body: onepassAcceptBody(st.acceptWrites), word: wordReq(st.acceptGate)}
		if _, ok := byKey[k]; !ok {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], st.id)
		if k.word != 0 {
			needLast = true
		}
	}
	for _, k := range order {
		ids := byKey[k]
		sort.Ints(ids)
		strIDs := make([]string, len(ids))
		for i, id := range ids {
			strIDs[i] = strconv.Itoa(id)
		}
		ctx.OPAccepts = append(ctx.OPAccepts, onepassAccept{IDs: strings.Join(strIDs, ", "), Body: k.body, Word: k.word})
	}
	ctx.OPHasAccept = len(ctx.OPAccepts) > 0
	ctx.OPNeedFirstRune = needFirst
	ctx.OPNeedLastRune = needLast
	if needFirst || needLast {
		ctx.OPWordFunc = ctx.Priv + "Word"
	}
}

// wordReq maps a residual empty-width gate to a word-boundary requirement code:
func wordReq(gate int) int {
	switch {
	case gate&emptyWordBoundary != 0:
		return 1
	case gate&emptyNoWordBoundary != 0:
		return 2
	default:
		return 0
	}
}

// onepassEdgeBody renders the capture writes plus the state assignment taken when
func onepassEdgeBody(writes []int, next int) string {
	var sb strings.Builder
	for _, w := range writes {
		if w == 0 || w == 1 {
			continue // group is forced from the match bounds
		}
		sb.WriteString("caps[")
		sb.WriteString(strconv.Itoa(w))
		sb.WriteString("] = i; ")
	}
	sb.WriteString("state = ")
	sb.WriteString(strconv.Itoa(next))
	return sb.String()
}

// onepassAcceptBody renders the capture writes performed at end-of-input for an
func onepassAcceptBody(writes []int) string {
	var parts []string
	for _, w := range writes {
		if w == 0 || w == 1 {
			continue // group is forced from the match bounds
		}
		parts = append(parts, "caps["+strconv.Itoa(w)+"] = len(input)")
	}
	return strings.Join(parts, "; ")
}

// onepassEdgeConds builds the case-condition alternatives for a consuming edge
func onepassEdgeConds(e capEdge, ascii bool) (conds []string, usedRange bool) {
	if e.anyNotNL {
		return []string{"r != '\\n'"}, false
	}
	conds = make([]string, 0, len(e.ranges))
	for _, r := range e.ranges {
		c, ur := rangeCond(r.lo, r.hi, ascii)
		usedRange = usedRange || ur
		conds = append(conds, c)
	}
	return conds, usedRange
}

// negateConds returns the guard expression that is true when NONE of a case's
func negateConds(conds []string) string {
	if len(conds) == 1 {
		c := conds[0]
		if lhs, rhs, ok := strings.Cut(c, " == "); ok {
			return lhs + " != " + rhs
		}
		if lhs, rhs, ok := strings.Cut(c, " != "); ok {
			return lhs + " == " + rhs
		}
		return "!(" + c + ")"
	}
	return "!(" + strings.Join(conds, " || ") + ")"
}

// rangeCond renders a [lo,hi] test against the current byte (c) or rune
func rangeCond(lo, hi rune, ascii bool) (cond string, usedRange bool) {
	varName := "r"
	quote := quoteRune
	if ascii {
		varName = "c"
		quote = quoteByte
	}
	if lo == hi {
		return varName + " == " + quote(lo), false
	}
	return "match.InRange(" + varName + ", " + quote(lo) + ", " + quote(hi) + ")", true
}
