package bench

//go:generate go run ../cmd/go-regex-compiler -regex "abc" -func MatchLiteral -package bench -output gen_literal.go
//go:generate go run ../cmd/go-regex-compiler -regex "[a-z]+" -func MatchCharClass -package bench -output gen_charclass.go
//go:generate go run ../cmd/go-regex-compiler -regex "\d{3}-\d{2}-\d{4}" -func MatchSSN -package bench -output gen_ssn.go
//go:generate go run ../cmd/go-regex-compiler -regex "[a-z]+@[a-z]+\.[a-z]{2,}" -func MatchEmail -package bench -output gen_email.go
//go:generate go run ../cmd/go-regex-compiler -regex "[A-Za-z_][A-Za-z0-9_]*" -func MatchIdentifier -package bench -output gen_identifier.go
//go:generate go run ../cmd/go-regex-compiler -regex "(https?://)?[a-z]+\.[a-z]{2,}" -func MatchURL -package bench -output gen_url.go
//go:generate go run ../cmd/go-regex-compiler -regex "(?i)hello" -func MatchCaseInsensitive -package bench -output gen_casei.go
//go:generate go run ../cmd/go-regex-compiler -regex "#[0-9a-f]{6}" -func MatchHexColor -package bench -output gen_hexcolor.go
