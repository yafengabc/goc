#include <stdio.h>

/* C23 __VA_OPT__: expands to its argument-list only when __VA_ARGS__ is
 * non-empty. The classic use is a trailing comma that must appear iff at
 * least one variadic argument was supplied. */
#define LOG(fmt, ...)   printf(fmt __VA_OPT__(, __VA_ARGS__))
#define WRAP(fn, ...)   fn(1 __VA_OPT__(, __VA_ARGS__))
#define MAKE(fn, ...)   fn(__VA_ARGS__ __VA_OPT__(, 99))

void foo(int x)        { printf("foo(%d)\n", x); }
void bar(int x, int y) { printf("bar(%d,%d)\n", x, y); }
void baz(int x, int y) { printf("baz(%d,%d)\n", x, y); }

int main(void) {
    LOG("hello\n");              /* no variadic arg -> no comma */
    LOG("sum=%d\n", 1 + 2);     /* variadic arg present -> comma inserted */
    WRAP(foo);                  /* -> foo(1) */
    WRAP(bar, 7);               /* -> bar(1, 7) */
    MAKE(baz, 5);               /* -> baz(5, 99) */
    return 0;
}
