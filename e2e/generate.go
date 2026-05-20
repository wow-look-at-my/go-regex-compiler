package e2e

//go:generate go run ../cmd/go-regex-compiler -regex "[a-z]+" -func MatchCharClass -package e2e -output gen_charclass.go
//go:generate go run ../cmd/go-regex-compiler -regex "\\d{3}-\\d{2}-\\d{4}" -func MatchSSN -package e2e -output gen_ssn.go
//go:generate go run ../cmd/go-regex-compiler -regex "[A-Za-z_][A-Za-z0-9_]*" -func MatchIdentifier -package e2e -output gen_identifier.go
//go:generate go run ../cmd/go-regex-compiler -regex "(https?://)?[a-z]+\\.[a-z]{2,}" -func MatchURL -package e2e -output gen_url.go
//go:generate go run ../cmd/go-regex-compiler -regex "(?i)hello" -func MatchCaseInsensitive -package e2e -output gen_casei.go
