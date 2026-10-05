//go:build !windows

package goa

import "fmt"

// The LLVM back end on platforms it does not exist for.
//
// llvm.go opens libLLVM through syscall.LazyDLL, which is Windows-only. Rather
// than hide that behind a build tag on the whole binding -- which would leave
// every caller unable to compile at all, including the ones that only mention
// the type -- the names live here and report the fact.
//
// So a Linux build of a compiler that has an LLVM back end produces a working
// binary: the back end refuses the programs that need it, with a message that
// says which platform it is, and everything else compiles exactly as before.
// That is the same behaviour as a Windows build with no libLLVM installed,
// which is the situation a user is most likely to hit.

// errUnsupportedPlatform is what every entry point here returns. It is not
// ErrNoLLVM: that one says the library is missing, and on this platform no
// amount of installing it would help.
var errUnsupportedPlatform = fmt.Errorf("goa: the LLVM back end is implemented for Windows only")

// LLVM is the handle the compiler uses to reach the LLVM back end. It is
// declared here as well as in llvm.go so that code naming the type compiles
// everywhere; it holds nothing, because there is nothing to hold.
type LLVM struct{}

// OpenLLVM reports that the back end is unavailable on this platform.
func OpenLLVM() (*LLVM, error) { return nil, errUnsupportedPlatform }

// LLVMAvailable reports whether the LLVM back end can run here. It cannot.
func LLVMAvailable() bool { return false }

// CompileToObject is unavailable here. The receiver is a nil *LLVM from
// OpenLLVM, which failed, so this is only reachable if a caller constructed one
// itself -- hence an error naming the platform rather than a nil dereference.
func (l *LLVM) CompileToObject(ir []byte, outPath string, opt LLVMCodeGenOptLevel, passes string) error {
	return errUnsupportedPlatform
}

// CompileToAssembly is unavailable here. See CompileToObject.
func (l *LLVM) CompileToAssembly(ir []byte, outPath string, opt LLVMCodeGenOptLevel, passes string) error {
	return errUnsupportedPlatform
}

// Version reports zeroes, since no library is ever opened here.
func (l *LLVM) Version() (int, int, int) { return 0, 0, 0 }
