//go:build gocl

// Command gocl is the C compiler whose back end is LLVM, built from the same
// entry point as goc (see goc.go).
//
// Like goc, the C library is embedded (see libembed.go) and installed before
// Main runs, so the resulting binary needs nothing beside it but libLLVM. The
// only difference from the goc build is which code generator the driver wires
// in: goc's own x86-64 emitter versus libLLVM. The driver itself (flags, build
// sequence) is gocl.Main, which lives in package gocl precisely so the entry
// point stays a three-line shell.
//
// Build it with `-tags gocl`.
package main

import (
	"os"

	"goc/common"

	"gocl"
)

func main() {
	common.SetLibrary(embeddedLib{})
	os.Exit(gocl.Main(os.Args[1:]))
}
