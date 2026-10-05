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
// against a different tree; then the directory holding the running executable,
// which covers both the repo layout and an installed layout with goclib/ beside
// the exe. Only then the source tree, which is what a developer running
// `go run ./cmd/goc` gets.
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
	// The running executable's directory: covers the repo layout (goc and
	// goclib are siblings under bin/ and the source tree) and an installed
	// layout with goclib/ shipped next to the exe.
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		if root, ok := consider(dir); ok {
			return root, nil
		}
		if root, ok := consider(filepath.Join(dir, "..")); ok {
			return root, nil
		}
	}
	// The source tree. goclib/ sits at the repository root, but the compiler
	// runs from all over it -- the root, cmd/goc, goa/, and (under `go test`)
	// a temporary directory whose path leads nowhere near the checkout. So walk
	// up from the working directory instead of testing a fixed number of
	// parent hops: two levels is enough for cmd/goc but not for cmd/goc/a/b.
	//
	// filepath.Join is not usable for this. Join(wd, "..") *cleans* the ".."
	// away, so it yields the parent -- correct for one hop, and the reason an
	// earlier version of this function missed the repository root from
	// cmd/goc: it asked for the parent of the parent and got the parent. Hence
	// Dir() in the loop rather than a Join per level.
	if wd, err := os.Getwd(); err == nil {
		for dir, i := wd, 0; i < 6; i++ {
			if root, ok := consider(dir); ok {
				return root, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break // reached the volume root
			}
			dir = parent
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
