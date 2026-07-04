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
{{- if .Onepass }}
{{ template "onepassIndexFunc" . }}
{{- else if .TDFA }}
{{ template "tdfaIndexFunc" . }}
{{- else }}
{{ template "nfaPackageDecls" . }}
{{ template "submatchIndexFunc" . }}
{{- end }}
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

// NFA op codes (mirror the numeric ops baked into {{ .Priv }}Prog). Hoisted to
// package scope so the recursive closure below is a plain method (no per-call
// closure allocation) and inlines the constants directly.
const (
	{{ .Priv }}OpRune      = 0
	{{ .Priv }}OpRune1     = 1
	{{ .Priv }}OpRuneAny   = 2
	{{ .Priv }}OpRuneAnyNL = 3
	{{ .Priv }}OpAlt       = 4
	{{ .Priv }}OpCapture   = 5
	{{ .Priv }}OpMatch     = 6
	{{ .Priv }}OpNop       = 7
	{{ .Priv }}OpFail      = 8
	{{ .Priv }}OpEmpty     = 9
)

// Empty-width assertion bits (from regexp/syntax.EmptyOp), inlined so the
// generated code does not import regexp/syntax.
const (
	{{ .Priv }}EmptyBeginLine      = 1
	{{ .Priv }}EmptyEndLine        = 2
	{{ .Priv }}EmptyBeginText      = 4
	{{ .Priv }}EmptyEndText        = 8
	{{ .Priv }}EmptyWordBoundary   = 16
	{{ .Priv }}EmptyNoWordBoundary = 32
)

// {{ .Priv }}IsWordChar reports whether r is a word character ([0-9A-Za-z_]).
func {{ .Priv }}IsWordChar(r rune) bool {
	return r == '_' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

// {{ .Priv }}EmptyOpsAt computes the satisfied empty-width assertion bits at the
// boundary between the rune before (bp) and the rune after (ap) the current
// position. bp/ap are -1 at the text edges.
func {{ .Priv }}EmptyOpsAt(bp, ap rune) int {
	var op int
	if bp < 0 {
		op |= {{ .Priv }}EmptyBeginText | {{ .Priv }}EmptyBeginLine
	}
	if bp == '\n' {
		op |= {{ .Priv }}EmptyBeginLine
	}
	if ap < 0 {
		op |= {{ .Priv }}EmptyEndText | {{ .Priv }}EmptyEndLine
	}
	if ap == '\n' {
		op |= {{ .Priv }}EmptyEndLine
	}
	beforeWord := bp >= 0 && {{ .Priv }}IsWordChar(bp)
	afterWord := ap >= 0 && {{ .Priv }}IsWordChar(ap)
	if beforeWord != afterWord {
		op |= {{ .Priv }}EmptyWordBoundary
	} else {
		op |= {{ .Priv }}EmptyNoWordBoundary
	}
	return op
}

// {{ .Priv }}RuneMatch reports whether the consuming instruction inst matches
// rune r (opRune scans the class's [lo,hi] range pairs; a trailing odd rune is a
// singleton).
func {{ .Priv }}RuneMatch(inst *{{ .Priv }}Inst, r rune) bool {
	switch inst.op {
	case {{ .Priv }}OpRuneAny:
		return true
	case {{ .Priv }}OpRuneAnyNL:
		return r != '\n'
	case {{ .Priv }}OpRune1:
		return len(inst.runes) > 0 && inst.runes[0] == r
	case {{ .Priv }}OpRune:
		for i := 0; i < len(inst.runes)-1; i += 2 {
			if r >= inst.runes[i] && r <= inst.runes[i+1] {
				return true
			}
		}
		if len(inst.runes)%2 == 1 {
			return r == inst.runes[len(inst.runes)-1]
		}
		return false
	}
	return false
}

// {{ .Priv }}Thread is a live NFA thread: a program counter plus a snapshot of
// its capture slots. The snapshot is copied out of the shared work vector only
// when the thread is actually enqueued (once per surviving thread per position),
// not on every epsilon-closure step.
type {{ .Priv }}Thread struct {
	pc   int
	caps [{{ .NumSlots }}]int
}

// {{ .Priv }}Scratch is the reusable per-call working set for {{ .IndexFuncName }},
// drawn from a sync.Pool so a steady-state call allocates only its result slice.
// visited is a generation-stamped seen-set: visited[pc] == gen means pc was
// already enqueued during the current epsilon-closure, replacing a per-position
// map[int]bool (no per-position allocation, no map hashing on the hot path).
// work holds the single capture vector threaded through the recursive closure:
// captures are written in place on descent and restored on unwind, so a fork of
// the closure costs a pc, not a 2*(N+1)-int copy. before/after/ops carry the
// boundary context for the empty-width assertions at the current position.
type {{ .Priv }}Scratch struct {
	visited []uint32
	gen     uint32
	work    [{{ .NumSlots }}]int
	ops     int
	before  rune
	after   rune
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
		cur:     make([]{{ .Priv }}Thread, 0, n),
		next:    make([]{{ .Priv }}Thread, 0, n),
	}
}}

// seed resets the per-position closure state (generation stamp, work vector,
// boundary context) and computes the epsilon-closure of pc into list. caps is
// the starting capture snapshot of the thread being advanced; it is copied into
// work exactly once here.
func (sc *{{ .Priv }}Scratch) seed(list []{{ .Priv }}Thread, caps *[{{ .NumSlots }}]int, pc, pos int, before, after rune) []{{ .Priv }}Thread {
	sc.gen++
	if sc.gen == 0 { // counter wrapped: clear stale stamps and restart
		for i := range sc.visited {
			sc.visited[i] = 0
		}
		sc.gen = 1
	}
	sc.work = *caps
	sc.ops = -1
	sc.before = before
	sc.after = after
	return sc.addThread(list, pc, pos)
}

// addThread computes the epsilon-closure of pc, appending every reachable
// consuming (or match) instruction to list and returning the extended list.
// Thread priority is preserved (leftmost-first, matching Go's default regexp
// engine): the high-priority branch (out) is explored before the low-priority
// branch (arg) of each alternation, and the generation-stamped visited set makes
// the first (highest-priority) path to a pc win. Captures are tracked in the
// shared work vector with save/restore, so descent never copies the capture set;
// the only copy is the snapshot taken when a thread is enqueued.
func (sc *{{ .Priv }}Scratch) addThread(list []{{ .Priv }}Thread, pc, pos int) []{{ .Priv }}Thread {
	// Single-successor ops (opNop, a satisfied opEmpty, opAlt's low-priority
	// branch, an out-of-range opCapture) advance pc in place; only opAlt's high
	// branch and an in-range opCapture recurse, halving the call count of a naive
	// per-instruction recursion. Go has no tail-call optimization, so this loop
	// is a measurable win over recursing on every edge.
	for {
		if pc < 0 || pc >= len({{ .Priv }}Prog) || sc.visited[pc] == sc.gen {
			return list
		}
		sc.visited[pc] = sc.gen
		inst := &{{ .Priv }}Prog[pc]
		switch inst.op {
		case {{ .Priv }}OpAlt:
			// High priority (out) first (recurse), then low priority (arg: loop).
			list = sc.addThread(list, inst.out, pos)
			pc = inst.arg
		case {{ .Priv }}OpNop:
			pc = inst.out
		case {{ .Priv }}OpEmpty:
			if sc.ops < 0 {
				sc.ops = {{ .Priv }}EmptyOpsAt(sc.before, sc.after)
			}
			// The assertion holds iff every required bit is satisfied.
			if inst.arg&^sc.ops != 0 {
				return list
			}
			pc = inst.out
		case {{ .Priv }}OpCapture:
			if inst.arg < {{ .NumSlots }} {
				saved := sc.work[inst.arg]
				sc.work[inst.arg] = pos
				list = sc.addThread(list, inst.out, pos)
				sc.work[inst.arg] = saved // restore on unwind
				return list
			}
			pc = inst.out
		default:
			// Consuming (opRune*) or opMatch: enqueue with a snapshot of the
			// captures accumulated along this (highest-priority) path.
			return append(list, {{ .Priv }}Thread{pc: pc, caps: sc.work})
		}
	}
}
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
	// Borrow a reusable working set. The pooled buffers (visited/work/cur/next)
	// are the only mutable state, so no allocation-heavy per-position scratch is
	// needed and concurrent calls each get their own set.
	sc := {{ .Priv }}Pool.Get().(*{{ .Priv }}Scratch)
	defer {{ .Priv }}Pool.Put(sc)

	var initCaps [{{ .NumSlots }}]int
	for i := range initCaps {
		initCaps[i] = -1
	}
	initCaps[0] = 0 // group 0 start = beginning of input

	// Peek the first rune (range decodes UTF-8) for the seed's "after" context;
	// no unicode/utf8 import is needed. range already fast-paths single-byte
	// (ASCII) runes, so a dedicated byte loop was measured and dropped: it beat
	// this path by only ~3% on long ASCII input and lost on short (the capture
	// closure, not the decode, dominates — see the PR notes).
	var firstRune rune = -1
	for _, r := range input {
		firstRune = r
		break
	}

	// Seed at position 0. before = -1 (text begin); after = first rune (or -1).
	cur := sc.seed(sc.cur[:0], &initCaps, {{ .Priv }}StartPC, 0, -1, firstRune)
	next := sc.next[:0]

	// Single pass over the input. A rune consumed at this step ends exactly where
	// the next rune begins, so range's byte offset i doubles as the post-consume
	// position; prevRune carries the "before" context and r the "after".
	havePrev := false
	var prevRune rune
	for i, r := range input {
		if havePrev {
			next = next[:0]
			for k := range cur {
				t := &cur[k]
				inst := &{{ .Priv }}Prog[t.pc]
				if {{ .Priv }}RuneMatch(inst, prevRune) {
					next = sc.seed(next, &t.caps, inst.out, i, prevRune, r)
				}
			}
			cur, next = next, cur
		}
		prevRune = r
		havePrev = true
	}
	if havePrev { // consume the final rune; after = -1 (text end)
		next = next[:0]
		for k := range cur {
			t := &cur[k]
			inst := &{{ .Priv }}Prog[t.pc]
			if {{ .Priv }}RuneMatch(inst, prevRune) {
				next = sc.seed(next, &t.caps, inst.out, len(input), prevRune, -1)
			}
		}
		cur, next = next, cur
	}
	sc.cur = cur[:0]   // retain both backing arrays for the next pooled use
	sc.next = next[:0]

	for k := range cur {
		t := &cur[k]
		if t.pc < len({{ .Priv }}Prog) && {{ .Priv }}Prog[t.pc].op == {{ .Priv }}OpMatch {
			result := make([]int, {{ .NumSlots }})
			copy(result, t.caps[:])
			result[1] = len(input) // group 0 end
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
