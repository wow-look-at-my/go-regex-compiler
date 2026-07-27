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

	// Guard/GuardBody are set for a single-transition state — exactly one
	// conditional edge and a "return nil" default (no any-rune fallback). The
	// template then emits an early-return guard instead of a one-case switch:
	//   if <Guard> { return nil }
	//   <GuardBody>
	// Guard is the negation of the edge condition; GuardBody is its write+goto.
	Guard     string
	GuardBody string
}

// onepassEmitCase is one `case <cond>: <body>` of a state's inner switch.
type onepassEmitCase struct {
	Cond string
	Body string
}

// onepassAccept groups accepting states that share the same end-of-input capture
// writes and word-boundary requirement, so they collapse into one `case a, b:`.
type onepassAccept struct {
	IDs  string // "3, 5"
	Body string // "caps[5] = len(input)", possibly empty
	Word int    // 0 none, 1 last rune must be a word char (\b), 2 must not (\B)
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
		// A single conditional edge with a plain "return nil" default (no any-rune
		// fallback) becomes an early-return guard, not a one-case switch.
		if len(es.Cases) == 1 && !hasAnyRune {
			es.Guard = negateConds(lastConds)
			es.GuardBody = es.Cases[0].Body
		}
		ctx.OPStates = append(ctx.OPStates, es)
	}
	ctx.HasRanges = hasRanges

	// Start gate: a word-boundary assertion (\b/\B) required at position 0. Text
	// anchors (^ \A) are always satisfied there and were masked off in buildCapDFA.
	ctx.OPStartWord = wordReq(d.startGate)
	needFirst := ctx.OPStartWord != 0

	// Group accepting states by their end-of-input write body AND word-boundary
	// requirement (\b/\B at the end, e.g. `(\w+)\b`); text anchors ($ \z) are
	// always satisfied at end-of-input.
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
// 1 => the boundary rune must be a word char (\b), 2 => must not be (\B), 0 =>
// no word-boundary constraint. Text-anchor bits are ignored (always satisfied at
// the position they guard).
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

// onepassEdgeConds builds the case-condition alternatives for a consuming edge
// (a Go `case A, B:` matches A || B) and reports whether any used match.InRange
// (so the caller can flag the import). anyRune edges never reach here (they
// become the switch default).
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
// condition alternatives match (a Go `case A, B:` matches A || B), used to
// early-return from a single-transition state. A lone comparison is negated by
// flipping its operator (`c == 'b'` -> `c != 'b'`, `r != '\n'` -> `r == '\n'`);
// anything else (a range test, or multiple alternatives) is wrapped as `!(...)`
// around the exact positive condition, which is always correct regardless of
// shape — no fragile hand-rolled De Morgan.
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
