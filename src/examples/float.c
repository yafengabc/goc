// Exercises the float type end to end: 4-byte storage with widening to double
// in every expression, literals with the f suffix, arrays, struct members,
// parameters and returns, globals, pointer arithmetic, cast conversion and ++.
//
// printf promotes every float argument to a double, so the printed value of a
// float reflects its single-precision rounding. After the widen, 1.0f/3.0f is
// the double 0.3333333432674408 (not 0.3333333333333333), but with the default
// %f it rounds to the same 6 digits; the widening difference is what the
// rounding test exercises via third==(float)dthird.

float gf = 2.5f;
float garr[3];

struct Pair {
    int n;
    float f;
    double d;
};

float addf(float a, float b) {
    return a + b;
}

float halve(float x) {
    return x / 2.0f;
}

int main() {
    printf("== scalar ==\n");
    float a = 1.25f;
    float b = 0.5f;
    printf("a=%f b=%f\n", a, b);
    printf("sum=%f\n", a + b);
    printf("diff=%f\n", a - b);
    printf("prod=%f\n", a * b);
    printf("div=%f\n", a / b);
    printf("neg=%f\n", -a);
    printf("sizeof float=%d\n", sizeof(float));
    printf("sizeof a=%d\n", sizeof(a));
    printf("sizeof double=%d\n", sizeof(double));

    printf("== rounding ==\n");
    float third = 1.0f / 3.0f;
    double dthird = 1.0 / 3.0;
    printf("third=%f\n", third);
    printf("dthird=%f\n", dthird);
    printf("cmp=%d\n", third == (float)dthird);

    printf("== array ==\n");
    float arr[4];
    arr[0] = 1.5f;
    arr[1] = 2.5f;
    arr[2] = arr[0] + arr[1];
    arr[3] = -0.5f;
    printf("arr: %f %f %f %f\n", arr[0], arr[1], arr[2], arr[3]);
    printf("sizeof arr=%d\n", sizeof(arr));

    printf("== struct ==\n");
    struct Pair p;
    p.n = 7;
    p.f = 3.5f;
    p.d = 9.5;
    printf("p: %d %f %f\n", p.n, p.f, p.d);
    printf("sizeof pair=%d\n", sizeof(struct Pair));

    printf("== pointer ==\n");
    float *q;
    q = &arr[1];
    printf("q=%f\n", *q);
    *q = 4.5f;
    printf("arr1=%f\n", arr[1]);
    q = q + 1;
    printf("next=%f\n", *q);

    printf("== function ==\n");
    printf("addf=%f\n", addf(1.5f, 2.25f));
    printf("halve=%f\n", halve(7.0f));
    float r = addf(a, b);
    printf("r=%f\n", r);
    printf("ints=%f\n", addf(1, 2));

    printf("== global ==\n");
    printf("gf=%f\n", gf);
    gf = gf * 2.0f;
    printf("gf=%f\n", gf);
    garr[0] = 1.25f;
    garr[1] = 2.5f;
    garr[2] = garr[0] + garr[1];
    printf("garr: %f %f %f\n", garr[0], garr[1], garr[2]);

    printf("== cast ==\n");
    printf("toint=%d\n", (int)a);
    printf("todouble=%f\n", (double)a);
    printf("fromdouble=%f\n", (float)dthird);
    double dd = a;
    printf("dd=%f\n", dd);
    float ff = dd;
    printf("ff=%f\n", ff);

    printf("== inc ==\n");
    a++;
    printf("a=%f\n", a);
    printf("post=%f\n", b++);
    printf("b=%f\n", b);

    return 0;
}
