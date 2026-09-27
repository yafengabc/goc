#ifndef GOC_WINDOWS_H
#define GOC_WINDOWS_H

#include <stddef.h>

/* goc windows.h -- a minimal Win32 API surface for the goc toolchain.
 *
 * goc has no struct type yet, so only APIs whose parameters and return
 * values are scalars or pointers can be declared. The struct-heavy parts of
 * the real windows.h (CreateWindow, WNDCLASS, GetMessage, ...) deliberately
 * have no entry here -- they arrive together with struct support.
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
typedef unsigned long      DWORD_PTR;
typedef long               LPARAM;
typedef unsigned long      WPARAM;
typedef long               LRESULT;

typedef unsigned int       COLORREF;
typedef unsigned short     WCHAR;

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

typedef void              *LPVOID;
typedef const void        *LPCVOID;
typedef char              *LPSTR;
typedef const char        *LPCSTR;
typedef unsigned short    *LPWSTR;
typedef const unsigned short *LPCWSTR;
typedef unsigned char     *LPBYTE;
typedef unsigned int      *LPDWORD;
typedef int               *LPBOOL;

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

/* ------------------------------------------------------------------ */
/* MessageBox                                                          */
/* ------------------------------------------------------------------ */
#define MB_OK                 0x00000000
#define MB_OKCANCEL           0x00000001
#define MB_YESNO              0x00000004
#define MB_ICONERROR          0x00000010
#define MB_ICONQUESTION       0x00000020
#define MB_ICONWARNING        0x00000030
#define MB_ICONINFORMATION    0x00000040
#define MB_DEFBUTTON2         0x00000100

#define IDOK      1
#define IDCANCEL  2
#define IDYES     6
#define IDNO      7

/* ------------------------------------------------------------------ */
/* GetSystemMetrics                                                    */
/* ------------------------------------------------------------------ */
#define SM_CXSCREEN 0
#define SM_CYSCREEN 1
#define SM_CXSMICON 49
#define SM_CYSMICON 50

/* ------------------------------------------------------------------ */
/* kernel32                                                            */
/* ------------------------------------------------------------------ */
DWORD  GetLastError(void);
void   SetLastError(DWORD err);

HANDLE GetStdHandle(DWORD nStdHandle);
DWORD  GetFileType(HANDLE h);
BOOL   GetConsoleMode(HANDLE h, LPDWORD lpMode);
BOOL   SetConsoleMode(HANDLE h, DWORD dwMode);

BOOL   WriteFile(HANDLE h, LPCVOID buf, DWORD n, LPDWORD written, LPVOID ovl);
BOOL   ReadFile(HANDLE h, LPVOID buf, DWORD n, LPDWORD read, LPVOID ovl);
BOOL   WriteConsoleA(HANDLE h, LPCVOID buf, DWORD n, LPDWORD written, LPVOID res);
BOOL   CloseHandle(HANDLE h);
BOOL   FlushFileBuffers(HANDLE h);

void   ExitProcess(UINT code);

HMODULE GetModuleHandleA(LPCSTR name);
DWORD  GetModuleFileNameA(HMODULE h, LPSTR buf, DWORD n);
LPSTR  GetCommandLineA(void);
DWORD  GetEnvironmentVariableA(LPCSTR name, LPSTR buf, DWORD n);
BOOL   SetEnvironmentVariableA(LPCSTR name, LPCSTR value);
DWORD  GetCurrentDirectoryA(DWORD n, LPSTR buf);
BOOL   SetCurrentDirectoryA(LPCSTR path);
DWORD  GetTempPathA(DWORD n, LPSTR buf);
BOOL   GetComputerNameA(LPSTR buf, LPDWORD n);

HMODULE LoadLibraryA(LPCSTR name);
BOOL   FreeLibrary(HMODULE h);

DWORD  GetTickCount(void);
void   Sleep(DWORD ms);
int    GetSystemMetrics(int index);

/* ------------------------------------------------------------------ */
/* user32                                                              */
/* ------------------------------------------------------------------ */
int    MessageBoxA(HWND parent, LPCSTR text, LPCSTR caption, UINT flags);
int    MessageBoxW(HWND parent, LPCWSTR text, LPCWSTR caption, UINT flags);

HWND   FindWindowA(LPCSTR cls, LPCSTR title);
int    GetWindowTextA(HWND h, LPSTR buf, int n);
int    GetWindowTextLengthA(HWND h);
BOOL   SetWindowTextA(HWND h, LPCSTR text);
HWND   GetForegroundWindow(void);
HWND   GetDesktopWindow(void);
BOOL   IsWindow(HWND h);
BOOL   EnableWindow(HWND h, BOOL enable);
BOOL   ShowWindow(HWND h, int cmd);
HWND   SetFocus(HWND h);
BOOL   SetCursorPos(int x, int y);
DWORD  GetSysColor(int index);
UINT   GetDoubleClickTime(void);
LRESULT SendMessageA(HWND h, UINT msg, WPARAM wp, LPARAM lp);
BOOL   PostMessageA(HWND h, UINT msg, WPARAM wp, LPARAM lp);
HDC    GetDC(HWND h);
int    ReleaseDC(HWND h, HDC dc);

/* ------------------------------------------------------------------ */
/* gdi32                                                               */
/* ------------------------------------------------------------------ */
HGDIOBJ GetStockObject(int obj);
HGDIOBJ SelectObject(HDC dc, HGDIOBJ obj);
BOOL   SetBkColor(HDC dc, COLORREF color);
BOOL   SetTextColor(HDC dc, COLORREF color);
BOOL   TextOutA(HDC dc, int x, int y, LPCSTR text, int len);
BOOL   LineTo(HDC dc, int x, int y);
BOOL   Rectangle(HDC dc, int l, int t, int r, int b);
BOOL   Ellipse(HDC dc, int l, int t, int r, int b);
BOOL   PatBlt(HDC dc, int x, int y, int w, int h, DWORD rop);

#endif /* GOC_WINDOWS_H */
