// What the two compilers share: the C preprocessor, and the compiled form of
// the C library a back end links against.
//
// It exists as its own module because two compilers need it -- src/goc (the
// native x86-64 generator) and src/gocl (the LLVM one) -- and neither should
// have to depend on the other. Both depend on this instead.
//
// The name is "common" rather than anything library-shaped on purpose: this is
// not a C library. It holds the preprocessor (which is front-end work) and the
// result of compiling goclib (which is a back end's input), and those are
// different kinds of thing that happen to share a need.
//
// What lives here is deliberately narrow: locate goclib/, preprocess and parse
// it, and hand back the functions, prototypes and globals it defines. Nothing
// here generates code, so neither back end can influence the other through it.
module goc/common

go 1.21

require goc/frontend v0.0.0

replace goc/frontend => ../frontend
