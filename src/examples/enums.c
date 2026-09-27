// enum support: auto-increment, explicit values, negative values, enum in
// array sizes, enum-typed variables, typedef enum, enumerators as rvalues.
enum Color { RED, GREEN = 5, BLUE };
enum Mood { HAPPY = -3, SAD };
typedef enum { ZERO, ONE = 1, TWO } Number;

// The array size may be an enumerator; the global is a plain int initialised
// from an enum-typed variable.
int g_arr[BLUE];
enum Color g_color = GREEN;
int g_num = TWO;

int main() {
    enum Color c = BLUE;
    enum Mood m = SAD;
    Number n = ZERO;
    int a = RED;
    int b = GREEN;
    int d = HAPPY;
    int size = BLUE - RED;      // 6
    int expr = ONE + TWO;       // 1 + 2
    g_arr[RED] = 42;
    g_arr[GREEN] = g_arr[RED] + 1;
    printf("%d %d %d %d\n", a, b, (int)c, (int)m);
    printf("%d %d %d %d\n", (int)n, size, expr, g_arr[BLUE - 1]);
    printf("%d %d %d\n", g_color, g_num, g_arr[GREEN]);
    return 0;
}
