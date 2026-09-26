int add(int a, int b) {
    return a + b;
}

int main() {
    int x = 10;
    int y = 32;
    int s = add(x, y);
    printf("10 + 32 = %d\n", s);

    int i = 0;
    int total = 0;
    while (i < 5) {
        total = total + i;
        i = i + 1;
    }
    printf("sum 0..4 = %d\n", total);

    if (s > 40) {
        printf("s is big\n");
    } else {
        printf("s is small\n");
    }
    return 0;
}
