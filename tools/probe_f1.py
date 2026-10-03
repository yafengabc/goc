# F1 probe: count "imul r11, K; add r10, r11; mov .., [r10]" adjacent triples
# that sibFold can collapse (K in {1,2,4,8}, load or store direction).
import re, sys, glob

LOAD = re.compile(r'^\s*mov\s+([a-z0-9]+)\s*,\s*\[r10\]\s*$')
STORE = re.compile(r'^\s*mov\s+(byte\s+|word\s+|dword\s+)?\[r10\]\s*,\s*(.+?)\s*$')
IMUL = re.compile(r'^\s*imul\s+r11,\s*(\d+)\s*$')
ADD = re.compile(r'^\s*add\s+r10,\s*r11\s*$')
FLAGS_RD = re.compile(r'^\s*(j[a-z]+|set[a-z]+|cmov[a-z]+|adc|sbb|jc|jnc|jo|jno|js|jns|jz|jnz|jpe|jpo)\s')

def count_folds(path):
    lines = open(path, encoding='utf-8', errors='replace').read().splitlines()
    n = 0
    ok_regs = {'eax','rax','ax','al','xmm0','xmm1','ecx','rcx','dx','bl','cl'}
    for i in range(len(lines)-2):
        m = IMUL.match(lines[i])
        if not m: continue
        k = int(m.group(1))
        if k not in (1,2,4,8): continue
        if not ADD.match(lines[i+1]): continue
        dst = None; src = None
        ml = LOAD.match(lines[i+2])
        if ml:
            dst = ml.group(1)
            if dst in ('r10','r11','r10d','r11d','r10b','r11b'): continue
        else:
            ms = STORE.match(lines[i+2])
            if ms:
                src = (ms.group(2) or '').strip()
                if re.search(r'\br10\b|\br11\b', src): continue
            else:
                continue
        # window: from i+3 until a write/read of r10/r11, flags read, or break
        brk = False
        for j in range(i+3, len(lines)):
            L = lines[j]
            if re.search(r'\br10\b|\br11\b', L):
                brk = True; break
            if FLAGS_RD.match(L):
                brk = True; break
            if L.strip().startswith('.') or re.search(r'^\s*(call|jmp|ret|movsd|movss|fld|fst|fstp|stos|scas|rep|lock)', L):
                brk = True; break
            # a flags-writing ALU op after the triple resets flag state
            if re.search(r'^\s*(add|sub|and|or|xor|shl|shr|sar|neg|inc|dec|imul|mul|cmp|test|lea)\s', L):
                break  # flags now owned by this op; triple's flags dead
        if brk: continue
        n += 1
    return n

def main():
    files = sys.argv[1:] or ['bench2.asm','optprobe.asm','bsort.asm']
    tot = 0
    for f in files:
        try:
            c = count_folds(f)
        except FileNotFoundError:
            c = -1
        print(f'{f}: folds={c}')
        if c >= 0: tot += c
    print(f'TOTAL folds={tot}')

if __name__ == '__main__':
    main()
