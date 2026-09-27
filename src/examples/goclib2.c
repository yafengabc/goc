// Exercises the stage-4 goclib additions: strtol, calloc, strchr, strncmp,
// memcmp, memmove, rand, srand (getchar is covered separately -- it needs
// interactive stdin). Pointers are stored in int variables because goc's C
// subset has no pointer type; all byte-level work happens inside the goclib.
//
// Deterministic output, no stdin, so it is safe for the headless test harness
// on both the Windows and Linux (elfcheck) targets.

int main() {
    // strtol: decimal, negative, and auto-detected hex (base 0).
    printf("strtol dec=%d\n", strtol("12345", 0, 10));
    printf("strtol neg=%d\n", strtol("-67", 0, 10));
    printf("strtol hex=%d\n", strtol("0x1a", 0, 0));

    // calloc returns a usable (zeroed) buffer.
    int c = calloc(8, 1);
    strcpy(c, "hi");
    printf("calloc=%s\n", c);

    // strchr: returns a pointer (non-zero) or 0. 'e' = 101, 'z' = 122.
    int s = "hello";
    printf("strchr hit=%d\n", strchr(s, 101) != 0);
    printf("strchr miss=%d\n", strchr(s, 122) == 0);

    // strncmp: compare only a prefix.
    printf("strncmp eq=%d\n", strncmp("hello", "help", 3) == 0);
    printf("strncmp ne=%d\n", strncmp("hello", "help", 4) != 0);

    // memcmp: compare raw memory.
    printf("memcmp eq=%d\n", memcmp("abc", "abc", 3) == 0);
    printf("memcmp ne=%d\n", memcmp("abc", "abd", 3) != 0);

    // memmove: overlapping copy must come out right (forward, dest < src).
    int b = malloc(16);
    strcpy(b, "abcdefgh");
    memmove(b, b + 2, 4);
    printf("memmove=%s\n", b);

    // rand/srand: deterministic for a fixed seed.
    srand(12345);
    int r1 = rand();
    int r2 = rand();
    srand(12345);
    int r3 = rand();
    printf("rand r1=%d r2=%d r3=%d same=%d\n", r1, r2, r3, r1 == r3);

    printf("done\n");
    return 0;
}
