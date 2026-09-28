#ifndef GOC_WINBASE_H
#define GOC_WINBASE_H

#include <windef.h>

/* goc winbase.h -- kernel32 API surface: process, file, console, module,
 * environment, time and memory primitives. Included by <windows.h>.
 *
 * All functions here import from kernel32.dll (see goclib/win32.def).
 * A function only appears here once the prototype is declared AND its name
 * is listed under "# kernel32" in win32.def; that file is the single source
 * of truth for "which DLL".
 */

/* ------------------------------------------------------------------ */
/* Error handling                                                      */
/* ------------------------------------------------------------------ */
DWORD  GetLastError(void);
void   SetLastError(DWORD err);

/* ------------------------------------------------------------------ */
/* Handles and devices                                                 */
/* ------------------------------------------------------------------ */
HANDLE GetStdHandle(DWORD nStdHandle);
DWORD  GetFileType(HANDLE h);
BOOL   CloseHandle(HANDLE h);
BOOL   FlushFileBuffers(HANDLE h);

/* ------------------------------------------------------------------ */
/* Console                                                             */
/* ------------------------------------------------------------------ */
BOOL   GetConsoleMode(HANDLE h, LPDWORD lpMode);
BOOL   SetConsoleMode(HANDLE h, DWORD dwMode);
BOOL   WriteConsoleA(HANDLE h, LPCVOID buf, DWORD n, LPDWORD written, LPVOID res);

/* ------------------------------------------------------------------ */
/* File I/O                                                            */
/* ------------------------------------------------------------------ */
BOOL   WriteFile(HANDLE h, LPCVOID buf, DWORD n, LPDWORD written, LPVOID ovl);
BOOL   ReadFile(HANDLE h, LPVOID buf, DWORD n, LPDWORD read, LPVOID ovl);
HANDLE CreateFileA(LPCSTR name, DWORD access, DWORD share, LPVOID sec,
                   DWORD creation, DWORD flags, HANDLE tmpl);
DWORD  GetFileSize(HANDLE h, LPDWORD high);
BOOL   SetFilePointer(HANDLE h, LONG dist, LPLONG high, DWORD move);
BOOL   GetFileTime(HANDLE h, LPVOID created, LPVOID accessed, LPVOID written);
BOOL   DeleteFileA(LPCSTR name);
BOOL   MoveFileA(LPCSTR from, LPCSTR to);
BOOL   CopyFileA(LPCSTR from, LPCSTR to, BOOL failIfExists);

/* ------------------------------------------------------------------ */
/* Process / module                                                    */
/* ------------------------------------------------------------------ */
void   ExitProcess(UINT code);
HMODULE GetModuleHandleA(LPCSTR name);
DWORD  GetModuleFileNameA(HMODULE h, LPSTR buf, DWORD n);
LPSTR  GetCommandLineA(void);
HMODULE LoadLibraryA(LPCSTR name);
BOOL   FreeLibrary(HMODULE h);

/* ------------------------------------------------------------------ */
/* Environment / paths                                                 */
/* ------------------------------------------------------------------ */
DWORD  GetEnvironmentVariableA(LPCSTR name, LPSTR buf, DWORD n);
BOOL   SetEnvironmentVariableA(LPCSTR name, LPCSTR value);
DWORD  GetCurrentDirectoryA(DWORD n, LPSTR buf);
BOOL   SetCurrentDirectoryA(LPCSTR path);
DWORD  GetTempPathA(DWORD n, LPSTR buf);
BOOL   GetComputerNameA(LPSTR buf, LPDWORD n);

/* ------------------------------------------------------------------ */
/* Time                                                                */
/* ------------------------------------------------------------------ */
DWORD  GetTickCount(void);
void   Sleep(DWORD ms);
void   GetSystemTime(LPVOID st);   /* SYSTEMTIME*  */
void   GetLocalTime(LPVOID st);    /* SYSTEMTIME*  */
void   GetSystemTimeAsFileTime(LPVOID ft); /* FILETIME* */
BOOL   SystemTimeToFileTime(LPVOID st, LPVOID ft);
BOOL   FileTimeToSystemTime(LPVOID ft, LPVOID st);

/* ------------------------------------------------------------------ */
/* Memory                                                              */
/* ------------------------------------------------------------------ */
LPVOID HeapAlloc(HANDLE heap, DWORD flags, SIZE_T bytes);
BOOL   HeapFree(HANDLE heap, DWORD flags, LPVOID mem);
HANDLE GetProcessHeap(void);

/* ------------------------------------------------------------------ */
/* SYSTEMTIME / FILETIME (struct layout, LLP64, matches winbase.h)     */
/* ------------------------------------------------------------------ */
typedef struct {
    WORD wYear;
    WORD wMonth;
    WORD wDayOfWeek;
    WORD wDay;
    WORD wHour;
    WORD wMinute;
    WORD wSecond;
    WORD wMilliseconds;
} SYSTEMTIME;

typedef struct {
    DWORD dwLowDateTime;
    DWORD dwHighDateTime;
} FILETIME;

typedef struct {
    ULONG_PTR Internal;
    ULONG_PTR InternalHigh;
    DWORD Offset;
    DWORD OffsetHigh;
    HANDLE hEvent;
} OVERLAPPED;

typedef struct {
    DWORD nLength;
    LPVOID lpSecurityDescriptor;
    BOOL bInheritHandle;
} SECURITY_ATTRIBUTES;

/* File-access / creation disposition constants for CreateFileA. */
#define GENERIC_READ    0x80000000
#define GENERIC_WRITE   0x40000000
#define FILE_SHARE_READ    0x00000001
#define FILE_SHARE_WRITE   0x00000002
#define OPEN_EXISTING      3
#define CREATE_ALWAYS      2
#define CREATE_NEW         1
#define OPEN_ALWAYS        4
#define TRUNCATE_EXISTING  5
#define FILE_ATTRIBUTE_NORMAL 0x80
#define INVALID_SET_FILE_POINTER ((DWORD)-1)

#define FILE_BEGIN   0
#define FILE_CURRENT 1
#define FILE_END     2

#endif /* GOC_WINBASE_H */
