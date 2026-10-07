// The C library, embedded so a binary built from this directory needs nothing
// beside it: carry the exe and it compiles C.
//
// The pattern is relative to this file's directory and cannot escape it, which
// is the entire reason this entry point lives at src/ rather than in
// src/goc/cmd/: go:embed needs goclib/ to be a sibling of the .go file that
// embeds it.
//
// This file carries no build tag on purpose: both the goc and the gocl drivers
// (selected by //go:build goc / //go:build gocl in their own files) embed the
// same library and install it through the same adapter, so the embedded FS and
// the adapter live here, shared by both.

package main

import (
	"embed"
	"fmt"
	"sort"
)

//go:embed goclib/*
var libFS embed.FS

// embeddedLib adapts the embedded library to common's Source interface.
//
// The adaptation is one method. common asks for a file by name and for the list
// of library files; embed.FS answers the first directly, and the second through
// a directory listing that this filters and sorts. The sort matters for the same
// reason it does on disk: the library's sources are compiled in list order, so
// that order decides when its definitions reach the symbol table, and an
// unstable one would make the output depend on the toolchain's mood.
type embeddedLib struct{}

func (embeddedLib) ReadFile(name string) ([]byte, error) {
	return libFS.ReadFile(name)
}

func (embeddedLib) ReadDir(name string) ([]string, error) {
	ents, err := libFS.ReadDir(name)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if len(n) > 2 && (n[len(n)-2:] == ".c" || n[len(n)-2:] == ".h") {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no library sources under %s in the embedded FS", name)
	}
	return out, nil
}
