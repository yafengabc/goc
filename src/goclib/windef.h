#ifndef GOC_WINDEF_H
#define GOC_WINDEF_H

#include <stddef.h>

/* goc windef.h -- fundamental Win32 scalar types, handles and the basic
 * geometry structs. This is the first include of <windows.h>.
 *
 * Type sizes follow LLP64, which goc's C subset expresses exactly:
 *   char=1  short=2  int=4  long=8
 * so DWORD/WORD/BYTE/LONG/LONGLONG all line up with the real Win32 types.
 * BOOL and the 32-bit handles are 8 bytes in goc's register model, but the
 * values Windows stores in RAX are 32-bit, so goc zero/sign-extends call
 * results according to the declared return type (see genCallExpr).
 *
 * Calling convention: Win64 has a single flat convention, so WINAPI and
 * CALLBACK expand to nothing.
 */

/* ------------------------------------------------------------------ */
/* Scalar types (LLP64)                                                */
/* ------------------------------------------------------------------ */
typedef signed char        INT8;
typedef short              INT16;
typedef int                INT32;
typedef long               INT64;

typedef unsigned char      BYTE;
typedef unsigned short     WORD;
typedef unsigned int       DWORD;
typedef unsigned int       UINT;
typedef unsigned int       ULONG;       /* Windows long is 32-bit         */
typedef unsigned long      ULONGLONG;   /* goc long is 64-bit            */

typedef int                LONG;
typedef long               LONGLONG;
typedef int                BOOL;
typedef int                INT;
typedef char               CHAR;
typedef unsigned char      UCHAR;
typedef short              SHORT;
typedef unsigned short     USHORT;
typedef size_t             SIZE_T;

typedef long               INT_PTR;
typedef unsigned long      UINT_PTR;
typedef long               LONG_PTR;
typedef unsigned long      ULONG_PTR;
typedef unsigned long      DWORD_PTR;
typedef long               LPARAM;
typedef unsigned long      WPARAM;
typedef long               LRESULT;

typedef unsigned int       COLORREF;
typedef unsigned short     WCHAR;
typedef unsigned short     ATOM;

/* ------------------------------------------------------------------ */
/* Pointer types                                                       */
/* ------------------------------------------------------------------ */
typedef void              *HANDLE;
typedef HANDLE             HWND;
typedef HANDLE             HMODULE;
typedef HANDLE             HINSTANCE;
typedef HANDLE             HDC;
typedef HANDLE             HGDIOBJ;
typedef HANDLE             HKEY;
typedef HANDLE             HICON;
typedef HANDLE             HCURSOR;
typedef HANDLE             HBRUSH;
typedef HANDLE             HFONT;
typedef HANDLE             HBITMAP;
typedef HANDLE             HPEN;
typedef HANDLE             HRGN;
typedef HANDLE             HMENU;
typedef HANDLE             HACCEL;

typedef void              *LPVOID;
typedef const void        *LPCVOID;
typedef char              *LPSTR;
typedef const char        *LPCSTR;
typedef unsigned short    *LPWSTR;
typedef const unsigned short *LPCWSTR;
typedef unsigned char     *LPBYTE;
typedef unsigned int      *LPDWORD;
typedef int               *LPBOOL;
typedef long              *LPLONG;
typedef void              *PVOID;

/* ------------------------------------------------------------------ */
/* Decorations: flat Win64 convention, so all are empty                */
/* ------------------------------------------------------------------ */
#define WINAPI
#define CALLBACK
#define APIENTRY
#define DECLSPEC_IMPORT

#ifndef NULL
#define NULL ((void *)0)
#endif
#define TRUE  1
#define FALSE 0

/* ------------------------------------------------------------------ */
/* Handles and standard devices                                        */
/* ------------------------------------------------------------------ */
#define STD_INPUT_HANDLE    ((DWORD)-10)
#define STD_OUTPUT_HANDLE   ((DWORD)-11)
#define STD_ERROR_HANDLE    ((DWORD)-12)
#define INVALID_HANDLE_VALUE ((HANDLE)-1)

#define MAX_PATH 260

/* ------------------------------------------------------------------ */
/* Basic geometry structs (LLP64: LONG is 4 bytes)                     */
/* ------------------------------------------------------------------ */
typedef struct {
    LONG left;
    LONG top;
    LONG right;
    LONG bottom;
} RECT;

typedef struct {
    LONG x;
    LONG y;
} POINT;

typedef struct {
    LONG cx;
    LONG cy;
} SIZE;

typedef RECT  *LPRECT;
typedef POINT *LPPOINT;

#endif /* GOC_WINDEF_H */
