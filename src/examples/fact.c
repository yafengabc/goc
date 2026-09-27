int fact(int n) {
    if (n <= 1) {
        return 1;
    }
    return n * fact(n - 1);
}
int main() {
    int r = fact(5);
    printf("fact(5) = %d\n", r);
    int t = (3 < 4) && (10 > 2);
    printf("logic = %d\n", t);
    int g = !0;
    printf("not0 = %d\n", g);
    return 0;
}
