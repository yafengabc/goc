//go:build windows

package gocl

// Windows half of the libLLVM loader: syscall.LazyDLL / LazyProc. Kept on the
// platform that has been running this backend since the beginning; the shared
// file llvm.go describes the proc seam this fills.

import (
	"fmt"
	"syscall"
)

// libHandle is the loaded DLL, as a concrete type implementing the libHandle
// interface from llvm.go. syscall.LazyDLL resolves symbols lazily, but bind()
// calls Find() on every entry point up front, so an unresolvable symbol is a
// load-time error naming it rather than a late panic.
type dllHandle struct {
	dll *syscall.LazyDLL
}

// openLibrary opens the DLL at path. The handle must stay alive as long as any
// proc resolved from it is called; llvmAPI keeps it on the lib field.
func openLibrary(path string) (libHandle, error) {
	dll := syscall.NewLazyDLL(path)
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("goa: loading %s: %w", path, err)
	}
	return dllHandle{dll: dll}, nil
}

// proc resolves one C function and wraps its calling convention.
//
// syscall.LazyProc.Call goes through the runtime's C-ABI trampoline, which is
// both lazier (a missing export is not fatal until it is used) and vet-clean --
// calling a C function through a bare syscall.SyscallN would mean passing
// uintptr arguments, and reading a returned uintptr back as memory needs a
// uintptr-to-pointer conversion that the unsafe-pointer check rightly refuses.
// The third return value (an error) is discarded: no call site here reads it.
func (h dllHandle) proc(name string) (proc, error) {
	p := h.dll.NewProc(name)
	if err := p.Find(); err != nil {
		return nil, err
	}
	return func(args ...uintptr) (uintptr, uintptr) {
		r1, r2, _ := p.Call(args...)
		return r1, r2
	}, nil
}

// libNames are the DLL names searched next to the running executable, in order.
func libNames() []string {
	return []string{"libLLVM.dll", "LLVM.dll", "libLLVM-9.dll"}
}

// libDirs are extra directories searched for the library. On Windows the DLL
// travels with the binary, so findLLVM's executable directory (added by the
// shared code) is enough; there are no system-wide libLLVM.dll directories to
// also look in.
func libDirs() []string { return nil }
