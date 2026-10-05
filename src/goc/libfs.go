package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
//
// The C runtime on disk.
//
// goc used to carry the whole C library inside the executable: //go:embed
// baked goclib/*.c and goclib/*.h into the binary, and the loader read them
// back out of an embed.FS. That made goc a single self-contained file -- carry
// the exe and you can compile C -- and it is the reason goclib/ and the
// compiler could never be separated into two projects.
//
// The split into goc and gocl needs the library to be an input both compilers
// read the same way, so it is loaded from disk instead. The cost is stated
// plainly because it is real: **goc.exe is no longer self-contained.** Copied
// away from its goclib/ directory it fails on the first program that calls
// anything, with a diagnostic naming the missing library -- not a silent wrong
// answer, but not a working compiler either. goclib/ therefore has to travel
// with the binary, and any packaging that ships a compiler has to ship it too.
//
// The lookup order is deliberate. GOCLIB_PATH wins so a distribution can point
// at a shared library copy, and so the test suite can build a second compiler
// against a different tree. After that the search runs from the directory
// holding the running executable, and then from the working directory -- a
// developer running `go run ./goc` or `go test` gets a different origin than
// one running the installed binary, and both have to work.
//
// Each origin is searched by probe, which alternates descending and climbing.
// See probe for why one direction alone is not enough.
//
// Everything below the found root is read as a plain directory: the calls are
// on the same "goclib/<name>" spelling the embed version used, so the call
// sites did not change.

// goclibRootEnv names the environment variable that overrides the search.
const goclibRootEnv = "GOCLIB_PATH"

// sourceFS is the subset of embed.FS the library loader uses: ReadFile and
// ReadDir, with a directory entry carrying just a name and an IsDir flag.
// Declaring it here rather than against *os.DirFS is what keeps the call sites
// unchanged -- they were written for embed.FS and they still work.
//
// Errors are the ones os.ReadFile and os.ReadDir return, so the existing
// "an unavailable <header> is skipped rather than fatal" logic in the
// preprocessor keeps working untouched: it tests err == nil, and a missing
// file on disk is indistinguishable from a missing file in the embed.
type sourceFS struct {
	// root is the directory that holds goclib/. A path handed to ReadFile is
	// joined onto it, so callers keep passing "goclib/<name>".
	root string
	// err records why the library could not be located. The loader checks it
	// once and reports it, rather than each ReadFile failing on its own and
	// the first caller to notice producing a confusing message.
	err error
}

// ReadFile reads one library file. The name is a slash-separated path
// relative to root, matching embed.FS conventions.
func (f sourceFS) ReadFile(name string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return os.ReadFile(filepath.Join(f.root, filepath.FromSlash(name)))
}

// dirEntry mirrors fs.DirEntry for the two fields the loader reads.
type dirEntry struct {
	name  string
	isDir bool
}

func (e dirEntry) Name() string { return e.name }
func (e dirEntry) IsDir() bool  { return e.isDir }

// ReadDir lists one directory, sorted by name. embed.FS sorts; matching that
// keeps the library's compilation order -- and therefore the order its
// definitions reach the symbol table -- independent of the file system.
func (f sourceFS) ReadDir(name string) ([]dirEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	ents, err := os.ReadDir(filepath.Join(f.root, filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	out := make([]dirEntry, 0, len(ents))
	for _, e := range ents {
		out = append(out, dirEntry{name: e.Name(), isDir: e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// probe searches outward from one origin, alternating between descending and
// climbing so the two directions interleave.
//
// The alternation is not decoration. goclib/ is at src/goclib while the
// binary is at bin/goc.exe: the library is neither an ancestor of the binary
// (climb from bin/ goes to the repo root, the parent, the volume, and never
// through src) nor a child of it (descend from bin/ reaches the goc-out*
// build directories). Only going up one level and then back down finds it, and
// a search that tries all of one direction before starting the other cannot
// express that.
//
// Each level is tried as descend-then-climb, so a nearer hit always wins over
// a farther one regardless of direction.
func probe(dir string, consider func(string) (string, bool)) (string, bool) {
	for depth := 0; depth < 4; depth++ {
		if root, ok := descend(dir, consider); ok {
			return root, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false // reached the volume root
		}
		dir = parent
	}
	return "", false
}

// descend tries dir, then its children. One level is enough here because
// probe alternates: a deeper library is found by climbing to its parent first
// and descending from there.
func descend(dir string, consider func(string) (string, bool)) (string, bool) {
	if root, ok := consider(dir); ok {
		return root, true
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	// Sorted so the choice is deterministic when two subdirectories both
	// carry a library, e.g. a stale copy beside a current one.
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if root, ok := consider(filepath.Join(dir, n)); ok {
			return root, true
		}
	}
	return "", false
}

// findGoclibRoot locates the directory that holds goclib/.
//
// The candidates are tried in order and the first one containing goclib/goclib.h
// wins; a directory that exists but has no library in it is skipped rather than
// accepted, so a stray GOCLIB_PATH does not silently produce an empty runtime.
func findGoclibRoot() (string, error) {
	var tried []string
	consider := func(dir string) (string, bool) {
		if dir == "" {
			return "", false
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			abs = dir
		}
		abs = filepath.Clean(abs)
		tried = append(tried, abs)
		// goclib.h is the umbrella header every include path goes through, so
		// its presence is the one signal that this is a real library tree and
		// not a directory that merely shares the name.
		if st, err := os.Stat(filepath.Join(abs, "goclib", "goclib.h")); err == nil && !st.IsDir() {
			return abs, true
		}
		return "", false
	}

	if env := os.Getenv(goclibRootEnv); env != "" {
		if root, ok := consider(env); ok {
			return root, nil
		}
	}
	// Walk up from the executable, then from the working directory.
	//
	// Both are loops over filepath.Dir rather than a fixed number of ".."
	// hops, because the library has moved more than once (src/goclib, then the
	// repository root, now src/ again) and each move broke a hardcoded depth.
	// Two details make this more than a loop:
	//
	//   - filepath.Join(dir, "..") is NOT the way up. Join cleans the ".." away
	//     and returns the parent, so a loop written with it never climbs at all.
	//     An earlier version asked for the parent of the parent and silently
	//     got the parent, which is why the library went missing from src/goc.
	//   - an installed layout has goclib/ beside the exe, while the repository
	//     has it under src/. Climbing covers both without a special case.
	//
	// The executable is tried before the working directory so a copy of the
	// toolchain run from an unrelated project still finds its own library.
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		if root, ok := probe(filepath.Dir(exe), consider); ok {
			return root, nil
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if root, ok := probe(wd, consider); ok {
			return root, nil
		}
	}
	return "", fmt.Errorf("cannot find the goclib C library (looked in: %s); "+
		"set %s to the directory that contains goclib/", strings.Join(tried, ", "),
		goclibRootEnv)
}

// libFS opens the library, or returns a filesystem whose every call fails with
// the reason. The loader checks the error once, so a missing library is
// reported once with an actionable message instead of as a read error on an
// unrelated header deep inside the front end.
func libFS() sourceFS {
	root, err := findGoclibRoot()
	if err != nil {
		return sourceFS{err: err}
	}
	return sourceFS{root: root}
}
