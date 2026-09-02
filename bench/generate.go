package bench

// The only compiler here is the gosmopolitan fork, so the generator builds as
// an APE. A bare execve of one fails wherever binfmt_misc carries no APE entry,
// which is every GitHub runner; -exec sh hands it the shell that stages it.
//go:generate go run -exec sh ./benchgen
