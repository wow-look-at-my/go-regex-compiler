package codegen

const submatchFuncTemplate = `
{{- define "submatchFunc" -}}
{{- if .Onepass }}
{{ template "onepassIndexFunc" . }}
{{- else }}
{{ template "tdfaIndexFunc" . }}
{{- end }}
{{ template "submatchStringFunc" . }}
{{ template "submatchNamesFunc" . }}
{{- if .EmitStruct }}
{{ template "submatchStructFunc" . }}
{{- end }}
{{ end -}}
`

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
