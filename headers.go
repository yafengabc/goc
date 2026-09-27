package main

// builtinHeaders are the system headers goc ships with the compiler. They are
// injected when a program does `#include <name.h>`, with no disk lookup and no
// system headers required -- keeping the toolchain self-contained.
//
// The declarations use only the C subset goc's front end can parse today:
// void / char / int / long / unsigned / double as base types, pointers (*),
// arrays ([]), and function prototypes. We deliberately avoid `const`,
// `typedef`, `size_t`, and `struct`, none of which the parser models yet.
//
// printf and sprintf are intentionally NOT declared here: they are varargs
// functions, and goc models them through a runtime fallback (an undeclared
// call returns int), so declaring them with a prototype would wrongly reject
// their extra arguments. Everything else is declared and validated normally.
var builtinHeaders = map[string]string{
	"stdarg.h": `
#ifndef GOC_STDARG_H
#define GOC_STDARG_H
// va_list, va_start, va_arg and va_end are provided by the compiler as a
// built-in pointer type and special-cased code generation. This header exists
// only so that "#include <stdarg.h>" resolves without a disk lookup; the actual
// machinery is recognised by name in the parser and code generator.
#endif
`,

	"stddef.h": `
#ifndef GOC_STDDEF_H
#define GOC_STDDEF_H
// NULL is just the integer constant 0; goc has no real void* distinction.
#define NULL 0
#endif
`,

	"stdio.h": `
#ifndef GOC_STDIO_H
#define GOC_STDIO_H
// printf / sprintf are provided by the runtime as varargs builtins and are
// intentionally not prototyped here (see comment in headers.go).
int puts(char *s);
int putchar(int c);
int getchar(void);
#endif
`,

	"stdlib.h": `
#ifndef GOC_STDLIB_H
#define GOC_STDLIB_H
void *malloc(int size);
void free(void *ptr);
int atoi(char *s);
int abs(int x);
void exit(int code);
void *calloc(int n, int size);
long strtol(char *s, char **endp, int base);
int rand(void);
void srand(int seed);
#endif
`,

	"string.h": `
#ifndef GOC_STRING_H
#define GOC_STRING_H
int strlen(char *s);
char *strcpy(char *dest, char *src);
int strcmp(char *s1, char *s2);
char *strcat(char *dest, char *src);
void *memset(void *s, int c, int n);
void *memcpy(void *dest, void *src, int n);
char *strchr(char *s, int c);
int strncmp(char *s1, char *s2, int n);
int memcmp(void *s1, void *s2, int n);
void *memmove(void *dest, void *src, int n);
#endif
`,
}
