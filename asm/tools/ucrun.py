#!/usr/bin/env python
"""ucrun.py -- run a Linux x86-64 static ELF under the Unicorn engine.

Unicorn is the QEMU TCG engine exposed as a library, so instruction semantics
(SSE2, flags, addressing) match real hardware much more closely than the
elfcheck interpreter.  This is the local stand-in for "run it on a real Linux
kernel" on Windows: same ELF layout and brk rules as the kernel, but CPU
execution via TCG.

Usage:
    python ucrun.py <linux-elf> [args...]
Exit code: the program's exit code (or 128+signal-ish for traps).
"""

import os
import struct
import sys

from unicorn import Uc, UC_ARCH_X86, UC_MODE_64, UC_PROT_READ, UC_PROT_WRITE, UC_PROT_EXEC
from unicorn import UC_HOOK_CODE, UC_HOOK_MEM_INVALID
from unicorn.x86_const import (
    UC_X86_REG_RAX, UC_X86_REG_RBX, UC_X86_REG_RCX, UC_X86_REG_RDX,
    UC_X86_REG_RSI, UC_X86_REG_RDI, UC_X86_REG_RBP, UC_X86_REG_RSP,
    UC_X86_REG_RIP, UC_X86_REG_R8, UC_X86_REG_R9,
)

STACK_TOP = 0x7ffc00000000
STACK_SIZE = 0x100000
PAGE = 0x1000


def align_up(x, a):
    return (x + a - 1) & ~(a - 1)


class Trap(Exception):
    def __init__(self, code):
        self.code = code


class Loader:
    """Parse an ELF64 and give us the segments to map."""

    def __init__(self, data):
        if data[:4] != b"\x7fELF" or data[4] != 2 or data[5] != 1:
            raise ValueError("not a 64-bit little-endian ELF")
        (self.entry,) = struct.unpack_from("<Q", data, 0x18)
        (phoff,) = struct.unpack_from("<Q", data, 0x20)
        (phentsize,) = struct.unpack_from("<H", data, 0x36)
        (phnum,) = struct.unpack_from("<H", data, 0x38)
        self.segs = []
        for i in range(phnum):
            off = phoff + i * phentsize
            (p_type,) = struct.unpack_from("<I", data, off)
            (p_flags,) = struct.unpack_from("<I", data, off + 4)
            (p_offset,) = struct.unpack_from("<Q", data, off + 8)
            (p_vaddr,) = struct.unpack_from("<Q", data, off + 16)
            (p_filesz,) = struct.unpack_from("<Q", data, off + 32)
            (p_memsz,) = struct.unpack_from("<Q", data, off + 40)
            if p_type == 1:  # PT_LOAD
                self.segs.append((p_offset, p_vaddr, p_filesz, p_memsz, p_flags))

    def brk_start(self):
        """Kernel rule: initial brk == end of the highest LOAD mapping, page-aligned."""
        end = 0
        for _, vaddr, _, memsz, _ in self.segs:
            end = max(end, vaddr + memsz)
        return align_up(end, PAGE)


class UcLinux:
    def __init__(self, path, argv):
        data = open(path, "rb").read()
        ldr = Loader(data)
        self.mu = Uc(UC_ARCH_X86, UC_MODE_64)
        self.out = sys.stdout.buffer
        self.brk_cur = ldr.brk_start()
        self.heap_top = self.brk_cur

        # Map load segments.
        for p_offset, p_vaddr, p_filesz, p_memsz, p_flags in ldr.segs:
            base = align_up(p_vaddr, PAGE)  # start of the page containing p_vaddr
            seg_start = p_vaddr
            seg_end = align_up(p_vaddr + p_memsz, PAGE)
            if base < seg_start:
                # p_vaddr may not be page aligned; map one page earlier.
                base = seg_start & ~(PAGE - 1)
            prot = 0
            if p_flags & 4:
                prot |= UC_PROT_READ
            if p_flags & 2:
                prot |= UC_PROT_WRITE
            if p_flags & 1:
                prot |= UC_PROT_EXEC
            self.mu.mem_map(base, seg_end - base, prot)
            if p_filesz:
                self.mu.mem_write(p_vaddr, data[p_offset:p_offset + p_filesz])

        # Map the stack.
        stack_base = STACK_TOP - STACK_SIZE
        self.mu.mem_map(stack_base, STACK_SIZE, UC_PROT_READ | UC_PROT_WRITE)

        # Build a minimal argv/envp block on the stack (SysV style).
        toks = [b"ucrun"] + [a.encode() for a in argv]
        words = []
        rsp = STACK_TOP
        for t in reversed(toks):
            rsp -= len(t) + 1
            self.mu.mem_write(rsp, t + b"\x00")
            words.append(rsp)
        words.reverse()
        rsp -= 8 * (len(words) + 2)
        rsp &= ~0xF
        p = rsp
        self.mu.mem_write(p, struct.pack("<Q", len(words)))
        p += 8
        for w in words:
            self.mu.mem_write(p, struct.pack("<Q", w))
            p += 8
        self.mu.mem_write(p, struct.pack("<Q", 0))  # NULL argv terminator
        p += 8
        self.mu.mem_write(p, struct.pack("<Q", 0))  # empty envp

        self.mu.reg_write(UC_X86_REG_RSP, rsp)
        self.mu.reg_write(UC_X86_REG_RIP, ldr.entry)
        self.entry = ldr.entry
        self.mu.hook_add(UC_HOOK_CODE, self.hook_code)
        self.mu.hook_add(UC_HOOK_MEM_INVALID, self.hook_invalid)

    def grow_heap(self, addr):
        # Kernel semantics: brk() moves the break pointer (may be unaligned),
        # but memory is mapped page-granularly.  Unicorn's mem_map requires a
        # page-aligned address and a page-multiple size, so track how far the
        # mapping actually extends (heap_top) and only map fresh pages.
        if addr <= self.brk_cur:
            return self.brk_cur
        end = align_up(addr, PAGE)
        if end > self.heap_top:
            need = end - self.heap_top
            self.mu.mem_map(self.heap_top, need, UC_PROT_READ | UC_PROT_WRITE)
            self.heap_top = end
        self.brk_cur = addr
        return self.brk_cur

    def hook_invalid(self, mu, access, address, size, value, ud):
        rip = mu.reg_read(UC_X86_REG_RIP)
        raise Trap(1)  # segfault -> exit 1, printed by caller

    def hook_code(self, mu, address, size, ud):
        if size < 2:
            return
        code = mu.mem_read(address, 2)
        if code != b"\x0f\x05":  # syscall
            return
        n = mu.reg_read(UC_X86_REG_RAX)
        if n == 60:  # exit
            raise Trap(mu.reg_read(UC_X86_REG_RDI))
        elif n == 1:  # write
            buf = mu.reg_read(UC_X86_REG_RSI)
            cnt = mu.reg_read(UC_X86_REG_RDX)
            if cnt:
                self.out.write(mu.mem_read(buf, cnt))
            self.out.flush()
            mu.reg_write(UC_X86_REG_RAX, cnt)
        elif n == 12:  # brk
            mu.reg_write(UC_X86_REG_RAX, self.grow_heap(mu.reg_read(UC_X86_REG_RDI)))
        else:
            raise Trap(1)  # unhandled syscall -> treat as fatal

    def run(self):
        self.mu.emu_start(self.entry, 0)
        return 0


def main():
    if len(sys.argv) < 2:
        print("usage: ucrun.py <linux-elf> [args...]", file=sys.stderr)
        return 2
    elf = sys.argv[1]
    try:
        rc = UcLinux(elf, sys.argv[2:]).run()
    except Trap as t:
        rc = t.code
    except Exception as e:
        print(f"ucrun: {e}", file=sys.stderr)
        rc = 1
    sys.exit(rc & 0xFF)


if __name__ == "__main__":
    main()
