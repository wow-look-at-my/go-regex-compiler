package codegen

const tdfaIndexFuncTemplate = `
{{- define "tdfaIndexFunc" -}}
// {{ .IndexFuncName }} returns the submatch index slice for the regex {{ quoteRegex .Regex }},
// or nil if the input does not match. It is a COMPILED tagged-DFA register
// machine: a straight-line state machine that records capture-group byte offsets
// into an integer register file in a single left-to-right pass — no NFA program,
// live-position list, or epsilon-closure at run time. Each config that is live
// in a state owns a block of the register file; a transition sets a block's slot
// to the current position when a group boundary is crossed, or copies it from
// the source config's block. At end of input the winning config's block holds
// the capture offsets. The slice layout matches
// regexp.Regexp.FindStringSubmatchIndex: pair (2*g, 2*g+1) is group g's
// [start, end) byte offsets, index 0 is the whole match, and a group that did
// not participate is (-1, -1). It allocates only the result slice and is safe
// for concurrent use (all state is local).
func {{ .IndexFuncName }}(input string) []int {
	var reg [{{ .TDRegCount }}]int
	for i := range reg {
		reg[i] = -1
	}
{{- range .TDStartInit }}
	{{ . }}
{{- end }}
	state := {{ .TDStart }}
{{- if .TDStates }}
{{- if .ASCII }}
	for i := 0; i < len(input); i++ {
		c := input[i]
{{- if .TDUsesPos }}
		np := i + 1
{{- end }}
		switch state {
{{- range .TDStates }}
		case {{ .ID }}:
{{- if .Guard }}
			if {{ .Guard }} { return nil }
			{{ .GuardBody }}
{{- else }}
			switch {
{{- range .Cases }}
			case {{ .Cond }}: {{ .Body }}
{{- end }}
			default:
				return nil
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
{{- if .TDUsesPos }}
		np := i + size
{{- end }}
		switch state {
{{- range .TDStates }}
		case {{ .ID }}:
{{- if .Guard }}
			if {{ .Guard }} { return nil }
			{{ .GuardBody }}
{{- else }}
			switch {
{{- range .Cases }}
			case {{ .Cond }}: {{ .Body }}
{{- end }}
			default:
				return nil
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
{{- if .TDHasAccept }}
	switch state {
{{- range .TDAccepts }}
	case {{ .IDs }}:
		result := make([]int, {{ $.NumSlots }})
		result[0] = 0
		result[1] = len(input)
		copy(result[2:], reg[{{ .ReadLo }}:{{ .ReadHi }}])
		return result
{{- end }}
	default:
		return nil
	}
{{- else }}
	return nil
{{- end }}
}
{{ end -}}
`
