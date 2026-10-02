/* C23 feature: thread_local / _Thread_local at block scope without static (negative test)
 * Clause:     C23 6.7.1 (_Thread_local at block scope must also carry static or extern)
 * Strategy:   declare non-static _Thread_local objects inside main() using both
 *             spellings and an address-of. A conforming C23 compiler must reject;
 *             gcc -std=c2x does ("function-scope ... implicitly auto and declared
 *             '_Thread_local'"). goc used to Go-panic here (P0.2) and must now
 *             reject cleanly.
 * Status:     PASS (verified 2026-10-02, goc vs gcc -std=c2x)
 * EXPECT: REJECT
 */
int main(void) {
    _Thread_local int a = 5;
    thread_local int b = 6;
    _Thread_local int *p = &a;
    return a + b + *p;
}
