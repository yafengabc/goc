#ifndef GOC_STDARG_H
#define GOC_STDARG_H

/* This header has two jobs, selected by __goc__ -- the macro goc's own
 * preprocessor predefines (common/preprocess.go) and which no other compiler
 * defines.
 *
 * Under goc, va_list / va_start / va_arg / va_end / va_copy are compiler
 * built-ins: goc recognises them by name in the parser and code generator,
 * mirroring how GCC's <stdarg.h> routes everything through __builtin_va_list.
 * goc wires up va_list itself, so no typedef is emitted here; this header
 * exists so that "#include <stdarg.h>" resolves and documents the contract.
 *
 * Under any other host compiler (gcc, clang, ...), this header is reached only
 * when goclib is being built as an ordinary C library -- see the -nostdinc
 * recipe in README.md -- and the host's own built-ins do the work. Defining the
 * typedef and the four operations here keeps goclib byte-identical between the
 * two hosts.
 */
#ifdef __goc__

/* va_copy (C99 7.15.2.2) is the one variadic operation whose *lowering* differs
 * per ABI, so it is the one that is not a fixed macro:
 *
 *   - Windows x64: va_list is a plain char* cursor, so copying it is a pointer
 *     assignment. Same rule as goc has always used.
 *   - x86-64 SysV (Linux): va_list is `struct __va_list_tag[1]`, a 24-byte
 *     record holding four independent cursors (gp_offset, fp_offset,
 *     overflow_arg_area, reg_save_area). "Copying" it must copy all 24 bytes,
 *     not just the first 8, or the copy keeps sharing -- and overwriting --
 *     the original's register-save pointer. Assigning the first 8 bytes would
 *     leave the copy's overflow_arg_area null and crash on the first stack
 *     argument.
 *
 * On Linux the macro is therefore deliberately NOT defined: the name reaches
 * codegen intact and is lowered to a target-sized block move (goc: an explicit
 * 24-byte copy; gocl: the llvm.va_copy intrinsic, which LLVM lowers per
 * target).
 */
#if !defined(__linux__) && !defined(__linux)
/* Windows x64: a char* cursor, so the copy is a plain pointer assignment. */
#define va_copy(dest, src) ((dest) = (src))
#endif

#else /* !__goc__: gcc / clang / any other host compiler */

/* Every compiler with <stdarg.h> exposes va_list as __builtin_va_list and the
 * four operations as builtins, so the standard mapping is one line each. Using
 * the builtins (rather than, say, glibc's header) is what lets goclib be
 * compiled -nostdinc against nothing but its own headers. */
typedef __builtin_va_list va_list;

#define va_start(ap, last) __builtin_va_start(ap, last)
#define va_arg(ap, type)   __builtin_va_arg(ap, type)
#define va_end(ap)         __builtin_va_end(ap)
#define va_copy(dest, src) __builtin_va_copy(dest, src)

#endif /* __goc__ */

#endif /* GOC_STDARG_H */
