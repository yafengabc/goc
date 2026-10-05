// The self-contained toolchain: package compiler plus the C library embedded
// in the binary. See main.go for why this exists alongside src/goc, which
// builds the same compiler reading the library from disk.
//
// This module depends on src/goc; src/goc does not depend on this one. A
// second compiler (src/gocl, planned) will follow the same shape -- depend on
// goc/frontend and on the shared pipeline pieces -- and this one is the working
// example of that arrangement.
module goc/selfcontained

go 1.21

require goc v0.0.0

require (
	goa v0.0.0 // indirect
	goc/common v0.0.0 // indirect
	goc/frontend v0.0.0 // indirect
)

// The three replaces below all point inside the repository. Each name is
// dotless, which Go only tolerates for a module resolved locally -- the tidy
// error "malformed module path: missing dot in first path element" is what a
// missing replace looks like, and it is the only symptom.
replace goc => ./goc

replace goc/common => ./common

replace goc/frontend => ./frontend

replace goa => ./goa
