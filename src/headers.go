// This file is intentionally empty of declarations.
//
// It exists so that `//go:embed goclib/*` in libembed.go can be made to
// re-read the directory after a new header is added there. go:embed resolves
// its pattern when the *package* is compiled, and a build that changes only
// files under goclib/ leaves the embedded FS cached from the previous build --
// so a newly added goclib/*.h is simply absent, and the compiler then reports
// every symbol it header declares as missing, which reads like a compiler bug
// rather than a stale embed. Touching this file invalidates the package and
// forces the embed to run again.
//
// Adding a goclib header therefore means:
//
//	touch src/headers.go && bash build.sh
package main
