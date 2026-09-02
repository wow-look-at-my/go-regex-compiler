package codegen

const onepassIndexFuncTemplate = `
{{- define "onepassIndexFunc" -}}
{{- if .OPWordFunc }}
// {{ .OPWordFunc }} reports whether r is an ASCII word character ([0-9A-Za-z_]),
// matching the \b/\B word definition used by regexp.
func {{ .OPWordFunc }}(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
{{ end }}
// {{ .IndexFuncName }} returns the submatch index slice for the regex {{ quoteRegex .Regex }},
// or nil if the input does not match. It is a COMPILED one-pass automaton: a
// straight-line state machine that records capture-group byte offsets inline
// while consuming the input in a single left-to-right pass — no NFA program,
// thread lists, or epsilon-closure at run time. The slice layout matches
// regexp.Regexp.FindStringSubmatchIndex: pair (2*g, 2*g+1) is group g's
// [start, end) byte offsets, index 0 is the whole match, and a group that did
// not participate is (-1, -1). It allocates only the result slice and is safe
// for concurrent use (all state is local).
func {{ .IndexFuncName }}(input string) []int {
	var caps [{{ .NumSlots }}]int
	for i := range caps {
		caps[i] = -1
	}
{{- if .OPNeedFirstRune }}
	firstRune := rune(-1)
	if len(input) > 0 {
{{- if .ASCII }}
		firstRune = rune(input[0])
{{- else }}
		firstRune, _ = utf8.DecodeRuneInString(input)
{{- end }}
	}
{{- if eq .OPStartWord 1 }}
	if len(input) == 0 || !{{ .OPWordFunc }}(firstRune) { // leading \b
		return nil
	}
{{- else if eq .OPStartWord 2 }}
	if len(input) > 0 && {{ .OPWordFunc }}(firstRune) { // leading \B
		return nil
	}
{{- end }}
{{- end }}
{{- if .OPNeedLastRune }}
	lastRune := rune(-1)
	if len(input) > 0 {
{{- if .ASCII }}
		lastRune = rune(input[len(input)-1])
{{- else }}
		lastRune, _ = utf8.DecodeLastRuneInString(input)
{{- end }}
	}
{{- end }}
	state := {{ .OPStart }}
{{- if .OPStates }}
{{- if .ASCII }}
	for i := 0; i < len(input); i++ {
		c := input[i]
		switch state {
{{- range .OPStates }}
		case {{ .ID }}:
{{- if .Guard }}
			if {{ .Guard }} { return nil }
			{{ .GuardBody }}
{{- else }}
			switch {
{{- range .Cases }}
			case {{ .Cond }}: {{ .Body }}
{{- end }}
			default: {{ .Default }}
			}
{{- end }}
{{- end }}
		default:
			return nil
		}
	}
{{- else }}
	for i := 0; i < len(input); {
		r, size := utf8.DecodeRuneInString(input[i:])
		if r == utf8.RuneError && size == 1 {
			return nil
		}
		switch state {
{{- range .OPStates }}
		case {{ .ID }}:
{{- if .Guard }}
			if {{ .Guard }} { return nil }
			{{ .GuardBody }}
{{- else }}
			switch {
{{- range .Cases }}
			case {{ .Cond }}: {{ .Body }}
{{- end }}
			default: {{ .Default }}
			}
{{- end }}
{{- end }}
		default:
			return nil
		}
		i += size
	}
{{- end }}
{{- else }}
	if len(input) != 0 {
		return nil
	}
{{- end }}
{{- if .OPHasAccept }}
	switch state {
{{- range .OPAccepts }}
	case {{ .IDs }}:
{{- if eq .Word 1 }}
		if len(input) == 0 || !{{ $.OPWordFunc }}(lastRune) { // trailing \b
			return nil
		}
{{- else if eq .Word 2 }}
		if len(input) > 0 && {{ $.OPWordFunc }}(lastRune) { // trailing \B
			return nil
		}
{{- end }}
{{- if .Body }}
		{{ .Body }}
{{- end }}
{{- end }}
	default:
		return nil
	}
	result := make([]int, {{ .NumSlots }})
	copy(result, caps[:])
	result[0] = 0
	result[1] = len(input)
	return result
{{- else }}
	return nil
{{- end }}
}
{{ end -}}
`
