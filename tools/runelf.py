#!/usr/bin/env python3
"""Run one of gocl's static Linux ELFs under the Unicorn CPU emulator.

CI cannot execute a Linux binary: it has no Linux kernel and no unpacked root
filesystem. This script supplies the smallest thing that makes one run anyway --
a CPU, the two syscalls goc's C library actually issues, and a stack. That is
enough to compare a program's output across all six back ends on any machine,
which is how the floating-point and VLA suites are checked without a farm of
real boards.

  python tools/runelf.py prog.aarch64        # prints the program's stdout
  GOC_EMU_DEBUG=1 python tools/runelf.py prog.riscv32   # + a syscall trace

The exit code of the emulated program is reported on stderr as
`[exit code = N]`; the script's own status is 0 when the program reached an
exit syscall, 2 when emulation faulted and 3 when it never terminated.

Architecture is taken from the ELF's e_machine, so the same command serves
every target:

  x86_64   `syscall`   UC_HOOK_INSN(UC_X86_INS_SYSCALL); rax = number
  aarch64  `svc #0`    UC_HOOK_INTR; x8 = number
  arm      `svc #0`    UC_HOOK_INTR; r7 = number  (CORTEX_A15 => VFPv3)
  armel    `svc #0`    UC_HOOK_INTR; r7 = number
  riscv64  `ecall`     UC_HOOK_INTR; a7 = number
  riscv32  `ecall`     UC_HOOK_INTR; a7 = number

Requires the unicorn package (pip install unicorn) and Python 3. Without
unicorn the script exits with a message instead of a traceback, so it can be
probed by a test that wants to skip rather than fail.
"""
import os
import struct
import sys

DEBUG = "GOC_EMU_DEBUG" in os.environ

try:
    from unicorn import (
        Uc, UC_ARCH_X86, UC_ARCH_ARM, UC_ARCH_ARM64, UC_ARCH_RISCV,
        UC_MODE_64, UC_MODE_ARM, UC_MODE_RISCV32, UC_MODE_RISCV64,
        UC_HOOK_INTR, UC_HOOK_INSN,
    )
    from unicorn.x86_const import (
        UC_X86_REG_RAX, UC_X86_REG_RDI, UC_X86_REG_RSI, UC_X86_REG_RDX,
        UC_X86_REG_RSP, UC_X86_REG_RIP, UC_X86_INS_SYSCALL,
    )
    from unicorn.arm64_const import (
        UC_ARM64_REG_X8, UC_ARM64_REG_X0, UC_ARM64_REG_X1, UC_ARM64_REG_X2,
        UC_ARM64_REG_SP, UC_ARM64_REG_PC,
    )
    from unicorn.arm_const import (
        UC_ARM_REG_R7, UC_ARM_REG_R0, UC_ARM_REG_R1, UC_ARM_REG_R2,
        UC_ARM_REG_SP, UC_ARM_REG_PC, UC_CPU_ARM_CORTEX_A15,
    )
    from unicorn.riscv_const import (
        UC_RISCV_REG_A7, UC_RISCV_REG_A0, UC_RISCV_REG_A1, UC_RISCV_REG_A2,
        UC_RISCV_REG_SP, UC_RISCV_REG_PC,
    )
except ImportError as e:
    sys.stderr.write(
        "runelf.py needs the unicorn CPU emulator: pip install unicorn\n"
        f"  ({e})\n"
    )
    raise SystemExit(4)

PAGE = 0x1000
STACK_TOP = 0x30010000
STACK_SIZE = 0x40000

# e_machine -> gocl arch name. arm and armel share 0x28 (ELF cannot tell a
# soft-float ARM from a hard-float one), as do the two RISC-V widths; the class
# byte breaks the RISC-V tie, and the ARM tie is resolved by which file was
# built -- the caller names it, since neither needs different handling here.
EM = {"x86_64": 0x3E, "aarch64": 0xB7, "arm": 0x28, "armel": 0x28,
      "riscv64": 0xF3, "riscv32": 0xF3}


def detect(data):
    if data[:4] != b"\x7fELF":
        raise SystemExit(f"not an ELF file: {data[:4]!r}")
    cls = data[4]
    machine = struct.unpack_from("<H", data, 18)[0]
    for name, m in EM.items():
        if m == machine and ((name in ("x86_64", "aarch64", "riscv64")) == (cls == 2)):
            return name, cls
    raise SystemExit(f"unknown ELF machine {machine:#x} class {cls}")


def load_segs(data, cls):
    """Return (entry vaddr, [(vaddr, file offset, filesz, memsz), ...])."""
    if cls == 2:
        e_entry, e_phoff = struct.unpack_from("<QB", data, 24)
        e_phentsize, e_phnum = struct.unpack_from("<HH", data, 54)
    else:
        e_entry, e_phoff = struct.unpack_from("<II", data, 24)
        e_phentsize, e_phnum = struct.unpack_from("<HH", data, 42)
    segs = []
    for i in range(e_phnum):
        off = e_phoff + i * e_phentsize
        if cls == 2:
            # ELF64 phdr: two words then six quads. Reading it as eight 32-bit
            # fields silently halves every size, which maps a 7-byte segment
            # and faults on the first instruction fetch.
            p_type, _pflags, p_offset, p_vaddr, _pp, p_filesz, p_memsz, _pal = \
                struct.unpack_from("<IIQQQQQQ", data, off)
        else:
            # ELF32 phdr: eight words, in a different order than ELF64 --
            # type, offset, vaddr, paddr, filesz, memsz, flags, align.
            p_type, p_offset, p_vaddr, _pp, p_filesz, p_memsz, _pflags, _pal = \
                struct.unpack_from("<8I", data, off)
        if p_type == 1:  # PT_LOAD
            segs.append((p_vaddr, p_offset, p_filesz, p_memsz))
    if DEBUG:
        for s in segs:
            sys.stderr.write("SEG vaddr=%#x off=%#x filesz=%#x memsz=%#x\n" % s)
    return e_entry, segs


def setup(arch):
    """Build the emulator: return (Uc, hook kind, optional instruction id)."""
    if arch == "x86_64":
        mu = Uc(UC_ARCH_X86, UC_MODE_64)
        # Unicorn has a dedicated hook for the x86 `syscall` instruction; it
        # fires *instead of* executing it and RIP then advances normally.
        # Neither alternative works:
        #   - HOOK_CODE + reg_write(RIP, addr+2): Unicorn ignores RIP writes in
        #     HOOK_CODE on x86, so the syscall is re-executed forever.
        #   - HOOK_CODE + patching the bytes to NOPs: the C library loops on
        #     the same syscall site, so every call after the first is lost.
        return mu, "insn", UC_X86_INS_SYSCALL
    if arch == "aarch64":
        # UC_MODE_ARM, not UC_MODE_64: for ARM64 that constant *is* the 64-bit
        # mode, and passing UC_MODE_64 raises "Invalid mode".
        mu = Uc(UC_ARCH_ARM64, UC_MODE_ARM)
        return mu, "intr", None
    if arch in ("arm", "armel"):
        mu = Uc(UC_ARCH_ARM, UC_MODE_ARM)
        mu.ctl_set_cpu_model(UC_CPU_ARM_CORTEX_A15)
        # Enable the VFP/NEON coprocessors (CPACR cp10/cp11 = full access).
        # The register is CP15 C1 C0 2, spelled UC_ARM_REG_C1_C0_2 here; there
        # is no UC_ARM_REG_CPACR, and a name that does not exist inside a
        # try/except silently left the coprocessor disabled. Note this still
        # does not get an armhf binary to run: LLVM emits NEON (`vmov.i32 qN`)
        # for bulk operations on armv7, which Unicorn does not implement.
        try:
            from unicorn.arm_const import UC_ARM_REG_C1_C0_2
            mu.reg_write(UC_ARM_REG_C1_C0_2, 0x00F00000)
        except Exception as e:  # pragma: no cover - surface it, never swallow
            sys.stderr.write(f"warning: could not enable VFP/NEON: {e}\n")
        return mu, "intr", None
    if arch == "riscv64":
        return Uc(UC_ARCH_RISCV, UC_MODE_RISCV64), "intr", None
    if arch == "riscv32":
        return Uc(UC_ARCH_RISCV, UC_MODE_RISCV32), "intr", None
    raise SystemExit("bad arch")


def syscall_regs(arch):
    """(number, arg0, arg1, arg2) registers -- arg2 is write(2)'s length."""
    if arch == "x86_64":
        return (UC_X86_REG_RAX, UC_X86_REG_RDI, UC_X86_REG_RSI, UC_X86_REG_RDX)
    if arch == "aarch64":
        return (UC_ARM64_REG_X8, UC_ARM64_REG_X0, UC_ARM64_REG_X1, UC_ARM64_REG_X2)
    if arch in ("arm", "armel"):
        return (UC_ARM_REG_R7, UC_ARM_REG_R0, UC_ARM_REG_R1, UC_ARM_REG_R2)
    if arch in ("riscv64", "riscv32"):
        return (UC_RISCV_REG_A7, UC_RISCV_REG_A0, UC_RISCV_REG_A1, UC_RISCV_REG_A2)
    raise SystemExit("bad arch")


def ret_reg(arch):
    """Register that receives the syscall *return* value.

    Every ABI but x86-64 puts it in the first argument register. x86-64 is the
    odd one out because RAX also carries the syscall number, so write()'s
    result has to go back into RAX: writing it to RDI instead overwrites the
    fd and leaves RAX = 1, which makes the C library's write loop advance a
    single byte per iteration and re-emit the tail of the line each time.
    """
    if arch == "x86_64":
        return UC_X86_REG_RAX
    return syscall_regs(arch)[1]


def sp_pc(arch):
    if arch == "x86_64":
        return UC_X86_REG_RSP, UC_X86_REG_RIP
    if arch == "aarch64":
        return UC_ARM64_REG_SP, UC_ARM64_REG_PC
    if arch in ("arm", "armel"):
        return UC_ARM_REG_SP, UC_ARM_REG_PC
    if arch in ("riscv64", "riscv32"):
        return UC_RISCV_REG_SP, UC_RISCV_REG_PC
    raise SystemExit("bad arch")


WRITE = {"x86_64": 1, "aarch64": 64, "arm": 4, "armel": 4,
         "riscv64": 64, "riscv32": 64}
# x86-64 goc exits through exit_group (231), not exit (60); matching only 60
# left the program falling off the end of the stub into unmapped memory.
EXIT = {"x86_64": (60, 231), "aarch64": (93, 94), "arm": (1, 248),
        "armel": (1, 248), "riscv64": (93, 94), "riscv32": (93, 94)}


def run(path):
    data = open(path, "rb").read()
    arch, cls = detect(data)
    entry, segs = load_segs(data, cls)
    mu, hook_kind, insn = setup(arch)
    mu.mem_map(STACK_TOP - STACK_SIZE, STACK_SIZE)
    mapped = set()
    for p_vaddr, p_offset, p_filesz, p_memsz in segs:
        base = p_vaddr & ~(PAGE - 1)
        size = (p_memsz + PAGE - 1) & ~(PAGE - 1)
        if size == 0:
            size = PAGE
        # Unicorn refuses to map a page twice, and gocld lays segments out so
        # that two of them can share a page; skip the overlap.
        if base in mapped:
            continue
        mapped.add(base)
        mu.mem_map(base, size)
        if p_filesz:
            mu.mem_write(p_vaddr, data[p_offset:p_offset + p_filesz])
    sp_r, pc_r = sp_pc(arch)
    mu.reg_write(sp_r, STACK_TOP)
    mu.reg_write(pc_r, entry)

    num_r, a0_r, a1_r, a2_r = syscall_regs(arch)
    ret_r = ret_reg(arch)
    result = {"nw": 0}

    def do_syscall():
        nr = mu.reg_read(num_r)
        a0 = mu.reg_read(a0_r)
        a1 = mu.reg_read(a1_r)
        a2 = mu.reg_read(a2_r)
        if nr == WRITE[arch]:
            buf = bytes(mu.mem_read(a1, a2))
            sys.stdout.buffer.write(buf)
            sys.stdout.buffer.flush()
            if DEBUG:
                sys.stderr.write(f"[write #{result['nw']} fd={a0} len={a2} buf={buf!r}]\n")
                result["nw"] += 1
            # write(2) returns the byte count, and the C library's sequential
            # path loops on it (p += w; len -= w). Unicorn leaves the register
            # untouched, so without this the loop crawls forward one byte per
            # call and prints the line's suffixes one after another.
            mu.reg_write(ret_r, a2)
        elif nr in EXIT[arch]:
            result["exit"] = a0
            mu.emu_stop()

    def on_intr(uc, intno, user):
        do_syscall()

    def on_insn(uc, user):
        do_syscall()

    if hook_kind == "intr":
        mu.hook_add(UC_HOOK_INTR, on_intr)
    else:
        mu.hook_add(UC_HOOK_INSN, on_insn, None, 1, 0, insn)

    try:
        mu.emu_start(entry, 0, timeout=30_000_000, count=200_000_000)
    except Exception as e:
        import traceback
        pc = mu.reg_read(pc_r)
        addr = getattr(e, "address", None)
        sys.stderr.write(f"EMU ERROR at pc={pc:#x} addr={addr!r}: {e}\n")
        traceback.print_exc()
        return 2

    if "exit" not in result:
        sys.stderr.write("no exit syscall (did not terminate)\n")
        return 3
    sys.stderr.write(f"[exit code = {result['exit']}]\n")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2 or sys.argv[1] in ("-h", "--help"):
        sys.stderr.write(__doc__)
        raise SystemExit(0 if len(sys.argv) == 2 else 2)
    raise SystemExit(run(sys.argv[1]))
