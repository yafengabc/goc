#ifndef GOC_STDARG_H
#define GOC_STDARG_H

/* goc stdarg.h -- variadic support.
 *
 * va_list is a compiler built-in pointer type, and va_start / va_arg / va_end
 * are compiler built-in operations: goc recognises them by name in the parser
 * and code generator, mirroring how GCC's <stdarg.h> routes everything through
 * __builtin_va_list. goc wires up va_list itself, so no typedef is emitted
 * here; this header exists so that "#include <stdarg.h>" resolves and
 * documents the contract.
 */

#endif /* GOC_STDARG_H */
