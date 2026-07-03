package codegen

// This file holds the text/template definitions for the submatch (capture
// extraction) function family: the package-level NFA program/scratch pool
// declarations plus the FindXxxIndex core and its thin accessor wrappers. They
// are concatenated into allTemplates in templates.go and are split out here to
// keep each source file within the toolchain length limit.

// ---------- submatch ----------
//
// The submatch family shares a single Thompson NFA core, emitted once in the
// <FuncName>Index function. The other accessors are thin wrappers over it so
// the (potentially large) instruction table is never duplicated.

const submatchFuncTemplate = `
{{- define "submatchFunc" -}}
{{ template "nfaPackageDecls" . }}
{{ template "submatchIndexFunc" . }}
{{ template "submatchStringFunc" . }}
{{ template "submatchNamesFunc" . }}
{{- if .EmitStruct }}
{{ template "submatchStructFunc" . }}
{{- end }}
{{ end -}}
`

// ---------- index core ----------

const nfaPackageDeclsTemplate = `
{{- define "nfaPackageDecls" -}}
// {{ .Priv }}Inst is one instruction of the Thompson NFA program used by
// {{ .IndexFuncName }}. op codes: 0 opRune, 1 opRune1, 2 opRuneAny,
// 3 opRuneAnyNL, 4 opAlt, 5 opCapture, 6 opMatch, 7 opNop, 8 opFail, 9 opEmpty.
type {{ .Priv }}Inst struct {
	op    int
	out   int
	arg   int
	runes []rune
}

// {{ .Priv }}Prog is the immutable NFA program for {{ .IndexFuncName }}. It is
// built once at package initialization (including its rune-class tables) and is
// never mutated, so the hot path allocates no per-call instruction table and is
// safe to share across concurrent calls.
var {{ .Priv }}Prog = []{{ .Priv }}Inst{
{{- range .Instructions }}
	/* {{ .Index }}: {{ .OpName }} */ {op: {{ .OpCode }}, out: {{ .Out }}, arg: {{ .Arg }}{{ if .Runes }}, runes: {{ .Runes }}{{ end }}},
{{- end }}
}

// {{ .Priv }}StartPC is the program counter the simulation seeds from.
const {{ .Priv }}StartPC = {{ .StartPC }}

// {{ .Priv }}Thread is a live NFA thread: a program counter plus capture slots.
type {{ .Priv }}Thread struct {
	pc   int
	caps [{{ .NumSlots }}]int
}

// {{ .Priv }}Frame is a work item on the epsilon-closure DFS stack.
type {{ .Priv }}Frame struct {
	pc   int
	caps [{{ .NumSlots }}]int
}

// {{ .Priv }}Scratch is the reusable per-call working set for {{ .IndexFuncName }},
// drawn from a sync.Pool so a steady-state call allocates only its result slice.
// visited is a generation-stamped seen-set: visited[pc] == gen means pc was
// already enqueued during the current addThread call, replacing a per-position
// map[int]bool (no per-position allocation, no map hashing on the hot path).
type {{ .Priv }}Scratch struct {
	visited []uint32
	gen     uint32
	stack   []{{ .Priv }}Frame
	cur     []{{ .Priv }}Thread
	next    []{{ .Priv }}Thread
}

// {{ .Priv }}Pool recycles {{ .Priv }}Scratch working sets across calls. The
// pooled buffers are per-call mutable state (never shared while in use), so
// concurrent {{ .IndexFuncName }} calls each borrow their own and never race.
var {{ .Priv }}Pool = sync.Pool{New: func() any {
	n := len({{ .Priv }}Prog)
	return &{{ .Priv }}Scratch{
		visited: make([]uint32, n),
		stack:   make([]{{ .Priv }}Frame, 0, n),
		cur:     make([]{{ .Priv }}Thread, 0, n),
		next:    make([]{{ .Priv }}Thread, 0, n),
	}
}}
{{ end -}}
`

const submatchIndexFuncTemplate = `
{{- define "submatchIndexFunc" -}}
// {{ .IndexFuncName }} returns the submatch index slice for the regex {{ quoteRegex .Regex }},
// or nil if the input does not match. The slice has length 2*(N+1) where N is
// the number of capture groups: pair (2*g, 2*g+1) holds the absolute byte
// offsets [start, end) of group g, with index 0 being the whole match. A
// non-participating group has the pair (-1, -1). This is parity with
// regexp.Regexp.FindStringSubmatchIndex.
func {{ .IndexFuncName }}(input string) []int {
	if !{{ .MatchFunc }}(input) {
		return nil
	}
{{ template "nfaSim" . }}
}
{{ end -}}
`

const nfaSimTemplate = `
{{- define "nfaSim" -}}
	// NFA op codes (mirror the numeric ops baked into {{ .Priv }}Prog).
	const (
		opRune      = 0
		opRune1     = 1
		opRuneAny   = 2
		opRuneAnyNL = 3
		opAlt       = 4
		opCapture   = 5
		opMatch     = 6
		opNop       = 7
		opFail      = 8
		opEmpty     = 9
	)

	// Empty-width assertion bits (from regexp/syntax.EmptyOp), inlined so the
	// generated code does not import regexp/syntax.
	const (
		emptyBeginLine      = 1
		emptyEndLine        = 2
		emptyBeginText      = 4
		emptyEndText        = 8
		emptyWordBoundary   = 16
		emptyNoWordBoundary = 32
	)

	// isWordChar reports whether r is a word character ([0-9A-Za-z_]).
	isWordChar := func(r rune) bool {
		return r == '_' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
	}

	// emptyOpsAt computes the satisfied empty-width assertion bits at the
	// boundary between the rune before (bp) and the rune after (ap) the current
	// position. bp/ap are -1 at the text edges.
	emptyOpsAt := func(bp, ap rune) int {
		var op int
		if bp < 0 {
			op |= emptyBeginText | emptyBeginLine
		}
		if bp == '\n' {
			op |= emptyBeginLine
		}
		if ap < 0 {
			op |= emptyEndText | emptyEndLine
		}
		if ap == '\n' {
			op |= emptyEndLine
		}
		beforeWord := bp >= 0 && isWordChar(bp)
		afterWord := ap >= 0 && isWordChar(ap)
		if beforeWord != afterWord {
			op |= emptyWordBoundary
		} else {
			op |= emptyNoWordBoundary
		}
		return op
	}

	// Borrow a reusable working set. The pooled buffers (visited/stack/cur/next)
	// are the only mutable state, so no allocation-heavy per-position scratch is
	// needed and concurrent calls each get their own set.
	sc := {{ .Priv }}Pool.Get().(*{{ .Priv }}Scratch)
	defer {{ .Priv }}Pool.Put(sc)

	// addThread computes the epsilon-closure of pc into list, tracking captures.
	// Thread priority is preserved (leftmost-first, matching Go's default regexp
	// engine): the DFS explores the high-priority branch (out) before the
	// low-priority branch (arg) of each alternation. Visited membership uses a
	// generation stamp so the scratch is reused without clearing between calls.
	addThread := func(list []{{ .Priv }}Thread, pc int, caps [{{ .NumSlots }}]int, pos int, before, after rune) []{{ .Priv }}Thread {
		sc.gen++
		if sc.gen == 0 { // counter wrapped: clear stale stamps and restart
			for i := range sc.visited {
				sc.visited[i] = 0
			}
			sc.gen = 1
		}
		gen := sc.gen
		stack := sc.stack[:0]
		stack = append(stack, {{ .Priv }}Frame{pc: pc, caps: caps})
		// emptyOps for this position, computed lazily on the first opEmpty.
		ops := -1
		for len(stack) > 0 {
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if f.pc < 0 || f.pc >= len({{ .Priv }}Prog) || sc.visited[f.pc] == gen {
				continue
			}
			sc.visited[f.pc] = gen
			inst := &{{ .Priv }}Prog[f.pc]
			switch inst.op {
			case opAlt:
				// Push low priority first so high priority (out) is processed first.
				stack = append(stack, {{ .Priv }}Frame{pc: inst.arg, caps: f.caps})
				stack = append(stack, {{ .Priv }}Frame{pc: inst.out, caps: f.caps})
			case opNop:
				stack = append(stack, {{ .Priv }}Frame{pc: inst.out, caps: f.caps})
			case opEmpty:
				if ops < 0 {
					ops = emptyOpsAt(before, after)
				}
				// The assertion holds iff every required bit is satisfied.
				if inst.arg&^ops == 0 {
					stack = append(stack, {{ .Priv }}Frame{pc: inst.out, caps: f.caps})
				}
			case opCapture:
				newCaps := f.caps
				if inst.arg < {{ .NumSlots }} {
					newCaps[inst.arg] = pos
				}
				stack = append(stack, {{ .Priv }}Frame{pc: inst.out, caps: newCaps})
			default:
				list = append(list, {{ .Priv }}Thread{pc: f.pc, caps: f.caps})
			}
		}
		sc.stack = stack // retain the (possibly grown) backing array
		return list
	}

	runeMatch := func(inst *{{ .Priv }}Inst, r rune) bool {
		switch inst.op {
		case opRuneAny:
			return true
		case opRuneAnyNL:
			return r != '\n'
		case opRune1:
			if len(inst.runes) > 0 && inst.runes[0] == r {
				return true
			}
			return false
		case opRune:
			for i := 0; i < len(inst.runes)-1; i += 2 {
				if r >= inst.runes[i] && r <= inst.runes[i+1] {
					return true
				}
			}
			if len(inst.runes)%2 == 1 {
				if r == inst.runes[len(inst.runes)-1] {
					return true
				}
			}
			return false
		}
		return false
	}

	var initCaps [{{ .NumSlots }}]int
	for i := range initCaps {
		initCaps[i] = -1
	}
	initCaps[0] = 0 // group 0 start = beginning of input

	// Peek the first rune (range decodes UTF-8) for the seed's "after" context;
	// no unicode/utf8 import is needed.
	var firstRune rune = -1
	for _, r := range input {
		firstRune = r
		break
	}

	// Seed at position 0. before = -1 (text begin); after = first rune (or -1).
	cur := addThread(sc.cur[:0], {{ .Priv }}StartPC, initCaps, 0, -1, firstRune)
	next := sc.next[:0]

	// Single pass over the input. A rune consumed at this step ends exactly where
	// the next rune begins, so range's byte offset i doubles as the post-consume
	// position; prevRune carries the "before" context and r the "after".
	havePrev := false
	var prevRune rune
	for i, r := range input {
		if havePrev {
			next = next[:0]
			for _, t := range cur {
				inst := &{{ .Priv }}Prog[t.pc]
				if runeMatch(inst, prevRune) {
					next = addThread(next, inst.out, t.caps, i, prevRune, r)
				}
			}
			cur, next = next, cur
		}
		prevRune = r
		havePrev = true
	}
	if havePrev { // consume the final rune; after = -1 (text end)
		next = next[:0]
		for _, t := range cur {
			inst := &{{ .Priv }}Prog[t.pc]
			if runeMatch(inst, prevRune) {
				next = addThread(next, inst.out, t.caps, len(input), prevRune, -1)
			}
		}
		cur, next = next, cur
	}
	sc.cur = cur[:0]   // retain both backing arrays for the next pooled use
	sc.next = next[:0]

	for _, t := range cur {
		if t.pc < len({{ .Priv }}Prog) && {{ .Priv }}Prog[t.pc].op == opMatch {
			t.caps[1] = len(input) // group 0 end
			result := make([]int, {{ .NumSlots }})
			copy(result, t.caps[:])
			return result
		}
	}
	return nil
{{- end -}}
`

// ---------- string wrapper ----------

const submatchStringFuncTemplate = `
{{- define "submatchStringFunc" -}}
// {{ .FuncName }} returns captured groups for the regex {{ quoteRegex .Regex }}.
// Returns nil if the input does not match. Index 0 is the entire match,
// indices 1..N are capture groups. A group that did not participate in the
// match yields "" (parity with regexp.Regexp.FindStringSubmatch); use
// {{ .IndexFuncName }} to distinguish an absent group (offset pair -1) from an
// empty one.
func {{ .FuncName }}(input string) []string {
	idx := {{ .IndexFuncName }}(input)
	if idx == nil {
		return nil
	}
	result := make([]string, {{ .NumGroups }})
	for g := 0; g < {{ .NumGroups }}; g++ {
		s, e := idx[g*2], idx[g*2+1]
		if s >= 0 && e >= 0 && s <= len(input) && e <= len(input) {
			result[g] = input[s:e]
		}
	}
	return result
}
{{ end -}}
`

// ---------- names accessor ----------

const submatchNamesFuncTemplate = `
{{- define "submatchNamesFunc" -}}
// {{ .NamesFuncName }} returns the names of the capture groups for the regex
// {{ quoteRegex .Regex }}. The slice has one entry per group index: index 0 (the
// whole match) is always "", and an unnamed group is "". This is parity with
// regexp.Regexp.SubexpNames.
func {{ .NamesFuncName }}() []string {
	return []string{{"{"}}{{ range $i, $n := .GroupNames }}{{ if $i }}, {{ end }}{{ goString $n }}{{ end }}}
}
{{ end -}}
`

// ---------- typed struct ----------

const submatchStructFuncTemplate = `
{{- define "submatchStructFunc" -}}
// {{ .StructType }} holds the named capture groups extracted from a match.
// Matched reports whether the input matched at all (distinguishing a non-match
// from a match where every named group is empty).
//
// NOTE: two regex group names that differ only by the case of their first
// rune (e.g. "ip" and "Ip") collide into a single exported field here; the
// last such group wins. See the README for details.
type {{ .StructType }} struct {
{{- range .StructFields }}
	{{ .Name }} string
{{- end }}
	Matched bool
}

// {{ .StructFunc }} extracts the named capture groups of the regex
// {{ quoteRegex .Regex }} from input. On no match it returns the zero value
// ({{ .StructType }}{Matched: false} with all fields ""). Otherwise each field
// is filled from its group (an unmatched optional group yields "").
func {{ .StructFunc }}(input string) {{ .StructType }} {
	groups := {{ .FuncName }}(input)
	if groups == nil {
		return {{ .StructType }}{}
	}
	return {{ .StructType }}{
{{- range .StructFields }}
		{{ .Name }}: groups[{{ .Group }}],
{{- end }}
		Matched: true,
	}
}
{{ end -}}
`
