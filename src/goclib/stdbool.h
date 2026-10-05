#ifndef GOC_STDBOOL_H
#define GOC_STDBOOL_H

/* goc stdbool.h -- bool/true/false (C99 7.16, C23 7.20).
 *
 * goc treats _Bool, bool, true and false as builtin tokens, so this header
 * only supplies the C99 spellings that include <stdbool.h> users expect.
 * The macros re-expand to the same builtins and are harmless.
 */

#define bool _Bool
#define true 1
#define false 0
#define __bool_true_false_are_defined 1

#endif /* GOC_STDBOOL_H */
