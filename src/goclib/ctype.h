#ifndef GOC_CTYPE_H
#define GOC_CTYPE_H

/* goc ctype.h -- character classification and conversion (C89 signatures).
 *
 * Each predicate returns nonzero for a true classification, zero otherwise,
 * for any int representable as an unsigned char or EOF. Implementations are
 * linked in from goclib on demand; this header only carries declarations.
 */

int isalnum(int c);
int isalpha(int c);
int iscntrl(int c);
int isdigit(int c);
int isgraph(int c);
int islower(int c);
int isprint(int c);
int ispunct(int c);
int isspace(int c);
int isupper(int c);
int isxdigit(int c);
int tolower(int c);
int toupper(int c);

#endif /* GOC_CTYPE_H */
