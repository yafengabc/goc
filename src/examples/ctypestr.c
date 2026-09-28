// ctypestr.c -- regression lock for the stage-15 goclib additions.
//
// Exercises all 13 ctype.h predicates/transforms and the 7 string.h
// functions added during the goclib split (strncat, strrchr, strstr,
// strspn, strcspn, strpbrk, strtok). Output is deterministic -- every
// predicate prints as 0/1 -- so the golden file is byte-stable. This file
// also proves the go:embed goclib/*.h wildcard picked up ctype.h without
// any Go-side change (the #include below resolves from the embedded FS).

#include <stdio.h>
#include <ctype.h>
#include <string.h>

static void pred(int c) {
    printf("char %d alnum=%d alpha=%d cntrl=%d digit=%d graph=%d lower=%d "
           "print=%d punct=%d space=%d upper=%d xdigit=%d\n",
           c,
           isalnum(c), isalpha(c), iscntrl(c), isdigit(c), isgraph(c),
           islower(c), isprint(c), ispunct(c), isspace(c), isupper(c),
           isxdigit(c));
}

int main() {
    char buf[32];
    char s[64];
    char *t;

    printf("== ctype predicates ==\n");
    pred('A'); pred('z'); pred('7'); pred(' '); pred('!'); pred(',');
    pred(0); pred(127);

    printf("== case transforms ==\n");
    printf("tolower('Q')=%c tolower('5')=%c\n", tolower('Q'), tolower('5'));
    printf("toupper('q')=%c toupper('!')=%c\n", toupper('q'), toupper('!'));

    printf("== strncat ==\n");
    strcpy(buf, "foo");
    strncat(buf, "barbaz", 3);
    printf("short=%s\n", buf);
    strcpy(buf, "foo");
    strncat(buf, "barbaz", 20);
    printf("long=%s\n", buf);

    printf("== strrchr ==\n");
    printf("last-o=%s\n", strrchr("hello", 'o'));
    printf("last-l=%s\n", strrchr("hello", 'l'));
    printf("missing=%d\n", strrchr("hello", 'x') == 0);

    printf("== strstr ==\n");
    printf("find=%s\n", strstr("hello world", "world"));
    printf("mid=%s\n", strstr("hello world", "lo w"));
    printf("missing=%d\n", strstr("hello", "xyz") == 0);

    printf("== strspn ==\n");
    printf("all-in=%d\n", strspn("aabbccdd", "abc"));
    printf("none=%d\n", strspn("xyzabc", "abc"));

    printf("== strcspn ==\n");
    printf("stop-at=%d\n", strcspn("hello, world", ",!"));
    printf("no-hit=%d\n", strcspn("abcdef", "xyz"));

    printf("== strpbrk ==\n");
    printf("hit=%s\n", strpbrk("hello, world", ", "));
    printf("missing=%d\n", strpbrk("hello", "xyz") == 0);

    printf("== strtok ==\n");
    strcpy(s, "the,quick;brown.fox");
    t = strtok(s, ",;.");
    while (t != 0) {
        printf("[%s]\n", t);
        t = strtok(0, ",;.");
    }

    printf("== strtok padding ==\n");
    strcpy(s, "  a  b  ");
    t = strtok(s, " ");
    while (t != 0) {
        printf("[%s]\n", t);
        t = strtok(0, " ");
    }

    return 0;
}
