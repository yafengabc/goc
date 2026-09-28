/* asm1.c -- inline assembly (__asm { ... }) end-to-end demo.
 *
 * The block body is assembler text handed to goa almost verbatim, but every
 * bare C variable name is rewritten by goc into the memory operand that
 * addresses it:
 *
 *   parameter / local    -> [rbp+off]
 *   global / static loc  -> [rip+G_x]
 *
 * So "mov eax, a" (a a parameter) becomes "mov eax, [rbp+16]", and
 * "mov g, eax" (g a global) becomes "mov [rip+G_g], eax". A variable inside
 * explicit brackets keeps its bare form: "[x]" -> "[rbp-8]" (never
 * "[ [rbp-8] ]"). This demo exercises all four forms plus lea.
 *
 * NOTE: inside a __asm block only ';' and "//" line comments are legal --
 * goa has no C-style block comments, and the binder only scans to end of
 * line. Keep the block text to one instruction per line.
 */
#include <stdio.h>

int g = 10;

/* read two parameters, write a local, and store into a global, all from
 * inside a single asm block */
int bind_params(int a, int b) {
    int r;
    __asm {
        mov eax, a        // param a  -> [rbp+16]
        add eax, b        // param b  -> [rbp+24]
        mov r, eax        // local r  -> [rbp-8]
        mov g, eax        // global g -> [rip+G_g]
    }
    return r;
}

/* swap two ints through their pointers; the pointer params bind as
 * "[rbp+off]" inside the brackets, and r8 is a scratch caller-save reg */
void ptr_swap(int *x, int *y) {
    __asm {
        mov rax, [x]      // rax = x (the pointer itself)
        mov rcx, [y]      // rcx = y
        mov rdx, [rax]    // rdx = *x
        mov r8,  [rcx]    // r8  = *y
        mov [rax], r8     // *x  = *y
        mov [rcx], rdx    // *y  = *x
    }
}

/* take the address of a local with lea, then read/write through it */
int lea_local(void) {
    int v = 7;
    int *p;
    __asm {
        lea rax, v        // rax = &v -> lea rax, [rbp-8]
        mov p, rax        // p   = &v -> mov [rbp-16], rax
        mov rax, [p]      // rax = p  (the pointer value)
        mov eax, [rax]    // eax = *p
        add eax, 1
        mov v, eax        // v = *p + 1
    }
    return v;
}

int main(void) {
    int a = 3, b = 4;
    printf("sum=%d g=%d\n", bind_params(a, b), g); /* 7, and g=7 */
    a = 1; b = 2;
    ptr_swap(&a, &b);
    printf("swap a=%d b=%d\n", a, b);              /* 2 1 */
    printf("lea v=%d\n", lea_local());             /* 8 */
    return 0;
}
