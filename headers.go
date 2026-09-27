package main

import "embed"

// System headers goc ships with the compiler. They live as real header files
// under goclib/ (stddef.h, stdarg.h, stdio.h, stdlib.h, string.h) and are
// embedded here so `#include <name.h>` can be injected with no disk lookup and
// no system headers -- keeping the toolchain self-contained.
//
// The declarations use the full subset goc's front end parses today:
// const/volatile qualifiers are accepted and ignored, typedefs (size_t,
// ptrdiff_t) are real, and variadic prototypes (printf, sprintf) validate any
// number of trailing arguments. goclib/goclib.h is also embedded here; it is
// the internal umbrella header that includes the standard headers and the
// __goclib_* platform primitives for goclib/goclib.c.
//
//go:embed goclib/*.h
var goclibHeaders embed.FS
