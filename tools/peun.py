#!/usr/bin/env python
"""peun.py -- run a Windows x86-64 PE (as produced by goa) under Unicorn.

Mirror of ucrun.py for the Windows side: instead of emulating Linux
syscalls we hook the import thunks (`call [rip+disp32]`) and emulate the
small set of Win32 functions the asm examples use (GetStdHandle /
WriteFile / ExitProcess).  TCG gives real x86-64 instruction semantics
(SSE2, flags, addressing) independent of the host CPU/OS, so a Windows
example whose output depends on genuine instruction behaviour can be
checked locally against its golden.

Usage:
    python peun.py <windows-pe>
Exit code: the program's exit code (or 1 on a trap).

Scope, deliberately: this is a *diagnoser*, not a regression runner. The
Windows leg of run_tests.sh execs the real .exe on the real OS, which is
strictly stronger than anything modelled here -- so nothing runs this in
CI. What it adds over exec'ing is the fault detail: a real access
violation gives you 0xC0000005 and nothing else, while this reports the
faulting address, the size and the rip that asked for it (that is how the
phase1 null-dereference was pinned down on the Linux side).

Only three imports are modelled: GetStdHandle, WriteFile, ExitProcess.
That covers goa's own examples and nothing that allocates -- goclib's
Windows side calls GetProcessHeap/HeapAlloc/HeapFree, so any goc example
using malloc stops here with a named message. Emulating a heap would be
modelling our own model of kernel32, which is exactly the trap this
project already fell into with its hand-written ELF interpreter.
"""

import struct
import sys

from unicorn import Uc, UC_ARCH_X86, UC_MODE_64, UC_PROT_READ, UC_PROT_WRITE, UC_PROT_EXEC
from unicorn import UC_HOOK_CODE, UC_HOOK_MEM_INVALID
from unicorn.x86_const import (
    UC_X86_REG_RAX, UC_X86_REG_RCX, UC_X86_REG_RDX, UC_X86_REG_R8, UC_X86_REG_R9,
    UC_X86_REG_RSP, UC_X86_REG_RIP,
)

STACK_TOP = 0x7ffc00000000
STACK_SIZE = 0x100000
PAGE = 0x1000
IMAGE_BASE = 0x140000000


def align_up(x, a):
    return (x + a - 1) & ~(a - 1)


class Trap(Exception):
    def __init__(self, code):
        self.code = code


class PE:
    """Parse a minimal PE32+ (the kind goa writes) into loadable sections."""

    def __init__(self, data):
        pe = struct.unpack_from("<I", data, 0x3C)[0]
        if data[pe:pe + 4] != b"PE\x00\x00":
            raise ValueError("not a PE")
        self.data = data
        self.pe = pe
        num_sec = struct.unpack_from("<H", data, pe + 6)[0]
        size_opt = struct.unpack_from("<H", data, pe + 20)[0]
        opt = pe + 24
        if struct.unpack_from("<H", data, opt)[0] != 0x20B:
            raise ValueError("not PE32+")
        self.opt = opt
        self.image_base = struct.unpack_from("<Q", data, opt + 24)[0]
        self.entry = self.image_base + struct.unpack_from("<I", data, opt + 16)[0]
        sec_off = opt + size_opt
        self.sections = []
        for i in range(num_sec):
            s = sec_off + i * 40
            name = data[s:s + 8].rstrip(b"\x00").decode()
            vsz, va, rawsz, rawptr = struct.unpack_from("<IIII", data, s + 8)
            chars = struct.unpack_from("<I", data, s + 36)[0]
            self.sections.append((name, va, vsz, rawptr, rawsz, chars))

    def rva2off(self, rva):
        for _, va, _vsz, rawptr, rawsz, _ in self.sections:
            if va <= rva < va + rawsz:
                return rawptr + (rva - va)
        return None

    def imports(self):
        """Return {IAT_va: function_name} by walking the import directory."""
        imp_rva = struct.unpack_from("<I", self.data, self.opt + 120)[0]
        io = self.rva2off(imp_rva)
        if io is None:
            return {}
        iat = {}
        j = 0
        while True:
            e = io + j * 20
            oft = struct.unpack_from("<I", self.data, e)[0]
            name_rva = struct.unpack_from("<I", self.data, e + 12)[0]
            ft = struct.unpack_from("<I", self.data, e + 16)[0]
            if not oft and not name_rva and not ft:
                break
            no = self.rva2off(oft)
            for k in range(64):
                thunk = struct.unpack_from("<Q", self.data, no + k * 8)[0]
                if not thunk:
                    break
                if thunk & 0x8000000000000000:
                    continue
                noff = self.rva2off(thunk & 0x7FFFFFFF)
                fname = self.data[noff + 2:].split(b"\x00")[0].decode()
                iat[self.image_base + ft + k * 8] = fname
            j += 1
        return iat


class UcPE:
    def __init__(self, path):
        pe = PE(open(path, "rb").read())
        self.mu = Uc(UC_ARCH_X86, UC_MODE_64)
        self.out = sys.stdout.buffer
        self.hstdout = 0x1234  # pseudo handle for GetStdHandle

        for name, va, vsz, rawptr, rawsz, chars in pe.sections:
            base = pe.image_base + va
            size = align_up(va + max(vsz, rawsz), PAGE) - va
            prot = UC_PROT_READ
            if chars & 0x80000000:
                prot |= UC_PROT_WRITE
            if chars & 0x20000000:
                prot |= UC_PROT_EXEC
            self.mu.mem_map(base, size, prot)
            if rawsz:
                self.mu.mem_write(base, pe.data[rawptr:rawptr + rawsz])

        stack_base = STACK_TOP - STACK_SIZE
        self.mu.mem_map(stack_base, STACK_SIZE, UC_PROT_READ | UC_PROT_WRITE)
        self.mu.reg_write(UC_X86_REG_RSP, STACK_TOP)

        self.iat = pe.imports()
        self.entry = pe.entry
        self.image_base = pe.image_base
        self.mu.hook_add(UC_HOOK_CODE, self.hook_code)
        self.mu.hook_add(UC_HOOK_MEM_INVALID, self.hook_invalid)

    def hook_invalid(self, mu, access, address, size, value, ud):
        # Say where and from where. A bare "invalid memory" is the whole
        # reason this tool exists over just exec'ing the exe: a real fault
        # gives you 0xC0000005 and nothing else, while here you get the
        # faulting address, the size and the rip that asked for it.
        rip = mu.reg_read(UC_X86_REG_RIP)
        print(
            "peun: unmapped %s at 0x%x (size %d) rip=0x%x\n"
            "      image_base=0x%x, stack [0x%x,0x%x)"
            % ("read" if access in (16, 17, 18) else "access", address, size, rip,
               self.image_base, STACK_TOP - STACK_SIZE, STACK_TOP),
            file=sys.stderr,
        )
        raise Trap(1)

    def hook_code(self, mu, address, size, ud):
        if size < 6:
            return
        code = mu.mem_read(address, 6)
        if code[0] != 0xFF or code[1] != 0x15:  # call [rip+disp32]
            return
        disp = struct.unpack("<i", code[2:6])[0]
        target = address + 6 + disp
        fname = self.iat.get(target)
        if fname is None:
            # Not an import we recognised. Saying the address, rather than
            # exiting 1 silently, is the difference between "the program is
            # broken" and "this tool does not model that call".
            print(
                "peun: indirect call to 0x%x (rip=0x%x) is not in the IAT"
                % (target, address),
                file=sys.stderr,
            )
            raise Trap(1)
        # Simulate the call: push the return address, then emulate the body.
        rsp = mu.reg_read(UC_X86_REG_RSP) - 8
        mu.mem_write(rsp, struct.pack("<Q", address + 6))
        mu.reg_write(UC_X86_REG_RSP, rsp)
        if fname == "GetStdHandle":
            mu.reg_write(UC_X86_REG_RAX, self.hstdout)
        elif fname == "WriteFile":
            buf = mu.reg_read(UC_X86_REG_RDX)
            cnt = mu.reg_read(UC_X86_REG_R8)
            written = mu.reg_read(UC_X86_REG_R9)
            if cnt:
                self.out.write(mu.mem_read(buf, cnt))
                self.out.flush()
            mu.mem_write(written, struct.pack("<Q", cnt))
            mu.reg_write(UC_X86_REG_RAX, 1)  # TRUE
        elif fname == "ExitProcess":
            raise Trap(mu.reg_read(UC_X86_REG_RCX))
        else:
            # An import we parsed but do not emulate -- e.g. HeapAlloc, which
            # goclib's Windows side needs. Name it: exit 1 on its own looks
            # like the program failed.
            print(
                "peun: no emulation for imported %s() (rip=0x%x)" % (fname, address),
                file=sys.stderr,
            )
            raise Trap(1)
        mu.reg_write(UC_X86_REG_RIP, address + 6)

    def run(self):
        self.mu.emu_start(self.entry, 0)
        return 0


def main():
    if len(sys.argv) < 2:
        print("usage: peun.py <windows-pe>", file=sys.stderr)
        return 2
    try:
        rc = UcPE(sys.argv[1]).run()
    except Trap as t:
        rc = t.code
    except Exception as e:
        print(f"peun: {e}", file=sys.stderr)
        rc = 1
    sys.exit(rc & 0xFF)


if __name__ == "__main__":
    main()
