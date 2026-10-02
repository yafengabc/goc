#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""T1.6 benefit probe: quantify the int-carry-model inflation in goc asm output.

Metrics per .asm file (all counts are of REAL instruction lines; labels,
directives and comments excluded):
  instr     : total instruction lines
  wrapS     : adjacent `shl r,32` + `sar r,32` pairs (signed canonInt/extendInt)
  wrapU     : adjacent `shl r,32` + `shr r,32` pairs (unsigned canonInt)
  shl32/sar32/shr32: individual canonicalisation-shift counts
  movsxd    : sign-extension count (sites already narrowed today)
  dstore    : dword stores   mov [mem], <32-bit reg>    (hardware truncation)
  dload     : dword loads    mov <32-bit reg>, [mem]    (zero-extending)
  cmp       : cmp instructions
  leaIdx    : lea with a scaled index register (lea r,[r+r*k])
  op64      : 64-bit int-class ALU ops (add/sub/imul/and/or/xor/neg/not)
  op64w     : of those, ops whose result is wrapped right after (op;shl;sar)
  op32      : 32-bit ALU ops already emitted
  frameLoad / frameStore: [rbp-N] slot loads / stores
  call / ret: call and ret counts

Savings model: a wrapped int op costs op + shl + sar = 3 lines; the 32-bit
form costs 1. grossSave = (wrapS + wrapU) * 2. True saving = grossSave minus
the movsxd inserts at genuine 64-bit consumption points.
"""
import re
import sys

REG64 = r'(?:rax|rbx|rcx|rdx|rsi|rdi|rbp|rsp|r8|r9|r10|r11|r12|r13|r14|r15)'
REG32 = r'(?:eax|ebx|ecx|edx|esi|edi|ebp|esp|r8d|r9d|r10d|r11d|r12d|r13d|r14d|r15d)'
ALU = r'(?:add|sub|imul|and|or|xor|neg|not)'

INSTR = re.compile(r'^([a-z][a-z0-9]*)(?: (.*))?$')
WRAPS = re.compile(r'^shl (' + REG64 + r'), 32$')
WRAPU = re.compile(r'^shr (' + REG64 + r'), 32$')
SAR32 = re.compile(r'^sar (' + REG64 + r'), 32$')
MOVSXD = re.compile(r'^movsxd\b')
DSTORE = re.compile(r'^mov (?:dword )?\[.*\], (' + REG32 + r')$')
DLOAD = re.compile(r'^mov (' + REG32 + r'), (?:dword )?\[.*\]$')
CMP = re.compile(r'^cmp\b')
LEAIDX = re.compile(r'^lea ' + REG64 + r', \[[^\]]*\*')
OP64 = re.compile(r'^' + ALU + r' (' + REG64 + r')\b')
OP32 = re.compile(r'^' + ALU + r'(?:, )? (' + REG32 + r')\b')
FLOAD = re.compile(r'^mov (' + REG64 + r'|' + REG32 + r'), \[rbp-\d+\]')
FSTORE = re.compile(r'^mov \[rbp-\d+\], (' + REG64 + r'|' + REG32 + r')$')
DIV64 = re.compile(r'^(?:idiv|div) r\d+$|^cqo$|^cdq$')
DIV32 = re.compile(r'^(?:idiv|div) r\d+d$')
IDXLD = re.compile(r'^mov r1[0-5], \[rbp-\d+\]$')
CALL = re.compile(r'^call\b')
RET = re.compile(r'^ret\b')


def reg32of(r):
    """32-bit half of a 64-bit register name."""
    m = re.match(r'^r(1[0-5]|[0-9])$', r)
    if m:
        return 'r' + m.group(1) + 'd'
    m = re.match(r'^r(ax|bx|cx|dx|si|di|bp|sp)$', r)
    if m:
        return {'ax': 'eax', 'bx': 'ebx', 'cx': 'ecx', 'dx': 'edx',
                'si': 'esi', 'di': 'edi', 'bp': 'ebp', 'sp': 'esp'}[m.group(1)]
    return r


def findDef(bodies, i, reg):
    """Walk back from body index i (the shl of a pair) following the value in
    `reg` through mov-shuffles, and return (op, line) of the nearest
    instruction defining it, or None. Boundaries (call/ret/branch/label)
    stop the walk."""
    for back in range(1, 6):
        if i - back < 0:
            return None
        p = bodies[i - back]
        if CALL.match(p) or RET.match(p) or re.match(r'^j[a-z]+\b', p) \
                or p.startswith('.') or re.match(r'^[a-z0-9_.]+:', p):
            return None
        m = re.match(r'^(mov|add|sub|imul|and|or|xor|neg|not|inc|dec|lea)\s+([a-z0-9]+)', p)
        if not m:
            continue  # flags/other instruction: not a def of reg
        op, dst = m.group(1), m.group(2)
        if op == 'mov' and dst == reg:
            # A move INTO reg from another register: follow the source.
            sm = re.match(r'^mov ' + re.escape(reg) + r', (' + REG64 + r'|' + REG32 + r')$', p)
            if sm:
                src = sm.group(1)
                if src in ('rax', 'eax', 'rbx', 'ebx', 'rcx', 'ecx', 'rdx', 'edx',
                           'rsi', 'esi', 'rdi', 'edi', 'r8', 'r8d', 'r9', 'r9d',
                           'r10', 'r10d', 'r11', 'r11d', 'r12', 'r12d', 'r13',
                           'r13d', 'r14', 'r14d', 'r15', 'r15d'):
                    return findDef(bodies, i - back, src)
                return op, p
            return op, p  # e.g. mov rax, 5 (immediate) -- not a wrap source
        if dst == reg or dst == reg32of(reg):
            return op, p
    return None


def classifyWrap(bodies, i):
    """Classify a shl rax,32;sar rax,32 pair by the instruction defining rax:
      0 = ALU result (wrap canonicalises the op),
      1 = dword memory load (mov e32,[..]) re-sign-extended,
      2 = slot/register move into rax,
      3 = other."""
    d = findDef(bodies, i, 'rax')
    if d is None:
        return 3
    op, line = d
    if op in ('add', 'sub', 'imul', 'and', 'or', 'xor', 'neg', 'not', 'inc',
              'dec'):
        return 0
    if op == 'mov':
        if re.match(r'^mov e[a-z0-9]+, \[', line):
            return 1
        if re.match(r'^mov rax, \[rbp', line):
            return 2
        return 2
    return 3


def probe(path, debug=False):
    st = {
        'instr': 0, 'wrapS': 0, 'wrapU': 0, 'shl32': 0, 'sar32': 0, 'shr32': 0,
        'movsxd': 0, 'dstore': 0, 'dload': 0, 'cmp': 0, 'leaIdx': 0,
        'op64': 0, 'op64w': 0, 'op32': 0, 'frameLoad': 0, 'frameStore': 0,
        'call': 0, 'ret': 0, 'wrapCat': [0, 0, 0, 0], 'div64': 0, 'div32': 0,
        'idxld': 0,
    }
    bodies = []  # instruction bodies without the leading tab
    with open(path, 'r', encoding='utf-8', errors='replace') as f:
        for raw in f:
            s = raw.rstrip('\n')
            if s.startswith('\t'):
                bodies.append(s[1:])
    n = len(bodies)
    for i, b in enumerate(bodies):
        m = INSTR.match(b)
        if not m:
            continue
        op = m.group(1)
        rest = m.group(2) or ''
        st['instr'] += 1
        if CALL.match(b):
            st['call'] += 1
        if RET.match(b):
            st['ret'] += 1
        if CMP.match(b):
            st['cmp'] += 1
        if MOVSXD.match(b):
            st['movsxd'] += 1
        if DSTORE.match(b):
            st['dstore'] += 1
        if DLOAD.match(b):
            st['dload'] += 1
        if LEAIDX.match(b):
            st['leaIdx'] += 1
        if FLOAD.match(b):
            st['frameLoad'] += 1
        if FSTORE.match(b):
            st['frameStore'] += 1
        if OP64.match(b):
            st['op64'] += 1
            if i + 2 < n and WRAPS.match(bodies[i + 1]) and SAR32.match(bodies[i + 2]):
                st['op64w'] += 1
        if OP32.match(b):
            st['op32'] += 1
        if DIV64.match(b):
            st['div64'] += 1
        if DIV32.match(b):
            st['div32'] += 1
        if IDXLD.match(b):
            st['idxld'] += 1
        mw = WRAPS.match(b)
        if mw:
            st['shl32'] += 1
            if i + 1 < n and SAR32.match(bodies[i + 1]):
                nxt = SAR32.match(bodies[i + 1])
                if nxt.group(1) == mw.group(1):
                    st['wrapS'] += 1
                    cat = classifyWrap(bodies, i)
                    st['wrapCat'][cat] += 1
                    if debug and st['wrapS'] <= 12:
                        print(f"  cat={cat} @" + str(i) + ": " + " | ".join(
                            bodies[max(0, i - 4):i + 2]))
        mu = WRAPU.match(b)
        if mu:
            st['shr32'] += 1
            if i + 1 < n and re.match(r'^shr (' + REG64 + r'), 32$', bodies[i + 1]):
                nxt = re.match(r'^shr (' + REG64 + r'), 32$', bodies[i + 1])
                if nxt.group(1) == mu.group(1):
                    st['wrapU'] += 1
        if SAR32.match(b):
            st['sar32'] += 1
    return st


def fmt(name, st):
    saved = (st['wrapS'] + st['wrapU']) * 2
    pct = 100.0 * saved / st['instr'] if st['instr'] else 0
    return (
        f"{name:28s} instr={st['instr']:6d} wrapS={st['wrapS']:4d} wrapU={st['wrapU']:3d} "
        f"[ALU={st['wrapCat'][0]:3d} mem={st['wrapCat'][1]:3d} reg={st['wrapCat'][2]:3d} oth={st['wrapCat'][3]:3d}] "
        f"movsxd={st['movsxd']:3d} dstore={st['dstore']:4d} dload={st['dload']:4d} "
        f"cmp={st['cmp']:4d} op64={st['op64']:4d} op32={st['op32']:4d} "
        f"div64={st['div64']:2d} div32={st['div32']:2d} idx={st['idxld']:4d} "
        f"frLd={st['frameLoad']:4d} frSt={st['frameStore']:4d} "
        f"call={st['call']:3d} | gross={saved:4d} ({pct:.1f}%)"
    )


def main(paths):
    debug = '--debug' in sys.argv
    if debug:
        paths = [p for p in paths if p != '--debug']
    tot = None
    for p in paths:
        st = probe(p, debug)
        print(fmt(p, st))
        if tot is None:
            tot = dict(st)
        else:
            for k in tot:
                if isinstance(tot[k], list):
                    tot[k] = [a + b for a, b in zip(tot[k], st[k])]
                else:
                    tot[k] += st[k]
    if len(paths) > 1:
        print(fmt('TOTAL', tot))


if __name__ == '__main__':
    main(sys.argv[1:])
