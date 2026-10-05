#ifndef GOC_STDNORETURN_H
#define GOC_STDNORETURN_H

/* goc stdnoreturn.h -- the noreturn macro (C11 7.23).
 *
 * goc treats _Noreturn and noreturn as builtin keywords, so the macro below
 * is the standard spelling that re-expands to the keyword.
 */

#define noreturn _Noreturn

#endif /* GOC_STDNORETURN_H */
