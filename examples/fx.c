int fact(int n) {
    if (n <= 1) { return 1; }
    return n * fact(n - 1);
}
int main() {
    printf("fact(6) = %d\n", fact(6));
    return 0;
}
