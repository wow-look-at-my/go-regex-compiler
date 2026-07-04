package codegen

// This file lowers a capDFA (see onepass.go) into the flat, template-ready
// structures the compiled-submatch templates render (see templates_onepass.go).
// All Go-syntax decisions — byte vs. rune conditions, capture-write statements,
// accept grouping — happen here so the templates stay declarative.

import (
	"sort"
	"strconv"
	"strings"
)

// onepassEmitState is one `case <id>:` of the compiled matcher's state switch.
type onepassEmitState struct {
	ID      int
	Cases   []onepassEmitCase
	Default string // default clause: "return nil", or the write+goto for an any-rune edge
}

// onepassEmitCase is one `case <cond>: <body>` of a state's inner switch.
type onepassEmitCase struct {
	Cond string
	Body string
}

// onepassAccept groups accepting states that share the same end-of-input capture
// writes, so they can collapse into a single `case a, b:` clause.
type onepassAccept struct {
	IDs  string // "3, 5"
	Body string // "caps[5] = len(input)", possibly empty
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
		for _, e := range st.edges {
			body := onepassEdgeBody(e.writes, e.next)
			if e.anyRune {
				es.Default = body // any rune takes this edge
				continue
			}
			cond, usedRange := onepassEdgeCond(e, d.ascii)
			hasRanges = hasRanges || usedRange
			es.Cases = append(es.Cases, onepassEmitCase{Cond: cond, Body: body})
		}
		ctx.OPStates = append(ctx.OPStates, es)
	}
	ctx.HasRanges = hasRanges

	// Group accepting states by their end-of-input write body.
	byBody := make(map[string][]int)
	var order []string
	for _, st := range d.states {
		if !st.accept {
			continue
		}
		body := onepassAcceptBody(st.acceptWrites)
		if _, ok := byBody[body]; !ok {
			order = append(order, body)
		}
		byBody[body] = append(byBody[body], st.id)
	}
	for _, body := range order {
		ids := byBody[body]
		sort.Ints(ids)
		strIDs := make([]string, len(ids))
		for i, id := range ids {
			strIDs[i] = strconv.Itoa(id)
		}
		ctx.OPAccepts = append(ctx.OPAccepts, onepassAccept{IDs: strings.Join(strIDs, ", "), Body: body})
	}
	ctx.OPHasAccept = len(ctx.OPAccepts) > 0
}

// onepassEdgeBody renders the capture writes plus the state assignment taken when
// an edge fires: e.g. "caps[3] = i; caps[4] = i; state = 5". Writes record the
// current byte offset i (the position BEFORE the rune is consumed).
func onepassEdgeBody(writes []int, next int) string {
	var sb strings.Builder
	for _, w := range writes {
		if w == 0 || w == 1 {
			continue // group 0 is forced from the match bounds
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
// accepting state (its Match config's pending captures), recorded at len(input).
func onepassAcceptBody(writes []int) string {
	var parts []string
	for _, w := range writes {
		if w == 0 || w == 1 {
			continue // group 0 is forced from the match bounds
		}
		parts = append(parts, "caps["+strconv.Itoa(w)+"] = len(input)")
	}
	return strings.Join(parts, "; ")
}

// onepassEdgeCond builds the case condition for a consuming edge and reports
// whether it used match.InRange (so the caller can flag the import). anyRune
// edges never reach here (they become the switch default).
func onepassEdgeCond(e capEdge, ascii bool) (cond string, usedRange bool) {
	if e.anyNotNL {
		return "r != '\\n'", false
	}
	parts := make([]string, 0, len(e.ranges))
	for _, r := range e.ranges {
		c, ur := rangeCond(r.lo, r.hi, ascii)
		usedRange = usedRange || ur
		parts = append(parts, c)
	}
	return strings.Join(parts, ", "), usedRange
}

// rangeCond renders a single [lo,hi] test against the current byte (c) or rune
// (r), reporting whether it emitted a match.InRange call.
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
