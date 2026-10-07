// The self-contained toolchain: one entry point that builds either compiler --
// goc's own x86-64 back end or gocl's LLVM back end -- with the C library
// embedded in the binary.
//
// One module, two binaries, selected by a build tag:
//
//	-tags goc    -> goc.go   calls goc's compiler.Main
//	-tags gocl   -> gocl.go  calls gocl's driver Main
//
// The embedded library and its adapter live in libembed.go, which carries no
// tag so both drivers share it. Embedding is the only build now: an earlier
// arrangement also produced per-module binaries that read goclib/ from disk,
// which is a fine trade during development (edit the library, rebuild nothing)
// and the wrong one for shipping. Those entry points are gone; goc.go's header
// records what the self-contained build is for.
//
// This module depends on src/goc and src/gocl; neither depends on this one.
module goc/selfcontained

go 1.21

require (
	goc v0.0.0
	gocl v0.0.0
)

require (
	goa v0.0.0 // indirect
	goc/common v0.0.0 // indirect
	goc/frontend v0.0.0 // indirect
	gocld v0.0.0 // indirect: goa reaches the linker
)

// The replaces below all point inside the repository. Each name is dotless,
// which Go only tolerates for a module resolved locally -- the tidy error
// "malformed module path: missing dot in first path element" is what a missing
// replace looks like, and it is the only symptom.
replace goc => ./goc

replace gocl => ./gocl

replace goc/common => ./common

replace goc/frontend => ./frontend

replace goa => ./goa

replace gocld => ./gocld
