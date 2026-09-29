/* UFCS + print: struct methods (Go/Nim style) and a Python-style print.
 *
 * A method is a plain function named T_f whose first parameter is struct T
 * (value receiver) or struct T* (pointer receiver); the checker rewrites
 * p.f(a) into T_f(&p, a) -- or T_f(p, a) / T_f(*p, a) for value receivers.
 * No vtables, no runtime metadata: a pure compile-time rewrite.
 *
 * print(expr, ...) builds one printf call from the static types of its
 * arguments: ints as %d (a char is its numeric value), doubles as %g with
 * trailing zeros stripped, strings as %s, other pointers as %p. Arguments
 * are separated by one space and the line ends with a newline; print()
 * with no arguments is just that newline.
 */
#include <stdio.h>

struct Point { int x; int y; };

void Point_print(struct Point* p) {
    printf("(%d, %d)", p->x, p->y);
}

int Point_dist2(struct Point p) {
    return p.x * p.x + p.y * p.y;
}

void Point_move(struct Point* p, int dx, int dy) {
    p->x = p->x + dx;
    p->y = p->y + dy;
}

int main() {
    struct Point p;
    p.x = 3;
    p.y = 4;

    p.print();
    print();
    print("dist2 =", p.dist2());

    p.move(10, 20);
    p.print();
    print();

    print(1, 2.5, "three", 'A');
    print(0.5, 1.0, -2.0);
    return 0;
}
