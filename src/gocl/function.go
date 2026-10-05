package gocl

// The LLVM IR back end: a pure AST -> IR translator with no code-generator
// coupling.
//
// The type analysis, name resolution and scope handling are NOT reimplemented
// here. They live in typeResolver (src/llvmtype.go), which both this translator
// and the assembly generator draw on, so the two back ends never disagree about
// a program's meaning. The translator holds its own typeResolver (cg == nil)
// and changes only where the bytes go: instead of appending to the instruction
// stream that becomes assembly, it appends IR text.
//
// The result is that a function can move between the two back ends without
// either one having a different idea of what the program means.

import (
	"fmt"
	"goc/frontend"
	"strings"
)

// irEmitter renders one function as LLVM IR. It holds a *CG so the existing
// scope and type machinery is available; the ir* fields are the IR-side state.
type irEmitter struct {
	c  *irMod        // the module being written
	tr *typeResolver // the shared type/name resolver (no CG coupling)

	fname string
	retTy string // LLVM type of the return value
	// paramNames is the parameter list of the function being written. A va_list
	// is a char*, so `va_arg(ap, T)` needs the va_list itself when `ap` arrived
	// as a parameter -- it is already the pointer -- and the address of the
	// local when `ap` is a local `va_list ap;`. The two are told apart by which
	// list the name is in.
	paramNames map[string]bool
	// vaSlots maps a va_list variable's name to the 24-byte slot its va_start,
	// va_arg and va_end all share, so the front end's `char *` typedef and
	// llvm.va_start's structure agree on one object.
	vaSlots map[string]string
	entry   strings.Builder
	body    strings.Builder
	tmp     int
	lab     int
	closed  bool // the current block already has its terminator
	// slots maps a local's uid to its alloca, because the generator tracks
	// locals by uid rather than by name.
	slots map[int]string
	// breakTo and continueTo hold the enclosing loop's labels, innermost last.
	breakTo    []string
	continueTo []string
	// userLabels maps a C goto label to the LLVM block carrying it.
	userLabels map[string]string
	// pendingLabels are C labels waiting for the block that follows.
	pendingLabels []string
	// lastLabel is the most recently opened block, which is where a pending
	// C label or a pending goto lands.
	lastLabel string
	// scratch is a reusable alloca for the conversions that have to move a
	// value through memory.
	scratch string
	// forwards records branches to a C label that had not been seen yet.
	forwards []forwardGoto
}

// forwardGoto is one branch written before its target label existed.
type forwardGoto struct {
	label       string
	at          int    // where the branch text starts in body
	placeholder string // the block name used until the target is known
}

// --- emission primitives ----------------------------------------------------

func (e *irEmitter) newTmp() string {
	e.tmp++
	return "%t" + itoa(e.tmp)
}

func (e *irEmitter) newLabel() string {
	e.lab++
	return "L" + itoa(e.lab)
}

// line appends one instruction, opening a block first if the previous one was
// terminated. LLVM requires every basic block to end in exactly one terminator,
// and a label may only be the first thing in a block; this is where that
// invariant is maintained.
func (e *irEmitter) line(format string, args ...interface{}) {
	if e.closed {
		l := e.newLabel()
		e.body.WriteString(l + ":\n")
		e.lastLabel = l
		e.closed = false
	}
	s := format
	if len(args) > 0 {
		s = fmt.Sprintf(format, args...)
	}
	e.body.WriteString("  " + s + "\n")
}

func (e *irEmitter) term(format string, args ...interface{}) {
	e.line(format, args...)
	e.closed = true
}

// blockLabel starts a fresh block, attaching any C labels that were waiting for
// this point. A C label sits in front of a statement, and the block that follows
// it is exactly where control must land, so the association is recorded here.
func (e *irEmitter) blockLabel(l string) {
	e.body.WriteString(l + ":\n")
	e.lastLabel = l
	e.closed = false
	for _, cl := range e.pendingLabels {
		e.userLabels[cl] = l
	}
	e.pendingLabels = nil
}

func (e *irEmitter) ty(t *frontend.Type) string { return e.c.llirType(t) }

// --- function ---------------------------------------------------------------

// genIRFunc renders one C function as an LLVM IR definition.
func genIRFunc(tr *typeResolver, m *irMod, f *frontend.FuncDecl) (string, error) {
	e := &irEmitter{
		c:          m,
		tr:         tr,
		fname:      f.Name,
		retTy:      m.llirType(f.Ret),
		slots:      map[int]string{},
		userLabels: map[string]string{},
	}
	// Reset the per-function emission state the way genFunc does, so name
	// resolution and type answers are the same on the IR path.
	tr.resetScope()
	tr.pushScope()

	paramNames := map[string]bool{}
	for _, pn := range f.Params {
		paramNames[pn] = true
	}
	e.paramNames = paramNames

	params := e.bindParams(f)

	e.stmt(f.Body)
	if !e.closed {
		// Falling off the end of a function returns zero, as C requires. The
		// spelling depends on the return type: a pointer's zero is the null
		// pointer, and "ret ptr 0" is rejected by LLVM ("integer/byte constant
		// must have integer/byte type").
		switch e.retTy {
		case "void":
			e.term("ret void")
		case "float":
			e.term("ret float 0.0")
		case "double":
			e.term("ret double 0.0")
		case "ptr":
			e.term("ret ptr null")
		default:
			e.term("ret %s 0", e.retTy)
		}
	}
	tr.popScope()

	var b strings.Builder
	sig := "define " + e.retTy + " @" + f.Name + "(" + strings.Join(params, ", ")
	if f.Variadic {
		// The ellipsis is what makes this variadic to LLVM, and it has to come
		// after every named parameter. A function that takes a fixed count is not
		// variadic no matter what its body does: the caller would not set up the
		// register save area, and va_start would read whatever the registers
		// happened to hold.
		sig += ", ...)"
	} else {
		sig += ")"
	}
	b.WriteString(sig + " {\n")
	b.WriteString("entry:\n")
	b.WriteString(e.entry.String())
	b.WriteString(e.body.String())
	b.WriteString("}\n")
	return b.String(), nil
}

// bindParams turns the C parameters into LLVM block arguments and local slots.
// A parameter also gets an alloca because C allows its address to be taken;
// LLVM's mem2reg pass removes the slot again when nothing does.
func (e *irEmitter) bindParams(f *frontend.FuncDecl) []string {
	var out []string
	for i, pt := range f.ParamTypes {
		// A parameter declared as an array is a pointer: the caller passes the
		// address, and treating the slot as the whole array would load it as a
		// value and pass an [N x i32] where a pointer belongs.
		if pt != nil && pt.Kind == frontend.KArr {
			pt = frontend.PtrType(pt.Elem)
		}
		ty := e.ty(pt)
		name := "%p" + itoa(i)
		out = append(out, ty+" "+name)
		if i < len(f.Params) {
			uid := e.tr.declareVar(f.Params[i], varInfo{ty: pt})
			slot := e.newTmp()
			e.entry.WriteString("  " + slot + " = alloca " + ty + ", align " +
				itoa(alignOfIr(ty)) + "\n")
			e.slots[uid] = slot
			e.line("store %s %s, ptr %s, align %d", ty, name, slot, alignOfIr(ty))
		}
	}
	return out
}

// slotFor returns the alloca backing a local, creating it on first use. A
// variable declared inside a loop body is still one object for the whole
// activation, so the slot is created once and reused across iterations -- which
// is what distinguishes it from a temporary.
func (e *irEmitter) slotFor(uid int, ty string) string {
	if s, ok := e.slots[uid]; ok {
		return s
	}
	slot := e.newTmp()
	e.entry.WriteString("  " + slot + " = alloca " + ty + ", align " +
		itoa(alignOfIr(ty)) + "\n")
	e.slots[uid] = slot
	return slot
}

// vaListTy is the storage a va_list needs on this target.
//
// goc declares va_list as `char *` (parser.go: typedefs["va_list"] = frontend.PtrType(frontend.CharType())),
// which is eight bytes, and the intrinsic llvm.va_start stores exactly one
// eight-byte pointer into it: the cursor into the caller's register save area.
// The slot is still widened to 24 bytes so the intrinsic can never overwrite
// the three locals that would otherwise follow an eight-byte object, whatever
// a future target's va_start writes. The pointer typedef stays as it is -- it
// is what the rest of the front end reasons about, and passing a va_list
// between functions passes this pointer. Only the storage is widened, which is
// exactly what a real stdarg.h does when __builtin_va_list is an array type.
const vaListTy = "[3 x i64]"

// vaListSlot returns the address of the object backing a va_list named by x.
// Every mention of the same variable -- va_start, each va_arg, va_end -- must
// land on this one slot, so the mapping is kept by name for the duration of the
// function.
//
// A local `va_list ap;` gets a slot of its own, widened to the Windows x64
// va_list layout so the intrinsic never scribbles past the eight bytes its
// `char *` typedef would otherwise give it. A va_list that arrived as a
// parameter is backed by the alloca bindParams created for it, so its address
// is returned too: va_arg advances the cursor by writing back through it, and
// returning a value instead would make every va_arg read the same slot.
func (e *irEmitter) vaListSlot(x frontend.Expr) string {
	id, ok := x.(*frontend.Ident)
	if !ok {
		return e.lvalue(x)
	}
	if e.paramNames[id.Name] {
		if uid, ok := e.tr.lookupUID(id.Name); ok {
			return e.slotFor(uid, "ptr")
		}
		return e.rvalue(x)
	}
	if e.vaSlots == nil {
		e.vaSlots = map[string]string{}
	}
	if s, seen := e.vaSlots[id.Name]; seen {
		return s
	}
	slot := e.newTmp()
	e.entry.WriteString("  " + slot + " = alloca " + vaListTy + ", align 8\n")
	e.vaSlots[id.Name] = slot
	return slot
}

func alignOfIr(ty string) int {
	if len(ty) > 1 && ty[0] == 'i' {
		n := 0
		for i := 1; i < len(ty); i++ {
			if ty[i] < '0' || ty[i] > '9' {
				return 8
			}
			n = n*10 + int(ty[i]-'0')
		}
		if n/8 < 1 {
			return 1
		}
		return n / 8
	}
	// NOTE: "float" is 5 characters and "double" is 6, so the length test has
	// to be >= or these two branches are dead and every float and double ends
	// up with align 1 -- which is why loads and stores of them came out
	// misaligned. (This mirrors alignOfLlir in llvmmod.go; both must agree.)
	if len(ty) >= 5 && ty[:5] == "float" {
		return 4
	}
	if len(ty) >= 6 && ty[:6] == "double" {
		return 8
	}
	if ty == "ptr" {
		return 8
	}
	if len(ty) > 1 && ty[0] == '[' {
		// An array's alignment is its element's.
		if i := strings.IndexByte(ty, ' '); i > 0 {
			return alignOfIr(ty[i+1:])
		}
	}
	return 1
}
