//go:build linux

package gocl

// Linux half of the libLLVM loader: purego's Dlopen/Dlsym, no cgo and no
// import-time dependency on libLLVM (the library is opened at run time, exactly
// like the Windows port). The shared file llvm.go describes the proc seam this
// fills.

import (
	"fmt"

	"github.com/ebitengine/purego"
)

// libHandle is the handle returned by purego.Dlopen, as a concrete type
// implementing the libHandle interface from llvm.go. It must stay alive as long
// as any proc resolved from it is called; llvmAPI keeps it on the lib field.
type soHandle uintptr

// openLibrary opens the shared object at path. RTLD_NOW resolves every symbol
// at load time, so a broken library fails here rather than at the first call.
func openLibrary(path string) (libHandle, error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW)
	if err != nil {
		return nil, fmt.Errorf("goa: loading %s: %w", path, err)
	}
	return soHandle(h), nil
}

// proc resolves one C function and wraps its calling convention.
//
// purego.SyscallN uses the SysV ABI with no cgo. Every entry point the compiler
// reaches is an integer/pointer prototype (the LLVM C API), so a flat
// SyscallN-style call is correct: there are no float register arguments to
// misplace and no structs passed by value. The third return value (errno) is
// discarded: no call site here reads it.
func (h soHandle) proc(name string) (proc, error) {
	p, err := purego.Dlsym(uintptr(h), name)
	if err != nil {
		return nil, err
	}
	return func(args ...uintptr) (uintptr, uintptr) {
		r1, r2, _ := purego.SyscallN(p, args...)
		return r1, r2
	}, nil
}

// libNames are the shared object names searched in the library directories, in
// order. Arch ships libLLVM.so (the development symlink, which this binding
// wants) from the `llvm` package; the .so.N sonames are what libLLVM-libs
// provides without a symlink.
func libNames() []string {
	return []string{"libLLVM.so", "libLLVM-19.so", "libLLVM-20.so", "libLLVM-21.so"}
}

// libDirs are the system library directories searched after the executable's
// directory, so a distro-installed libLLVM.so is found without setting
// GOC_LLVM_DLL.
func libDirs() []string {
	return []string{"/usr/lib", "/usr/local/lib", "/lib", "/lib64"}
}
