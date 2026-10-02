/* ============================================================
   c99_implicit.c - constructs C99 must reject (6.5.1.1, 6.7.2, 6.9.1)
   Standard   : ISO/IEC 9899:1999 (C99) 6.5.1.1, 6.7.2, 6.9.1
   Strategy   : gcc side (permissive, warnings only after -Wno-error):
                file-scope tentative `x;`, implicit call f(), old-style
                g(){}. goc side: hard parse/codegen rejection.
   Status     : PASS (rejection class) (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
x;
int main(void) { return f(3) + g(); }
int f(int a) { return a + 1; }
g() { return 5; }