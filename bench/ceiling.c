#include <stdio.h>

/* 天花板实验：源码已经是 gcc 会产出的形态（指针步进、边界外提、
   无冗余子表达式临时槽）。goc -O3 在这个输入上还比 gcc 慢，
   说明差距在 codegen 而非缺循环 pass。 */
void bsort(int *a, int n) {
	int *end = a + n;
	int *last = end - 1;
	while (last > a) {
		int *p = a;
		int *lim = last;
		while (p < lim) {
			int x = *p;
			int y = *(p + 1);
			if (x > y) {
				*p = y;
				*(p + 1) = x;
			}
			p++;
		}
		last--;
	}
}

int main(void) {
	int a[10000];
	for (int i = 0; i < 10000; i++) a[i] = 10000 - i;
	bsort(a, 10000);
	printf("sorted=%d\n", a[0]);
	return 0;
}
