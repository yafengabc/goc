#include "goclib.h"

/* ----------------------------- <ctype.h> -------------------------------- */

/* The C standard says these take an int "whose value is representable as an
 * unsigned char or equal to EOF"; goc passes ints by value, so a plain
 * comparison table keyed on the char works for all values that fit.
 */

int isalpha(int c) {
    return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z');
}

int isdigit(int c) {
    return c >= '0' && c <= '9';
}

int isalnum(int c) {
    return isalpha(c) || isdigit(c);
}

int isspace(int c) {
    return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v';
}

int isupper(int c) {
    return c >= 'A' && c <= 'Z';
}

int islower(int c) {
    return c >= 'a' && c <= 'z';
}

int isxdigit(int c) {
    return isdigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
}

int ispunct(int c) {
    return (c >= 33 && c <= 47) || (c >= 58 && c <= 64) ||
           (c >= 91 && c <= 96) || (c >= 123 && c <= 126);
}

int isprint(int c) {
    return c >= 32 && c <= 126;
}

int isgraph(int c) {
    return c > 32 && c <= 126;
}

int iscntrl(int c) {
    return (c >= 0 && c <= 31) || c == 127;
}

int tolower(int c) {
    return isupper(c) ? c + ('a' - 'A') : c;
}

int toupper(int c) {
    return islower(c) ? c - ('a' - 'A') : c;
}
