#ifndef GOC_STDBIT_H
#define GOC_STDBIT_H
/* goc stdbit.h -- C23 7.18 bit and byte utilities.
 *
 * The standard prefixes every name with stdc_ ; the type-generic macros are
 * implemented with _Generic over the four distinct integer types goc has
 * (unsigned long long is the same type as unsigned long here, so the _ull
 * functions are separate entry points that the macro never selects -- take
 * their address instead).
 */
#include <stddef.h>
#include <stdint.h>
#include <stdbool.h>

#define __STDC_VERSION_STDBIT_H__ 202311L
/* goc targets little-endian x86-64 only; the values are implementation
 * defined and follow the __BYTE_ORDER__ convention. */
#define __STDC_ENDIAN_LITTLE__ 1234
#define __STDC_ENDIAN_BIG__    4321
#define __STDC_ENDIAN_NATIVE__ __STDC_ENDIAN_LITTLE__

unsigned int stdc_leading_zeros_uc(unsigned char x);
unsigned int stdc_leading_ones_uc(unsigned char x);
unsigned int stdc_trailing_zeros_uc(unsigned char x);
unsigned int stdc_trailing_ones_uc(unsigned char x);
unsigned int stdc_count_zeros_uc(unsigned char x);
unsigned int stdc_count_ones_uc(unsigned char x);
unsigned int stdc_first_leading_zero_uc(unsigned char x);
unsigned int stdc_first_leading_one_uc(unsigned char x);
unsigned int stdc_first_trailing_zero_uc(unsigned char x);
unsigned int stdc_first_trailing_one_uc(unsigned char x);
bool stdc_has_single_bit_uc(unsigned char x);
unsigned int stdc_bit_width_uc(unsigned char x);
unsigned char stdc_bit_floor_uc(unsigned char x);
unsigned char stdc_bit_ceil_uc(unsigned char x);
unsigned int stdc_leading_zeros_us(unsigned short x);
unsigned int stdc_leading_ones_us(unsigned short x);
unsigned int stdc_trailing_zeros_us(unsigned short x);
unsigned int stdc_trailing_ones_us(unsigned short x);
unsigned int stdc_count_zeros_us(unsigned short x);
unsigned int stdc_count_ones_us(unsigned short x);
unsigned int stdc_first_leading_zero_us(unsigned short x);
unsigned int stdc_first_leading_one_us(unsigned short x);
unsigned int stdc_first_trailing_zero_us(unsigned short x);
unsigned int stdc_first_trailing_one_us(unsigned short x);
bool stdc_has_single_bit_us(unsigned short x);
unsigned int stdc_bit_width_us(unsigned short x);
unsigned short stdc_bit_floor_us(unsigned short x);
unsigned short stdc_bit_ceil_us(unsigned short x);
unsigned int stdc_leading_zeros_ui(unsigned int x);
unsigned int stdc_leading_ones_ui(unsigned int x);
unsigned int stdc_trailing_zeros_ui(unsigned int x);
unsigned int stdc_trailing_ones_ui(unsigned int x);
unsigned int stdc_count_zeros_ui(unsigned int x);
unsigned int stdc_count_ones_ui(unsigned int x);
unsigned int stdc_first_leading_zero_ui(unsigned int x);
unsigned int stdc_first_leading_one_ui(unsigned int x);
unsigned int stdc_first_trailing_zero_ui(unsigned int x);
unsigned int stdc_first_trailing_one_ui(unsigned int x);
bool stdc_has_single_bit_ui(unsigned int x);
unsigned int stdc_bit_width_ui(unsigned int x);
unsigned int stdc_bit_floor_ui(unsigned int x);
unsigned int stdc_bit_ceil_ui(unsigned int x);
unsigned int stdc_leading_zeros_ul(unsigned long x);
unsigned int stdc_leading_ones_ul(unsigned long x);
unsigned int stdc_trailing_zeros_ul(unsigned long x);
unsigned int stdc_trailing_ones_ul(unsigned long x);
unsigned int stdc_count_zeros_ul(unsigned long x);
unsigned int stdc_count_ones_ul(unsigned long x);
unsigned int stdc_first_leading_zero_ul(unsigned long x);
unsigned int stdc_first_leading_one_ul(unsigned long x);
unsigned int stdc_first_trailing_zero_ul(unsigned long x);
unsigned int stdc_first_trailing_one_ul(unsigned long x);
bool stdc_has_single_bit_ul(unsigned long x);
unsigned int stdc_bit_width_ul(unsigned long x);
unsigned long stdc_bit_floor_ul(unsigned long x);
unsigned long stdc_bit_ceil_ul(unsigned long x);
unsigned int stdc_leading_zeros_ull(unsigned long long x);
unsigned int stdc_leading_ones_ull(unsigned long long x);
unsigned int stdc_trailing_zeros_ull(unsigned long long x);
unsigned int stdc_trailing_ones_ull(unsigned long long x);
unsigned int stdc_count_zeros_ull(unsigned long long x);
unsigned int stdc_count_ones_ull(unsigned long long x);
unsigned int stdc_first_leading_zero_ull(unsigned long long x);
unsigned int stdc_first_leading_one_ull(unsigned long long x);
unsigned int stdc_first_trailing_zero_ull(unsigned long long x);
unsigned int stdc_first_trailing_one_ull(unsigned long long x);
bool stdc_has_single_bit_ull(unsigned long long x);
unsigned int stdc_bit_width_ull(unsigned long long x);
unsigned long long stdc_bit_floor_ull(unsigned long long x);
unsigned long long stdc_bit_ceil_ull(unsigned long long x);

/* C23 rotation (7.18). `shift` is always unsigned int; each value is rotated
 * within its own width. goc has no rotate built-in, so these are plain shifts
 * (see stdbit.c for the width-bounded implementations). */
unsigned char       stdc_rotate_left_uc(unsigned char value, unsigned int shift);
unsigned short      stdc_rotate_left_us(unsigned short value, unsigned int shift);
unsigned int        stdc_rotate_left_ui(unsigned int value, unsigned int shift);
unsigned long       stdc_rotate_left_ul(unsigned long value, unsigned int shift);
unsigned long long  stdc_rotate_left_ull(unsigned long long value, unsigned int shift);
unsigned char       stdc_rotate_right_uc(unsigned char value, unsigned int shift);
unsigned short      stdc_rotate_right_us(unsigned short value, unsigned int shift);
unsigned int        stdc_rotate_right_ui(unsigned int value, unsigned int shift);
unsigned long       stdc_rotate_right_ul(unsigned long value, unsigned int shift);
unsigned long long  stdc_rotate_right_ull(unsigned long long value, unsigned int shift);

#define stdc_leading_zeros(x) _Generic((x), unsigned char: stdc_leading_zeros_uc(x), unsigned short: stdc_leading_zeros_us(x), unsigned int: stdc_leading_zeros_ui(x), unsigned long: stdc_leading_zeros_ul(x))
#define stdc_leading_ones(x) _Generic((x), unsigned char: stdc_leading_ones_uc(x), unsigned short: stdc_leading_ones_us(x), unsigned int: stdc_leading_ones_ui(x), unsigned long: stdc_leading_ones_ul(x))
#define stdc_trailing_zeros(x) _Generic((x), unsigned char: stdc_trailing_zeros_uc(x), unsigned short: stdc_trailing_zeros_us(x), unsigned int: stdc_trailing_zeros_ui(x), unsigned long: stdc_trailing_zeros_ul(x))
#define stdc_trailing_ones(x) _Generic((x), unsigned char: stdc_trailing_ones_uc(x), unsigned short: stdc_trailing_ones_us(x), unsigned int: stdc_trailing_ones_ui(x), unsigned long: stdc_trailing_ones_ul(x))
#define stdc_count_zeros(x) _Generic((x), unsigned char: stdc_count_zeros_uc(x), unsigned short: stdc_count_zeros_us(x), unsigned int: stdc_count_zeros_ui(x), unsigned long: stdc_count_zeros_ul(x))
#define stdc_count_ones(x) _Generic((x), unsigned char: stdc_count_ones_uc(x), unsigned short: stdc_count_ones_us(x), unsigned int: stdc_count_ones_ui(x), unsigned long: stdc_count_ones_ul(x))
#define stdc_first_leading_zero(x) _Generic((x), unsigned char: stdc_first_leading_zero_uc(x), unsigned short: stdc_first_leading_zero_us(x), unsigned int: stdc_first_leading_zero_ui(x), unsigned long: stdc_first_leading_zero_ul(x))
#define stdc_first_leading_one(x) _Generic((x), unsigned char: stdc_first_leading_one_uc(x), unsigned short: stdc_first_leading_one_us(x), unsigned int: stdc_first_leading_one_ui(x), unsigned long: stdc_first_leading_one_ul(x))
#define stdc_first_trailing_zero(x) _Generic((x), unsigned char: stdc_first_trailing_zero_uc(x), unsigned short: stdc_first_trailing_zero_us(x), unsigned int: stdc_first_trailing_zero_ui(x), unsigned long: stdc_first_trailing_zero_ul(x))
#define stdc_first_trailing_one(x) _Generic((x), unsigned char: stdc_first_trailing_one_uc(x), unsigned short: stdc_first_trailing_one_us(x), unsigned int: stdc_first_trailing_one_ui(x), unsigned long: stdc_first_trailing_one_ul(x))
#define stdc_has_single_bit(x) _Generic((x), unsigned char: stdc_has_single_bit_uc(x), unsigned short: stdc_has_single_bit_us(x), unsigned int: stdc_has_single_bit_ui(x), unsigned long: stdc_has_single_bit_ul(x))
#define stdc_bit_width(x) _Generic((x), unsigned char: stdc_bit_width_uc(x), unsigned short: stdc_bit_width_us(x), unsigned int: stdc_bit_width_ui(x), unsigned long: stdc_bit_width_ul(x))
#define stdc_bit_floor(x) _Generic((x), unsigned char: stdc_bit_floor_uc(x), unsigned short: stdc_bit_floor_us(x), unsigned int: stdc_bit_floor_ui(x), unsigned long: stdc_bit_floor_ul(x))
#define stdc_bit_ceil(x) _Generic((x), unsigned char: stdc_bit_ceil_uc(x), unsigned short: stdc_bit_ceil_us(x), unsigned int: stdc_bit_ceil_ui(x), unsigned long: stdc_bit_ceil_ul(x))
#define stdc_rotate_left(value, shift) _Generic((value), unsigned char: stdc_rotate_left_uc(value, shift), unsigned short: stdc_rotate_left_us(value, shift), unsigned int: stdc_rotate_left_ui(value, shift), unsigned long: stdc_rotate_left_ul(value, shift))
#define stdc_rotate_right(value, shift) _Generic((value), unsigned char: stdc_rotate_right_uc(value, shift), unsigned short: stdc_rotate_right_us(value, shift), unsigned int: stdc_rotate_right_ui(value, shift), unsigned long: stdc_rotate_right_ul(value, shift))
#endif /* GOC_STDBIT_H */
