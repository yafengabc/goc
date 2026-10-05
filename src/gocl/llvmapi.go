package gocl

import "errors"

// The LLVM back end's shared vocabulary: the error callers see, and the
// optimisation levels they can ask for.
//
// These live apart from the binding itself because the binding is Windows-only
// (it opens libLLVM through syscall.LazyDLL) while its callers are not. A
// compiler that can target Linux has to be able to *name* the error and the
// level on every platform, even where the work behind them is unavailable.

// ErrNoLLVM reports that no usable libLLVM shared library could be loaded. It is
// an ordinary error, not a crash: the caller can fall back or explain.
var ErrNoLLVM = errors.New("goa: libLLVM shared library not available")

// LLVMCodeGenOptLevel selects how hard the backend works. The values match
// llvm::CodeGenOptLevel.
type LLVMCodeGenOptLevel int32

const (
	LLVMOptNone       LLVMCodeGenOptLevel = 0
	LLVMOptLess       LLVMCodeGenOptLevel = 1
	LLVMOptDefault    LLVMCodeGenOptLevel = 2
	LLVMOptAggressive LLVMCodeGenOptLevel = 3
)
