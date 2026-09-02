package bench

// The only compiler here is the gosmopolitan fork, so the generator builds as
// an APE. The kernel cannot execve an APE unless binfmt_misc carries an entry
// for it, which no GitHub runner does; -exec sh hands it the shell that stages
// it.
//go:generate go run -exec sh ./benchgen
