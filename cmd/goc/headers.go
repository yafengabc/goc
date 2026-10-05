package main

// System headers goc ships with the compiler. They live as real header files
// under goclib/ (stddef.h, stdarg.h, stdio.h, stdlib.h, string.h) and are
// read from there at run time so `#include <name.h>` resolves with no system
// headers and no install step -- goc never consults the host's /usr/include.
//
// The declarations use the full subset goc's front end parses today:
// const/volatile qualifiers are accepted and ignored, typedefs (size_t,
// ptrdiff_t) are real, and variadic prototypes (printf, sprintf) validate any
// number of trailing arguments. goclib/goclib.h is also embedded here; it is
// the internal umbrella header that includes the standard headers and the
// __goclib_* platform primitives for goclib/goclib.c.
//
// Read from disk, like goclibCFS; see libfs.go.
var goclibHeaders = libFS()
