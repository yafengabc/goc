#ifndef GOC_STDATOMIC_H
#define GOC_STDATOMIC_H

/* This header is goc-only and stays inside this guard. It defines the atomics
 * as macros over goc's own builtins (__goc_atomic_compare_exchange,
 * atomic_fetch_add, ...) which only goc's code generator recognises, and it
 * defines them as plain macros rather than the functions C11 calls for. A
 * host compiler has real C11 <stdatomic.h> with real functions, so it defers
 * to that instead -- see the non-goc branch at the bottom of this file. */
#ifdef __goc__

/* goc stdatomic.h -- C11 7.17 atomics, goc subset.
 *
 * goc models an _Atomic scalar as its underlying integer type with the
 * Atomic flag set: the object always lives in memory (never in a register)
 * and every read-modify-write on it -- ++/-- and the compound assignments --
 * compiles to a single LOCK-prefixed instruction (LOCK XADD for a fetch-add,
 * a LOCK CMPXCHG retry loop for the operators with no atomic instruction).
 *
 * A plain load or store of an aligned scalar is already a single instruction
 * on x86-64, so atomic_load/atomic_store are ordinary reads and writes; the
 * memory_order argument is accepted and ignored (there is no reordering
 * barrier to emit -- this is a single-threaded code model with atomic RMW).
 *
 * The fetch_* family, atomic_exchange and the compare-exchange forms are
 * compiler builtins (see src/frontend/atomic.go): each is a single locked
 * instruction that also reports the value the object held before the update,
 * and no C expression can do both halves indivisibly. Every back end lowers
 * them itself -- the native one to LOCK XADD / XCHG / CMPXCHG, the LLVM one
 * to atomicrmw / cmpxchg. The width of the locked access comes from the
 * object, never from the operand, so one macro serves atomic_char as well as
 * atomic_llong.
 */
#include <stddef.h>
#include <stdint.h>
#include <stdbool.h>

typedef _Atomic bool               atomic_bool;
typedef _Atomic char               atomic_char;
typedef _Atomic unsigned char      atomic_uchar;
typedef _Atomic short              atomic_short;
typedef _Atomic unsigned short     atomic_ushort;
typedef _Atomic int                atomic_int;
typedef _Atomic unsigned int       atomic_uint;
typedef _Atomic long               atomic_long;
typedef _Atomic unsigned long      atomic_ulong;
typedef _Atomic long long          atomic_llong;
typedef _Atomic unsigned long long atomic_ullong;
typedef _Atomic size_t             atomic_size_t;
typedef _Atomic ptrdiff_t          atomic_ptrdiff_t;
typedef _Atomic intptr_t           atomic_intptr_t;
typedef _Atomic uintptr_t          atomic_uintptr_t;

/* The standard's memory order enumeration. Every ordering is accepted; the
 * model is sequentially consistent for the RMW instructions themselves. */
typedef enum memory_order {
    memory_order_relaxed,
    memory_order_consume,
    memory_order_acquire,
    memory_order_release,
    memory_order_acq_rel,
    memory_order_seq_cst
} memory_order;

#define ATOMIC_BOOL_LOCK_FREE   1
#define ATOMIC_CHAR_LOCK_FREE   1
#define ATOMIC_SHORT_LOCK_FREE  1
#define ATOMIC_INT_LOCK_FREE    1
#define ATOMIC_LONG_LOCK_FREE   1
#define ATOMIC_LLONG_LOCK_FREE  1
#define ATOMIC_POINTER_LOCK_FREE 1

#define ATOMIC_VAR_INIT(v) (v)
#define ATOMIC_FLAG_INIT   0

#define atomic_init(p, v)                 ((void)(*(p) = (v)))
#define kill_dependency(x)                (x)
#define atomic_thread_fence(mo)           ((void)0)
#define atomic_signal_fence(mo)           ((void)0)
#define atomic_is_lock_free(p)            1

/* Load and store: an aligned scalar read/write is indivisible already. */
#define atomic_store_explicit(p, v, mo)   ((void)(*(p) = (v)))
#define atomic_store(p, v)                atomic_store_explicit(p, v, memory_order_seq_cst)
#define atomic_load_explicit(p, mo)       (*(p))
#define atomic_load(p)                    atomic_load_explicit(p, memory_order_seq_cst)

/* The fetch family: result is the value the object held BEFORE the update. */
#define atomic_fetch_add(p, v) __goc_atomic_fetch_add(p, v)
#define atomic_fetch_sub(p, v) __goc_atomic_fetch_sub(p, v)
#define atomic_fetch_and(p, v) __goc_atomic_fetch_and(p, v)
#define atomic_fetch_or(p, v)  __goc_atomic_fetch_or(p, v)
#define atomic_fetch_xor(p, v) __goc_atomic_fetch_xor(p, v)
#define atomic_exchange(p, v)  __goc_atomic_exchange(p, v)

#define atomic_fetch_add_explicit(p, v, mo) atomic_fetch_add(p, v)
#define atomic_fetch_sub_explicit(p, v, mo) atomic_fetch_sub(p, v)
#define atomic_fetch_and_explicit(p, v, mo) atomic_fetch_and(p, v)
#define atomic_fetch_or_explicit(p, v, mo)  atomic_fetch_or(p, v)
#define atomic_fetch_xor_explicit(p, v, mo) atomic_fetch_xor(p, v)
#define atomic_exchange_explicit(p, v, mo)  atomic_exchange(p, v)

/* atomic_compare_exchange_strong(object, expected, desired): stores desired
 * into *object when *object equals *expected and reports success; otherwise
 * it writes the value it observed into *expected. goc emits no spurious
 * failure, so the _weak form is the same operation. The _explicit forms take
 * two memory orders (success and failure) and ignore them.
 */
#define atomic_compare_exchange_strong(p, e, d) \
    __goc_atomic_compare_exchange(p, e, d)
#define atomic_compare_exchange_weak(p, e, d) \
    __goc_atomic_compare_exchange(p, e, d)
#define atomic_compare_exchange_strong_explicit(p, e, d, sm, fm) \
    __goc_atomic_compare_exchange(p, e, d)
#define atomic_compare_exchange_weak_explicit(p, e, d, sm, fm) \
    __goc_atomic_compare_exchange(p, e, d)

#else /* !__goc__ */

/* goclib needs only a handful of the C11 atomic operations, and it needs them
 * to work under two quite different host setups: gcc/clang normally, and any
 * host compiler with -nostdinc (which drops the system include directories so
 * that goclib's own headers are provably the only ones in play). So rather than
 * deferring to a host <stdatomic.h> -- which the -nostdinc build cannot see --
 * the operations goclib actually uses are defined here directly, over
 * __atomic_*: compiler builtins that gcc and clang both provide and lower to
 * LOCK XADD / LOCK CMPXCHG, the same instructions goc itself emits. The
 * _Atomic qualifier on the objects (see threads.c's spin lock) is a C11 keyword
 * both hosts accept, so the two hosts agree on object layout as well as on
 * behaviour.
 *
 * The operations are spelled over __atomic_* builtins, whose pointer argument
 * must point to a *non-atomic* type: the C11 generic functions are selected on
 * such a pointer, so an `_Atomic long *' is rejected with "address argument to
 * atomic operation must be a pointer to integer or pointer". That is not a
 * restriction of this header but of the builtins, and it is why the fallback
 * here is not a restatement of C11 <stdatomic.h> -- it is the small set goclib
 * needs, for objects that need no _Atomic declaration of their own. */
#define atomic_load(p)                     (*(p))
#define atomic_store(p, v)                 (*(p) = (v))
#define atomic_exchange(p, v)              __atomic_exchange_n((p), (v), __ATOMIC_SEQ_CST)
#define atomic_fetch_add(p, v)             __atomic_fetch_add((p), (v), __ATOMIC_SEQ_CST)
#define atomic_fetch_sub(p, v)             __atomic_fetch_sub((p), (v), __ATOMIC_SEQ_CST)
#define atomic_fetch_or(p, v)              __atomic_fetch_or((p), (v), __ATOMIC_SEQ_CST)
#define atomic_fetch_xor(p, v)             __atomic_fetch_xor((p), (v), __ATOMIC_SEQ_CST)
#define atomic_compare_exchange_strong(p, e, d) \
    __atomic_compare_exchange_n((p), (e), (d), 0, __ATOMIC_SEQ_CST, __ATOMIC_SEQ_CST)
#define atomic_compare_exchange_weak(p, e, d) \
    __atomic_compare_exchange_n((p), (e), (d), 1, __ATOMIC_SEQ_CST, __ATOMIC_SEQ_CST)
#define atomic_thread_fence(mo)            __atomic_thread_fence(__ATOMIC_SEQ_CST)
#define kill_dependency(y)                 (y)

#endif /* __goc__ */

#endif /* GOC_STDATOMIC_H */
