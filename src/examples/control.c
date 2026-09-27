// Exercises the control-flow statements end to end:
//
//   switch/case/default -- fall-through, break, a default in the middle of the
//   body, no matching case at all, a switch nested in another switch, a switch
//   inside a loop (break leaves the switch, continue keeps looping), case
//   labels on chars and enumerators, and proof that the controlling expression
//   is evaluated exactly once.
//
//   do-while -- the body always runs once, continue jumps to the condition
//   (not back to the body), break exits.
//
//   goto / labels -- backward branch forming a loop, and a forward branch that
//   skips a statement.
#include <stdio.h>

enum Color { RED, GREEN, BLUE };

int side; // counts evaluations of a switch's controlling expression

int bump(int n) {
    side = side + 1;
    return n;
}

int classify(int v) {
    switch (v) {
    case 0:
        return 100;
    case 1:
    case 2:
        return 110; // cases 1 and 2 fall through to the same body
    case 3:
        return 120;
    default:
        return 130;
    }
}

int main() {
    int i;
    int n;
    int c;

    printf("== switch ==\n");
    printf("c0: %d\n", classify(0));
    printf("c2: %d\n", classify(2));
    printf("c3: %d\n", classify(3));
    printf("c9: %d\n", classify(9));

    // The controlling expression is evaluated once, not once per comparison.
    side = 0;
    switch (bump(2)) {
    case 1:
        printf("one\n");
        break;
    case 2:
        printf("two\n");
        break;
    default:
        printf("other\n");
    }
    printf("evals: %d\n", side);

    // Fall-through: case 3 runs 3 and 4 and then leaves the switch.
    n = 0;
    switch (3) {
    case 1:
        n = n + 1;
    case 2:
        n = n + 10;
    case 3:
        n = n + 100;
    case 4:
        n = n + 1000;
    }
    printf("fallthrough: %d\n", n);

    // default in the middle: no case matches, so control enters at default and
    // then falls through into case 2's body.
    n = 0;
    switch (7) {
    case 1:
        n = 1;
        break;
    default:
        n = 20;
    case 2:
        n = n + 2;
        break;
    }
    printf("middefault: %d\n", n);

    // No matching case and no default: the switch is skipped entirely.
    n = 5;
    switch (1) {
    case 2:
        n = 9;
    }
    printf("nomatch: %d\n", n);

    // A switch inside a loop: break leaves the switch (and then prints),
    // continue skips the rest of the loop body.
    printf("loop:");
    for (i = 0; i < 5; i = i + 1) {
        switch (i) {
        case 1:
            continue;
        case 3:
            break;
        default:
            printf(" %d", i);
            break;
        }
        printf("*%d", i);
    }
    printf("\n");

    // Nested switch: the inner break binds to the inner switch.
    n = 0;
    switch (1) {
    case 1:
        switch (2) {
        case 1:
            n = n + 1;
            break;
        case 2:
            n = n + 20;
            break;
        }
        n = n + 300;
        break;
    default:
        n = 0 - 1;
    }
    printf("nested: %d\n", n);

    // char and enumerator case labels.
    c = 'b';
    switch (c) {
    case 'a':
        printf("A\n");
        break;
    case 'b':
        printf("B\n");
        break;
    default:
        printf("?\n");
    }
    switch (BLUE) {
    case RED:
        printf("red\n");
        break;
    case BLUE:
        printf("blue\n");
        break;
    }

    printf("== do-while ==\n");
    printf("do:");
    i = 0;
    do {
        printf(" %d", i);
        i = i + 1;
    } while (i < 4);
    printf("\n");

    // The body runs even when the condition is false to begin with.
    i = 0;
    do {
        i = i + 7;
    } while (i < 0);
    printf("once: %d\n", i);

    // continue jumps to the condition, so this terminates (and skips 20).
    printf("cont:");
    i = 0;
    do {
        i = i + 1;
        if (i == 2) {
            continue;
        }
        printf(" %d", i * 10);
    } while (i < 4);
    printf("\n");

    i = 0;
    do {
        i = i + 1;
        if (i == 3) {
            break;
        }
    } while (i < 10);
    printf("dobreak: %d\n", i);

    printf("== goto ==\n");
    i = 0;
top:
    i = i + 1;
    if (i < 3) {
        goto top;
    }
    printf("back: %d\n", i);

    n = 1;
    goto skip;
    n = 99;
skip:
    printf("skip: %d\n", n);

    return 0;
}
