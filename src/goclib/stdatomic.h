#ifndef GOC_STDATOMIC_H
#define GOC_STDATOMIC_H
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
 * Not provided yet: atomic_fetch_add and the rest of the fetch_* family,
 * atomic_exchange and atomic_compare_exchange_*. They each need either a
 * compiler builtin or inline asm, and goclib is deliberately pure C (the
 * LLVM backend cannot translate inline asm). They are the next step.
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

#endif /* GOC_STDATOMIC_H */
