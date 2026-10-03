#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""F4 slotCache value-snapshot benefit probe (READ-ONLY).

Linear-scan simulator. On the SAME emitted .asm (already post T1.4/current
slotCache), replay two models and count forwardable unsized slot loads
`mov <r64>, [s]`:

  * current semantics (T1.4, src/opt.go slotCache):
        cache: slot -> ('reg', r) | ('imm', v)
        write r  -> delete every cache entry sourced by r   (killReg)
  * snapshot semantics (F4 proposal):
        slotRoot: slot -> canonical root register (copy-chain root)
        slotImm : slot -> int
        rootOf  : reg  -> root it currently mirrors (transitive)
        `mov d, s` (GP-GP) -> rootOf[d] = resolve(s); old chain dropped
        write r  -> drop rootOf[r] AND slots whose root == r;
                    slots rooted at OTHER registers survive
        load `mov d,[s]` -> if slot has a root/imm, FORWARD (value still live)

Barriers (identical in both, from opt.go): label/non-instr, call/ret/leave/
push/pop/loop/j*, non-slot memory write, lea of a slot address -> clearAll.
Sized / non-mov stores -> clearSlot only. xmm/other source -> clearSlot.

A snapshot-new forward is classified by whether the register actually stored
into the slot (`directSrc`, from the real `mov [s], X`) equals the forwarded
root:
  B (copy-chain) : root != directSrc   (e.g. store [s]=rax but rax mirrors r12,
                                       rax got rewritten, r12 still alive)
  A (direct root): root == directSrc  (structurally expected ~0: a correct
                                       snapshot also drops a slot whose own
                                       root register is overwritten)
"""
import glob
import os
import re
import sys

GP64 = {'rax', 'rbx', 'rcx', 'rdx', 'rsi', 'rdi', 'rbp', 'rsp',
        'r8', 'r9', 'r10', 'r11', 'r12', 'r13', 'r14', 'r15'}

# 64-bit name for a 64/32/8-bit register operand
def r64(r):
    if r in GP64:
        return r
    m = re.match(r'^r(1[0-5]|[0-9])[dwb]?$', r)      # r8 / r8d / r8w / r8b
    if m:
        return 'r' + m.group(1)
    m = re.match(r'^r(ax|bx|cx|dx|si|di|bp|sp)[lb]?$', r)
    if m:
        return 'r' + m.group(1)
    e32 = {'eax': 'rax', 'ebx': 'rbx', 'ecx': 'rcx', 'edx': 'rdx',
           'esi': 'rsi', 'edi': 'rdi', 'ebp': 'rbp', 'esp': 'rsp'}
    if r in e32:
        return e32[r]
    small = {'al': 'rax', 'cl': 'rcx', 'dl': 'rdx', 'bl': 'rbx',
             'sil': 'rsi', 'dil': 'rdi', 'bpl': 'rbp', 'spl': 'rsp'}
    if r in small:
        return small[r]
    return r

MEM_RE = re.compile(r'^(?:(?:dword|qword|byte|word)\s+)?\[.*\]$')
SIZE_RE = re.compile(r'^((?:dword|qword|byte|word)\s+)?\[(.*)\]$')
SLOT_STACK = re.compile(r'^rbp-\d+$')
SLOT_RIP = re.compile(r'^rip\+[A-Za-z0-9_$.@]+$')
IMM_RE = re.compile(r'^-?\d+$')
INSTR = re.compile(r'^([a-z][a-z0-9]*)(?:\s+(.*))?$')
JCC = re.compile(r'^j[a-z]+$')


def parse_body(body):
    """Return (op, [operands]) or None."""
    m = INSTR.match(body)
    if not m:
        return None
    op = m.group(1)
    rest = (m.group(2) or '').strip()
    if not rest:
        return op, []
    ops = [o.strip() for o in rest.split(',')]
    return op, ops


def mem_split(opnd):
    """(size, bare-mem-inside-brackets) or None."""
    m = SIZE_RE.match(opnd)
    if not m:
        return None
    return m.group(1).strip() if m.group(1) else '', m.group(2)


def is_slot(bare):
    return bool(SLOT_STACK.match(bare)) or bool(SLOT_RIP.match(bare))


class Simulator:
    def __init__(self):
        # current (T1.4)
        self.cur = {}                 # slot -> ('reg', r) | ('imm', v)
        # snapshot (F4)
        self.root_of = {}             # reg -> root it mirrors
        self.slot_root = {}           # slot -> root register
        self.slot_imm = {}            # slot -> int
        self.direct_src = {}          # slot -> register in the actual last store

        self.n_instr = 0
        self.n_load = 0               # unsized mov r64,[slot]
        self.cur_fwd = 0              # current would forward (re-sim)
        self.sn_fwd = 0               # snapshot would forward (re-sim)
        self.delta = 0                # snapshot forward but current did not
        self.shapeA = 0
        self.shapeB = 0

    # ---- current helpers ----
    def cur_clear_all(self):
        self.cur.clear()

    def cur_clear_slot(self, s):
        self.cur.pop(s, None)

    def cur_kill(self, r):
        r = r64(r)
        for k in [k for k, v in self.cur.items() if v[0] == 'reg' and v[1] == r]:
            del self.cur[k]

    # ---- snapshot helpers ----
    def sn_clear_all(self):
        self.root_of.clear()
        self.slot_root.clear()
        self.slot_imm.clear()
        self.direct_src.clear()

    def sn_clear_slot(self, s):
        self.slot_root.pop(s, None)
        self.slot_imm.pop(s, None)
        self.direct_src.pop(s, None)

    def sn_resolve(self, r):
        seen = set()
        while r in self.root_of and r not in seen:
            seen.add(r)
            r = self.root_of[r]
        return r

    def sn_kill(self, r):
        r = r64(r)
        self.root_of.pop(r, None)
        for k in [k for k, root in self.slot_root.items() if root == r]:
            del self.slot_root[k]

    # ---- main per-line ----
    def feed(self, body):
        if body.startswith('.') and body.endswith(':'):
            return  # local label: treated as boundary below by caller
        pr = parse_body(body)
        if pr is None:
            self.cur_clear_all()
            self.sn_clear_all()
            return
        op, ops = pr
        self.n_instr += 1

        # control flow / stack ops break every window
        if op in ('call', 'ret', 'leave', 'push', 'pop', 'loop') or JCC.match(op):
            self.cur_clear_all(); self.sn_clear_all()
            return

        # ---- memory WRITE as destination ----
        if ops:
            ms = mem_split(ops[0])
            if ms is not None:
                size, bare = ms
                if not is_slot(bare):
                    self.cur_clear_all(); self.sn_clear_all()
                    return
                if op != 'mov' or size != '' or len(ops) != 2:
                    self.cur_clear_slot(bare); self.sn_clear_slot(bare)
                    return
                src = ops[1]
                if src in GP64:
                    self.cur[bare] = ('reg', src)
                    self.slot_root[bare] = self.sn_resolve(src)
                    self.direct_src[bare] = src
                    self.slot_imm.pop(bare, None)
                    return
                mi = IMM_RE.match(src)
                if mi:
                    v = int(src)
                    self.cur[bare] = ('imm', v)
                    self.slot_imm[bare] = v
                    self.slot_root.pop(bare, None)
                    self.direct_src.pop(bare, None)
                    return
                # xmm / untracked source
                self.cur_clear_slot(bare); self.sn_clear_slot(bare)
                return

        # ---- lea ----
        if op == 'lea' and len(ops) == 2:
            ms = mem_split(ops[1])
            if ms is not None and is_slot(ms[1]):
                self.cur_clear_all(); self.sn_clear_all()
                return
            self.cur_kill(ops[0]); self.sn_kill(ops[0])
            return

        # ---- plain mov ----
        if op == 'mov' and len(ops) == 2:
            dst, src = ops[0], ops[1]
            if mem_split(dst) is not None:
                return  # store (already handled)
            ms = mem_split(src)
            if ms is not None:
                size, bare = ms
                # load
                if dst in GP64 and size == '' and is_slot(bare):
                    self.n_load += 1
                    cur_hit = bare in self.cur
                    sn_hit = (bare in self.slot_root) or (bare in self.slot_imm)
                    # current
                    if cur_hit:
                        self.cur_fwd += 1
                    self.cur_kill(dst)
                    # snapshot
                    if sn_hit:
                        self.sn_fwd += 1
                        if not cur_hit:
                            self.delta += 1
                            dsrc = self.direct_src.get(bare)
                            if bare in self.slot_imm or dsrc is None or self.slot_root.get(bare) == dsrc:
                                self.shapeA += 1
                            else:
                                self.shapeB += 1
                        # forwarded load now mirrors the root/imm
                        self.sn_kill(dst)
                        if bare in self.slot_root:
                            self.root_of[dst] = self.slot_root[bare]
                    else:
                        self.sn_kill(dst)
                    return
                # non-forwardable load: dst still written
                if dst in GP64:
                    self.cur_kill(dst); self.sn_kill(dst)
                return
            # GP-GP mov or imm->reg (no memory)
            self.cur_kill(dst)
            self.sn_kill(dst)
            if src in GP64:
                self.root_of[dst] = self.sn_resolve(src)
            else:
                self.root_of.pop(dst, None)
            return

        # ---- ALU / flags ----
        if op in ('cmp', 'test'):
            return
        if op in ('cqo', 'cdq'):
            self.cur_kill('rax'); self.cur_kill('rdx')
            self.sn_kill('rax'); self.sn_kill('rdx')
            return
        if op in ('imul', 'mul', 'div', 'idiv'):
            if len(ops) == 1:
                self.cur_kill('rax'); self.cur_kill('rdx')
                self.sn_kill('rax'); self.sn_kill('rdx')
            elif ops:
                self.cur_kill(ops[0]); self.sn_kill(ops[0])
            return
        # default: first non-mem operand is written
        if ops and mem_split(ops[0]) is None:
            self.cur_kill(ops[0]); self.sn_kill(ops[0])


def analyze(path):
    sim = Simulator()
    with open(path, 'r', encoding='utf-8', errors='replace') as f:
        for raw in f:
            s = raw.rstrip('\n')
            if s.startswith('\t'):
                sim.feed(s[1:].strip())
            elif s.startswith('.') and s.rstrip().endswith(':'):
                # local label -> barrier
                sim.cur_clear_all(); sim.sn_clear_all()
            elif re.match(r'^[A-Za-z_][A-Za-z0-9_$.]*:\s*$', s):
                # function label -> barrier
                sim.cur_clear_all(); sim.sn_clear_all()
    return sim


def row(tag, sim):
    pct = 100.0 * sim.delta / sim.n_instr if sim.n_instr else 0
    return (f"{tag:22s} instr={sim.n_instr:7d} load={sim.n_load:6d} "
            f"curFwd={sim.cur_fwd:5d} snFwd={sim.sn_fwd:5d} "
            f"delta={sim.delta:5d} ({pct:4.2f}%)  "
            f"A={sim.shapeA:4d} B={sim.shapeB:4d}")


def main():
    corpus = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                          'f4corpus')
    files = []
    for opt in ('O1', 'O2'):
        files.append((f'bench2.{opt}', os.path.join(corpus, f'bench2.{opt}.asm')))
        files.append((f'optprobe.{opt}', os.path.join(corpus, f'optprobe.{opt}.asm')))
    ex = os.path.join(corpus, 'examples')
    for opt in ('O1', 'O2'):
        for p in sorted(glob.glob(os.path.join(ex, f'*.{opt}.asm'))):
            files.append((os.path.basename(p)[:-len('.asm')], p))

    agg = {}
    print(f"{'corpus':22s} {'instr':>8} {'load':>7} {'curFwd':>7} {'snFwd':>7} "
          f"{'delta':>7} {'%':>7}  A/B")
    print('-' * 90)
    for tag, p in files:
        if not os.path.exists(p):
            print(f"{tag:22s}  MISSING {p}")
            continue
        sim = analyze(p)
        print(row(tag, sim))
        a = agg.setdefault(tag.split('.')[-1] if '.' in tag else tag, None)
    # aggregate by opt level + grand total
    tot = None
    peropt = {}
    for tag, p in files:
        if not os.path.exists(p):
            continue
        s = analyze(p)
        opt = tag.split('.')[-1]
        d = peropt.setdefault(opt, None)
        if d is None:
            peropt[opt] = dict(vars(s))
        else:
            for k in ('n_instr', 'n_load', 'cur_fwd', 'sn_fwd', 'delta',
                      'shapeA', 'shapeB'):
                d[k] += getattr(s, k)
        if tot is None:
            tot = dict(vars(s))
        else:
            for k in ('n_instr', 'n_load', 'cur_fwd', 'sn_fwd', 'delta',
                      'shapeA', 'shapeB'):
                tot[k] += getattr(s, k)
    print('-' * 90)
    for opt in sorted(peropt):
        d = peropt[opt]
        pct = 100.0 * d['delta'] / d['n_instr']
        print(f"{'(all) ' + opt:22s} instr={d['n_instr']:7d} load={d['n_load']:6d} "
              f"curFwd={d['cur_fwd']:5d} snFwd={d['sn_fwd']:5d} "
              f"delta={d['delta']:5d} ({pct:4.2f}%)  A={d['shapeA']} B={d['shapeB']}")
    pct = 100.0 * tot['delta'] / tot['n_instr']
    print(f"{'(GRAND)':22s} instr={tot['n_instr']:7d} load={tot['n_load']:6d} "
          f"curFwd={tot['cur_fwd']:5d} snFwd={tot['sn_fwd']:5d} "
          f"delta={tot['delta']:5d} ({pct:4.2f}%)  A={tot['shapeA']} B={tot['shapeB']}")


if __name__ == '__main__':
    main()
