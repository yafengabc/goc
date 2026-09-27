// elfcheck verifies a Linux ELF64 binary produced by goa and then *runs* it
// by interpreting its instructions. Native execution needs a Linux kernel;
// this gets as close as a Windows box can:
//
//  1. structural check -- every header field the loader actually reads
//  2. load the image the way the kernel would (PT_LOAD -> virtual memory)
//  3. interpret from the entry point, emulating write/exit syscalls
//
// The program's own output goes to stdout (so it can be diffed against a
// golden file exactly like a real run); diagnostics go to stderr.
//
// The interpreter covers the instruction subset goa emits. Anything else is a
// loud "unsupported opcode" failure, never a silent wrong answer.
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: elfcheck [-v] <elf>")
		os.Exit(2)
	}
	path := os.Args[len(os.Args)-1]
	verbose := len(os.Args) > 2 && os.Args[1] == "-v"

	f, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(2)
	}

	img, err := checkELF(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "elfcheck: %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "elfcheck: %s: ELF64 ok, entry=0x%x, %d sections\n",
		path, img.entry, img.shnum)

	c := newCPU(img, verbose)
	code := c.run()
	fmt.Fprintf(os.Stderr, "elfcheck: ran %d instructions, exit=%d\n", c.steps, code)
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// 1. Structural check
// ---------------------------------------------------------------------------

type image struct {
	data  []byte // the mapped image, starting at base
	base  uint64
	entry uint64
	shnum int
}

func checkELF(f []byte) (*image, error) {
	if len(f) < 64 {
		return nil, fmt.Errorf("file too short to hold an ELF header")
	}
	if f[0] != 0x7f || f[1] != 'E' || f[2] != 'L' || f[3] != 'F' {
		return nil, fmt.Errorf("not an ELF file (bad magic)")
	}
	if f[4] != 2 {
		return nil, fmt.Errorf("not ELF64 (EI_CLASS=%d)", f[4])
	}
	if f[5] != 1 {
		return nil, fmt.Errorf("not little-endian (EI_DATA=%d)", f[5])
	}
	if f[6] != 1 {
		return nil, fmt.Errorf("EI_VERSION=%d, want 1", f[6])
	}
	le := binary.LittleEndian
	eType := le.Uint16(f[16:])
	eMachine := le.Uint16(f[18:])
	eEntry := le.Uint64(f[24:])
	ePhoff := le.Uint64(f[32:])
	eShoff := le.Uint64(f[40:])
	eEhsize := le.Uint16(f[52:])
	ePhentsize := le.Uint16(f[54:])
	ePhnum := le.Uint16(f[56:])
	eShentsize := le.Uint16(f[58:])
	eShnum := le.Uint16(f[60:])
	eShstrndx := le.Uint16(f[62:])

	if eType != 2 {
		return nil, fmt.Errorf("e_type=%d, want 2 (ET_EXEC)", eType)
	}
	if eMachine != 0x3e {
		return nil, fmt.Errorf("e_machine=0x%x, want 0x3e (x86-64)", eMachine)
	}
	if eEhsize != 64 {
		return nil, fmt.Errorf("e_ehsize=%d, want 64", eEhsize)
	}
	if ePhentsize != 56 {
		return nil, fmt.Errorf("e_phentsize=%d, want 56", ePhentsize)
	}
	if ePhnum < 1 {
		return nil, fmt.Errorf("e_phnum=0: nothing to load")
	}
	if ePhoff+uint64(ePhnum)*uint64(ePhentsize) > uint64(len(f)) {
		return nil, fmt.Errorf("program headers run past end of file")
	}
	if eShnum > 0 {
		if eShentsize != 64 {
			return nil, fmt.Errorf("e_shentsize=%d, want 64", eShentsize)
		}
		if eShoff+uint64(eShnum)*64 > uint64(len(f)) {
			return nil, fmt.Errorf("section headers run past end of file")
		}
		if eShstrndx >= eShnum {
			return nil, fmt.Errorf("e_shstrndx=%d >= e_shnum=%d", eShstrndx, eShnum)
		}
		// .shstrtab must be a STRTAB whose bytes are all inside the file.
		sh := f[eShoff+uint64(eShstrndx)*64:]
		strOff := le.Uint64(sh[24:])
		strSize := le.Uint64(sh[32:])
		if strOff+strSize > uint64(len(f)) {
			return nil, fmt.Errorf(".shstrtab runs past end of file")
		}
		// Every section name offset must point inside .shstrtab.
		for i := 0; i < int(eShnum); i++ {
			h := f[eShoff+uint64(i)*64:]
			noff := le.Uint32(h[0:])
			if noff >= uint32(strSize) {
				return nil, fmt.Errorf("section %d: name offset %d outside .shstrtab", i, noff)
			}
		}
	}

	// Program headers: find the loadable, executable segment containing entry.
	var img image
	found := false
	for i := 0; i < int(ePhnum); i++ {
		h := f[ePhoff+uint64(i)*56:]
		pType := le.Uint32(h[0:])
		pFlags := le.Uint32(h[4:])
		pOffset := le.Uint64(h[8:])
		pVaddr := le.Uint64(h[16:])
		pFilesz := le.Uint64(h[32:])
		pMemsz := le.Uint64(h[40:])
		pAlign := le.Uint64(h[48:])
		if pType != 1 { // PT_LOAD
			continue
		}
		if pAlign == 0 {
			pAlign = 1
		}
		if pVaddr%pAlign != pOffset%pAlign {
			return nil, fmt.Errorf("PT_LOAD %d: p_vaddr 0x%x and p_offset 0x%x disagree mod 0x%x",
				i, pVaddr, pOffset, pAlign)
		}
		if pFilesz > pMemsz {
			return nil, fmt.Errorf("PT_LOAD %d: p_filesz > p_memsz", i)
		}
		if pOffset+pFilesz > uint64(len(f)) {
			return nil, fmt.Errorf("PT_LOAD %d: extends past end of file", i)
		}
		if pFlags&0x1 == 0 { // not executable
			continue
		}
		if eEntry >= pVaddr && eEntry < pVaddr+pFilesz {
			img = image{
				data:  append([]byte(nil), f[pOffset:pOffset+pFilesz]...),
				base:  pVaddr,
				entry: eEntry,
				shnum: int(eShnum),
			}
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("entry 0x%x is not inside any executable PT_LOAD", eEntry)
	}
	return &img, nil
}

// ---------------------------------------------------------------------------
// 2. + 3. Loading and interpreting
// ---------------------------------------------------------------------------

const (
	stackSize = 64 * 1024
	stackBase = 0x7fff0000
	heapBase  = 0x50000000
	heapSize  = 1 << 20 // 1 MiB of brk-managed heap
	maxSteps  = 5_000_000
)

type cpu struct {
	regs [16]uint64
	xmm  [16][2]uint64 // 128-bit XMM registers (low/high halves)
	rip  uint64
	img  *image
	// writable copy of the image
	mem   []byte
	stack []byte
	heap  []byte
	brk   uint64 // current program break
	// flags
	zf, sf, cf, of bool
	// state
	exited  bool
	code    int
	steps   int
	verbose bool
	// trace: ring buffer of recently executed addresses
	trace    [32]uint64
	traceIdx int
	traceCnt int
}

var regNames = [16]string{
	"rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi",
	"r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15",
}

func newCPU(img *image, verbose bool) *cpu {
	c := &cpu{img: img, verbose: verbose}
	c.mem = append([]byte(nil), img.data...)
	c.stack = make([]byte, stackSize)
	c.heap = make([]byte, heapSize)
	c.brk = heapBase
	c.rip = img.entry
	c.regs[4] = stackBase + stackSize - 8 // rsp
	// Minimal initial stack: argc=1, argv[0]=NULL, envp=NULL, auxv end.
	le := binary.LittleEndian
	le.PutUint64(c.stack[stackSize-8:], 1)
	return c
}

func (c *cpu) slice(addr uint64, n int) []byte {
	if addr >= c.img.base && addr+uint64(n) <= c.img.base+uint64(len(c.mem)) {
		off := addr - c.img.base
		return c.mem[off : off+uint64(n)]
	}
	if addr >= stackBase && addr+uint64(n) <= stackBase+stackSize {
		off := addr - stackBase
		return c.stack[off : off+uint64(n)]
	}
	if addr >= heapBase && addr+uint64(n) <= heapBase+heapSize {
		off := addr - heapBase
		return c.heap[off : off+uint64(n)]
	}
	return nil
}

func (c *cpu) readMem(addr uint64, n int) (uint64, bool) {
	s := c.slice(addr, n)
	if s == nil {
		return 0, false
	}
	switch n {
	case 1:
		return uint64(s[0]), true
	case 2:
		return uint64(binary.LittleEndian.Uint16(s)), true
	case 4:
		return uint64(binary.LittleEndian.Uint32(s)), true
	case 8:
		return binary.LittleEndian.Uint64(s), true
	}
	return 0, false
}

func (c *cpu) writeMem(addr uint64, n int, v uint64) bool {
	s := c.slice(addr, n)
	if s == nil {
		return false
	}
	switch n {
	case 1:
		s[0] = byte(v)
	case 2:
		binary.LittleEndian.PutUint16(s, uint16(v))
	case 4:
		binary.LittleEndian.PutUint32(s, uint32(v))
	case 8:
		binary.LittleEndian.PutUint64(s, v)
	default:
		return false
	}
	return true
}

func (c *cpu) fetch8() byte {
	s := c.slice(c.rip, 1)
	if s == nil {
		panic(fmt.Sprintf("fetch past memory at 0x%x", c.rip))
	}
	c.rip++
	return s[0]
}

func (c *cpu) fetch32() uint32 {
	s := c.slice(c.rip, 4)
	if s == nil {
		panic(fmt.Sprintf("fetch past memory at 0x%x", c.rip))
	}
	c.rip += 4
	return binary.LittleEndian.Uint32(s)
}

func (c *cpu) fetch64() uint64 {
	s := c.slice(c.rip, 8)
	if s == nil {
		panic(fmt.Sprintf("fetch past memory at 0x%x", c.rip))
	}
	c.rip += 8
	return binary.LittleEndian.Uint64(s)
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "elfcheck: "+format+"\n", a...)
	os.Exit(1)
}

func (c *cpu) dumpRegs() {
	fmt.Fprintf(os.Stderr, "  rip=0x%x\n", c.rip)
	for i := 0; i < 16; i++ {
		fmt.Fprintf(os.Stderr, "  %-3s=0x%x", regNames[i], c.regs[i])
		if i%4 == 3 {
			fmt.Fprintln(os.Stderr)
		}
	}
	fmt.Fprintf(os.Stderr, "  last instructions:")
	start := c.traceIdx - c.traceCnt
	if start < 0 {
		start += len(c.trace)
	}
	for i := 0; i < c.traceCnt; i++ {
		fmt.Fprintf(os.Stderr, " 0x%x", c.trace[(start+i)%len(c.trace)])
	}
	fmt.Fprintln(os.Stderr)
}

// run interprets until exit or step limit. Returns the exit status.
func (c *cpu) run() (code int) {
	// An unsupported instruction must fail loudly, not silently mis-execute.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "elfcheck: execution fault:", r)
			os.Exit(1)
		}
	}()

	for !c.exited {
		if c.steps > maxSteps {
			die("step limit reached (%d) -- infinite loop?", maxSteps)
		}
		c.steps++
		c.step()
	}
	return c.code
}

func (c *cpu) step() {
	pc := c.rip
	c.trace[c.traceIdx] = pc
	c.traceIdx = (c.traceIdx + 1) % len(c.trace)
	if c.traceCnt < len(c.trace) {
		c.traceCnt++
	}
	op := c.fetch8()

	// Legacy prefixes: 0x66 / 0xF2 / 0xF3. In goa's output these are always
	// the mandatory prefixes of the SSE2 instructions (there are no REP or
	// operand-size-override forms -- goclib has no string ops), so we remember
	// the last one and only apply it inside the 0F two-byte opcode switch.
	legacy := byte(0)
	for op == 0x66 || op == 0xf2 || op == 0xf3 {
		legacy = op
		op = c.fetch8()
	}

	// REX prefix (only REX.W / .R / .B matter for what goa emits).
	rex := byte(0)
	if op >= 0x40 && op <= 0x4f {
		rex = op
		op = c.fetch8()
	}
	w := rex&0x08 != 0
	size := 4
	if w {
		size = 8
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "  %08x  %02x\n", pc, op)
	}

	// modrm helper: returns register indices and, for memory operands, the
	// effective address (or 0 with isMem=false for register-direct).
	type rmOperand struct {
		isMem bool
		addr  uint64
		reg   int
	}
	modrm := func() (int, rmOperand) {
		m := c.fetch8()
		mod := m >> 6
		reg := int((m>>3)&7) + int((rex>>2)&1)*8
		rmRaw := int(m & 7)
		if mod == 3 {
			return reg, rmOperand{isMem: false, reg: rmRaw + int(rex&1)*8}
		}
		// Memory forms goa emits: [rip+disp32], [base+disp8/32], and
		// [base+index*scale+disp] via SIB.
		//
		// Subtlety: when rm==4 signals a SIB byte, REX.B extends the SIB's
		// base field, NOT the rm field. (Adding it to rm turns 4 into 12 and
		// silently skips the SIB.)
		var addr uint64
		switch {
		case rmRaw == 4:
			// SIB: [base + index*scale + disp]
			sib := c.fetch8()
			scale := (sib >> 6) & 3
			// index field 100 with no REX.X means "no index" (rsp cannot be
			// an index); otherwise REX.X extends it.
			idxField := int((sib >> 3) & 7)
			hasIdx := !(idxField == 4 && (rex>>1)&1 == 0)
			idx := idxField + int((rex>>1)&1)*8
			bs := int(sib&7) + int(rex&1)*8
			if (sib&7) == 5 && mod == 0 {
				addr = uint64(int64(int32(c.fetch32())))
			} else {
				addr = c.regs[bs]
			}
			if hasIdx {
				addr += c.regs[idx] << scale
			}
			switch mod {
			case 1:
				addr += uint64(int64(int8(c.fetch8())))
			case 2:
				addr += uint64(int64(int32(c.fetch32())))
			}
		case rmRaw == 5 && mod == 0:
			disp := int64(int32(c.fetch32()))
			addr = c.rip + uint64(disp)
		default:
			addr = c.regs[rmRaw+int(rex&1)*8]
			switch mod {
			case 1:
				addr += uint64(int64(int8(c.fetch8())))
			case 2:
				addr += uint64(int64(int32(c.fetch32())))
			}
		}
		return reg, rmOperand{isMem: true, addr: addr}
	}

	getRM := func(o rmOperand) uint64 {
		if !o.isMem {
			return c.regs[o.reg]
		}
		v, ok := c.readMem(o.addr, size)
		if !ok {
			fmt.Fprintf(os.Stderr, "elfcheck: REGDUMP rax=%x rcx=%x rdx=%x rbx=%x rsp=%x rbp=%x rsi=%x rdi=%x r8=%x r9=%x r10=%x r11=%x r12=%x r13=%x r14=%x r15=%x rip=%x\n",
				c.regs[0], c.regs[1], c.regs[2], c.regs[3], c.regs[4], c.regs[5], c.regs[6], c.regs[7], c.regs[8], c.regs[9], c.regs[10], c.regs[11], c.regs[12], c.regs[13], c.regs[14], c.regs[15], c.rip)
			die("read of unmapped memory 0x%x at 0x%x", o.addr, pc)
		}
		return v
	}
	setRM := func(o rmOperand, v uint64) {
		if !o.isMem {
			c.regs[o.reg] = v
			return
		}
		if !c.writeMem(o.addr, size, v) {
			die("write to unmapped memory 0x%x at 0x%x", o.addr, pc)
		}
	}

	switch op {
	case 0x0f:
		op2 := c.fetch8()
		switch {
		case op2 == 0x05 && legacy == 0:
			c.syscall()
		case op2 == 0xaf && legacy == 0: // imul r64, r/m64
			reg, o := modrm()
			c.regs[reg] = uint64(int64(c.regs[reg]) * int64(getRM(o)))
			c.cf, c.of = false, false
		case op2 >= 0x80 && op2 <= 0x8f && legacy == 0:
			rel := int64(int32(c.fetch32()))
			if c.cond(op2) {
				c.rip = c.rip + uint64(rel)
			}
		case legacy == 0xf2 || legacy == 0x66:
			// ---- SSE2 (the scalar double / quadword set goa emits) ----
			// XMM and GP registers share the ModRM field convention: REX.R
			// extends the reg field, REX.B the rm register field. The XMM
			// operand sits in the reg field for every form except cvttsd2si
			// and movq r64,xmm (where the GP operand takes it), exactly as
			// goa encodes them.
			//
			// getSse reads the rm operand as the low 64 bits of an XMM
			// register or 8 bytes of memory (a double).
			getSse := func(o rmOperand) uint64 {
				if !o.isMem {
					return c.xmm[o.reg][0]
				}
				v, ok := c.readMem(o.addr, 8)
				if !ok {
					die("SSE read of unmapped memory 0x%x at 0x%x", o.addr, pc)
				}
				return v
			}
			switch op2 {
			case 0x10, 0x11: // movsd xmm, xmm/m64 / movsd xmm/m64, xmm
				reg, o := modrm()
				if op2 == 0x10 {
					bits := getSse(o)
					if o.isMem {
						c.xmm[reg][1] = 0 // a memory source zeroes the upper half
					}
					c.xmm[reg][0] = bits
				} else {
					bits := c.xmm[reg][0]
					if !o.isMem {
						c.xmm[o.reg][0] = bits
					} else if !c.writeMem(o.addr, 8, bits) {
						die("SSE write to unmapped memory 0x%x at 0x%x", o.addr, pc)
					}
				}
			case 0x58, 0x59, 0x5c, 0x5e, 0x51: // addsd/mulsd/subsd/divsd/sqrtsd
				reg, o := modrm()
				a := c.xmmF(reg)
				b := math.Float64frombits(getSse(o))
				switch op2 {
				case 0x58:
					a += b
				case 0x59:
					a *= b
				case 0x5c:
					a -= b
				case 0x5e:
					a /= b
				case 0x51:
					a = math.Sqrt(b)
				}
				c.xmm[reg][0] = math.Float64bits(a)
			case 0x2a: // cvtsi2sd xmm, r/m64 (REX.W required)
				if !w {
					die("unsupported cvtsi2sd without REX.W at 0x%x", pc)
				}
				reg, o := modrm()
				var raw uint64
				if o.isMem {
					raw = getSse(o)
				} else {
					raw = c.regs[o.reg] // rm field holds the GP source
				}
				c.xmm[reg][0] = math.Float64bits(float64(int64(raw)))
			case 0x2c: // cvttsd2si r64, xmm/m64 (REX.W required); GP dst in reg
				if !w {
					die("unsupported cvttsd2si without REX.W at 0x%x", pc)
				}
				reg, o := modrm()
				c.regs[reg] = uint64(int64(math.Float64frombits(getSse(o))))
			case 0x57: // xorpd xmm, xmm/m128
				reg, o := modrm()
				var lo, hi uint64
				if !o.isMem {
					lo, hi = c.xmm[o.reg][0], c.xmm[o.reg][1]
				} else {
					s := c.slice(o.addr, 16)
					if s == nil {
						die("SSE read of unmapped memory 0x%x at 0x%x", o.addr, pc)
					}
					lo, hi = binary.LittleEndian.Uint64(s), binary.LittleEndian.Uint64(s[8:])
				}
				c.xmm[reg][0] ^= lo
				c.xmm[reg][1] ^= hi
			case 0x2e: // ucomisd xmm, xmm/m64
				reg, o := modrm()
				a := c.xmmF(reg)
				b := math.Float64frombits(getSse(o))
				// ZF=equal, CF=below, OF/SF/AF clear. PF (unordered/NaN) is
				// not tracked -- goa never compares NaN.
				c.zf = a == b
				c.cf = a < b
				c.sf, c.of = false, false
			case 0x6e: // movq xmm, r/m64 (66 REX.W 0F 6E)
				if !w {
					die("unsupported 66 0F 6E without REX.W (movd) at 0x%x", pc)
				}
				reg, o := modrm()
				var raw uint64
				if o.isMem {
					raw = getSse(o)
				} else {
					raw = c.regs[o.reg] // rm field holds the GP source
				}
				c.xmm[reg][0], c.xmm[reg][1] = raw, 0
			case 0x7e: // movq r/m64, xmm (66 REX.W 0F 7E); XMM src in reg field
				if !w {
					die("unsupported 66 0F 7E without REX.W (movd) at 0x%x", pc)
				}
				reg, o := modrm()
				bits := c.xmm[reg][0]
				if !o.isMem {
					c.regs[o.reg] = bits
				} else if !c.writeMem(o.addr, 8, bits) {
					die("SSE write to unmapped memory 0x%x at 0x%x", o.addr, pc)
				}
			default:
				die("unsupported opcode %02x 0F %02x at 0x%x", legacy, op2, pc)
			}
		default:
			die("unsupported opcode 0F %02x at 0x%x", op2, pc)
		}
		return

	case 0xc3: // ret
		v, ok := c.readMem(c.regs[4], 8)
		if !ok {
			c.dumpRegs()
			die("ret with unmapped stack at 0x%x (rsp=0x%x)", pc, c.regs[4])
		}
		c.regs[4] += 8
		c.rip = v
		return

	case 0xe8: // call rel32
		rel := int64(int32(c.fetch32()))
		next := c.rip
		c.regs[4] -= 8
		if !c.writeMem(c.regs[4], 8, next) {
			c.dumpRegs()
			die("call with unmapped stack at 0x%x (rsp=0x%x)", pc, c.regs[4])
		}
		c.rip = next + uint64(rel)
		return

	case 0xe9: // jmp rel32
		rel := int64(int32(c.fetch32()))
		c.rip = c.rip + uint64(rel)
		return

	}

	// push/pop with REX.B (r8-r15)
	if rex&0x01 != 0 {
		if op >= 0x50 && op <= 0x57 {
			r := 8 + int(op-0x50)
			c.regs[4] -= 8
			if !c.writeMem(c.regs[4], 8, c.regs[r]) {
				die("push with unmapped stack at 0x%x", pc)
			}
			return
		}
		if op >= 0x58 && op <= 0x5f {
			r := 8 + int(op-0x58)
			v, ok := c.readMem(c.regs[4], 8)
			if !ok {
				die("pop with unmapped stack at 0x%x", pc)
			}
			c.regs[4] += 8
			c.regs[r] = v
			return
		}
	}

	switch {
	case op >= 0x50 && op <= 0x57: // push r64
		r := int(op - 0x50)
		c.regs[4] -= 8
		if !c.writeMem(c.regs[4], 8, c.regs[r]) {
			die("push with unmapped stack at 0x%x", pc)
		}
		return
	case op >= 0x58 && op <= 0x5f: // pop r64
		r := int(op - 0x58)
		v, ok := c.readMem(c.regs[4], 8)
		if !ok {
			die("pop with unmapped stack at 0x%x", pc)
		}
		c.regs[4] += 8
		c.regs[r] = v
		return
	case op >= 0xb8 && op <= 0xbf: // mov r64, imm64 (REX.W) / imm32
		r := int(op-0xb8) + int(rex&1)*8
		if w {
			c.regs[r] = c.fetch64()
		} else {
			c.regs[r] = uint64(c.fetch32())
		}
		return
	}

	switch op {
	case 0x8d: // lea r64, [rip+disp32] or [base+disp]
		reg, o := modrm()
		if !o.isMem {
			die("lea with register operand at 0x%x", pc)
		}
		c.regs[reg] = o.addr
		return

	case 0x89: // mov r/m64, r64
		reg, o := modrm()
		setRM(o, c.regs[reg])
		return

	case 0x8b: // mov r64, r/m64
		reg, o := modrm()
		c.regs[reg] = getRM(o)
		return

	case 0x88: // mov r/m8, r8
		size = 1
		reg, o := modrm()
		if !o.isMem {
			c.regs[o.reg] = (c.regs[o.reg] &^ 0xff) | (c.regs[reg] & 0xff)
			return
		}
		if !c.writeMem(o.addr, 1, c.regs[reg]&0xff) {
			die("byte write to unmapped memory 0x%x at 0x%x", o.addr, pc)
		}
		return

	case 0x8a: // mov r8, r/m8
		size = 1
		reg, o := modrm()
		v := getRM(o) & 0xff
		c.regs[reg] = (c.regs[reg] &^ 0xff) | v
		return

	case 0xc7: // mov r/m64, imm32 (sign-extended)
		reg, o := modrm()
		if reg != 0 {
			die("unsupported C7 /%d at 0x%x", reg, pc)
		}
		v := uint64(int64(int32(c.fetch32())))
		if !w {
			v &= 0xffffffff
		}
		setRM(o, v)
		return

	case 0xc6: // mov r/m8, imm8
		reg, o := modrm()
		if reg != 0 {
			die("unsupported C6 /%d at 0x%x", reg, pc)
		}
		v := uint64(c.fetch8())
		if !c.writeMem(o.addr, 1, v) {
			die("byte write to unmapped memory 0x%x at 0x%x", o.addr, pc)
		}
		return

	case 0x31: // xor r/m, r
		reg, o := modrm()
		v := getRM(o) ^ c.regs[reg]
		setRM(o, v)
		c.zf, c.sf = v == 0, false
		c.cf, c.of = false, false
		return

	case 0x09, 0x21: // or / and r/m64, r64  (RAX is r/m, the source operand)
		reg, o := modrm()
		var v uint64
		if op == 0x09 {
			v = getRM(o) | c.regs[reg]
		} else {
			v = getRM(o) & c.regs[reg]
		}
		setRM(o, v)
		c.setLogicFlags(v)
		return

	case 0x0b, 0x23: // or / and r64, r/m64  (the GP register is the destination)
		reg, o := modrm()
		var v uint64
		if op == 0x0b {
			v = c.regs[reg] | getRM(o)
		} else {
			v = c.regs[reg] & getRM(o)
		}
		c.regs[reg] = v
		c.setLogicFlags(v)
		return

	case 0x01: // add r/m, r
		reg, o := modrm()
		a, b := getRM(o), c.regs[reg]
		v := a + b
		setRM(o, v)
		c.setFlagsAdd(a, b, v)
		return

	case 0x29: // sub r/m, r
		reg, o := modrm()
		a, b := getRM(o), c.regs[reg]
		v := a - b
		setRM(o, v)
		c.setFlagsSub(a, b, v)
		return

	case 0x39: // cmp r/m, r
		reg, o := modrm()
		a, b := getRM(o), c.regs[reg]
		c.setFlagsSub(a, b, a-b)
		return

	case 0x69: // imul r64, r/m64, imm32   (goa's "imul reg, imm" two-operand form)
		reg, o := modrm()
		imm := uint64(int64(int32(c.fetch32())))
		c.regs[reg] = uint64(int64(getRM(o)) * int64(imm))
		c.cf, c.of = false, false
		return

	case 0x6b: // imul r64, r/m64, imm8    (sign-extended immediate)
		reg, o := modrm()
		imm := uint64(int64(int8(c.fetch8())))
		c.regs[reg] = uint64(int64(getRM(o)) * int64(imm))
		c.cf, c.of = false, false
		return

	case 0xc1: // shift r/m64 by imm8: /4 shl, /5 shr, /7 sar (REX.W => 64-bit)
		shiftReg, o := modrm()
		cnt := c.fetch8()
		v := getRM(o)
		switch shiftReg {
		case 4: // shl / sal
			v <<= cnt
		case 5: // shr (logical, zero-fill)
			v >>= cnt
		case 7: // sar (arithmetic, sign-fill)
			v = uint64(int64(v) >> cnt)
		default:
			die("unsupported shift /%d at 0x%x", shiftReg, pc)
		}
		setRM(o, v)
		return

	case 0xd1: // shift r/m64 by 1: /4 shl, /5 shr, /7 sar
		shiftReg, o := modrm()
		v := getRM(o)
		switch shiftReg {
		case 4:
			v <<= 1
		case 5:
			v >>= 1
		case 7:
			v = uint64(int64(v) >> 1)
		default:
			die("unsupported shift /%d at 0x%x", shiftReg, pc)
		}
		setRM(o, v)
		return

	case 0xd3: // shift r/m64 by cl (rcx): /4 shl, /5 shr, /7 sar (REX.W => 64-bit)
		shiftReg, o := modrm()
		cnt := c.regs[1] & 0x3f // low 6 bits of rcx, as real hardware masks it
		v := getRM(o)
		switch shiftReg {
		case 4: // shl / sal
			v <<= cnt
		case 5: // shr (logical, zero-fill)
			v >>= cnt
		case 7: // sar (arithmetic, sign-fill)
			v = uint64(int64(v) >> cnt)
		default:
			die("unsupported shift /%d at 0x%x", shiftReg, pc)
		}
		setRM(o, v)
		return

	case 0x81, 0x83: // add/or/and/sub/xor/cmp r/m64, imm8 or imm32
		reg, o := modrm()
		var imm uint64
		if op == 0x83 {
			imm = uint64(int64(int8(c.fetch8())))
		} else {
			imm = uint64(int64(int32(c.fetch32())))
		}
		if !w {
			imm &= 0xffffffff
		}
		switch reg {
		case 0: // add
			a := getRM(o)
			v := a + imm
			setRM(o, v)
			c.setFlagsAdd(a, imm, v)
		case 1: // or
			a := getRM(o)
			v := a | imm
			setRM(o, v)
			c.setLogicFlags(v)
		case 4: // and
			a := getRM(o)
			v := a & imm
			setRM(o, v)
			c.setLogicFlags(v)
		case 5: // sub
			a := getRM(o)
			v := a - imm
			setRM(o, v)
			c.setFlagsSub(a, imm, v)
		case 6: // xor
			a := getRM(o)
			v := a ^ imm
			setRM(o, v)
			c.setLogicFlags(v)
		case 7: // cmp
			a := getRM(o)
			c.setFlagsSub(a, imm, a-imm)
		default:
			die("unsupported 83 /%d at 0x%x", reg, pc)
		}
		return

	case 0xff: // inc/dec/call/jmp r/m64
		reg, o := modrm()
		switch reg {
		case 0:
			v := getRM(o) + 1
			setRM(o, v)
			c.zf, c.sf = v == 0, v>>63 != 0
			c.of = getRM(o) == 0
		case 1:
			v := getRM(o) - 1
			setRM(o, v)
			c.zf, c.sf = v == 0, v>>63 != 0
		default:
			die("unsupported FF /%d at 0x%x", reg, pc)
		}
		return

	case 0xf7: // idiv/div/neg/mul r/m64
		reg, o := modrm()
		v := getRM(o)
		switch reg {
		case 3: // neg
			setRM(o, -v)
			c.cf = v != 0
			c.zf, c.sf = -v == 0, (-v)>>63 != 0
		case 6, 7: // div / idiv -- signed unless /6
			divisor := v
			if divisor == 0 {
				die("divide by zero at 0x%x", pc)
			}
			var quo, rem uint64
			if reg == 7 {
				a := int64(c.regs[0])
				d := int64(c.regs[2])
				n := (int128(a, d))
				q := n / int64(divisor)
				r := n % int64(divisor)
				quo, rem = uint64(q), uint64(r)
			} else {
				a := uint128(c.regs[0], c.regs[2])
				quo = uint64(a / uint64(divisor))
				rem = uint64(a % uint64(divisor))
			}
			c.regs[0], c.regs[2] = quo, rem
		default:
			die("unsupported F7 /%d at 0x%x", reg, pc)
		}
		return

	case 0x99: // cqo: sign-extend rax into rdx
		if c.regs[0]>>63 != 0 {
			c.regs[2] = ^uint64(0)
		} else {
			c.regs[2] = 0
		}
		return

	case 0x90: // nop
		return

	case 0xeb: // jmp rel8
		rel := int64(int8(c.fetch8()))
		c.rip += uint64(rel)
		return
	}

	die("unsupported opcode %02x at 0x%x (rex=%02x)", op, pc, rex)
}

// int128 / uint128 model the RDX:RAX dividend of a 128-bit divide. We only
// need values that fit, which is every case goa-generated code produces.
func int128(lo, hi int64) int64 {
	// Callers only use this when hi is a sign extension of lo.
	if hi == 0 || hi == -1 {
		return lo
	}
	die("idiv with a dividend wider than 64 bits is not supported")
	return 0
}

func uint128(lo, hi uint64) uint64 {
	if hi != 0 {
		die("div with a dividend wider than 64 bits is not supported")
	}
	return lo
}

// xmmF reads the low 64 bits of an XMM register as a double.
func (c *cpu) xmmF(i int) float64 {
	return math.Float64frombits(c.xmm[i][0])
}

func (c *cpu) setFlagsAdd(a, b, v uint64) {
	c.cf = v < a
	c.zf = v == 0
	c.sf = v>>63 != 0
	c.of = (a>>63 == b>>63) && (v>>63 != a>>63)
}

// setLogicFlags is the flag result of and/or/xor: CF and OF clear, ZF/SF
// from the result.
func (c *cpu) setLogicFlags(v uint64) {
	c.cf, c.of = false, false
	c.zf = v == 0
	c.sf = v>>63 != 0
}

func (c *cpu) setFlagsSub(a, b, v uint64) {
	c.cf = a < b
	c.zf = v == 0
	c.sf = v>>63 != 0
	c.of = (a>>63 != b>>63) && (v>>63 != a>>63)
}

func (c *cpu) cond(op byte) bool {
	switch op {
	case 0x84:
		return c.zf
	case 0x85:
		return !c.zf
	case 0x8c:
		return c.sf != c.of
	case 0x8d:
		return c.sf == c.of
	case 0x8e:
		return c.zf || c.sf != c.of
	case 0x8f:
		return !c.zf && c.sf == c.of
	case 0x82:
		return c.cf
	case 0x83:
		return !c.cf
	case 0x86:
		return c.cf || c.zf
	case 0x87:
		return !c.cf && !c.zf
	}
	die("unsupported conditional jump 0F %02x", op)
	return false
}

func (c *cpu) syscall() {
	num := c.regs[0]
	switch num {
	case 1: // write(fd, buf, count)
		fd, buf, n := c.regs[7], c.regs[6], c.regs[2]
		s := c.slice(buf, int(n))
		if s == nil {
			die("write(1, 0x%x, %d): buffer is not mapped", buf, n)
		}
		if c.verbose {
			fmt.Fprintf(os.Stderr, "  write(fd=%d, buf=0x%x, n=%d)\n", fd, buf, n)
		}
		if fd != 1 && fd != 2 {
			fmt.Fprintf(os.Stderr, "elfcheck: write to fd %d ignored\n", fd)
			c.regs[0] = uint64(n)
			return
		}
		os.Stdout.Write(s)
		c.regs[0] = uint64(n)
	case 60, 231: // exit / exit_group
		c.exited = true
		c.code = int(c.regs[7] & 0xff)
	case 12: // brk: set the program break, return the new one (or the old
		// one on failure, which is how malloc detects it).
		req := c.regs[7]
		if req != 0 && req >= heapBase && req <= heapBase+heapSize {
			c.brk = req
		}
		c.regs[0] = c.brk
	default:
		fmt.Fprintf(os.Stderr, "elfcheck: unimplemented syscall %d -- returning ENOSYS\n", num)
		c.regs[0] = ^uint64(38) + 1 // -ENOSYS as a 64-bit two's complement value
	}
	_ = regNames
}
