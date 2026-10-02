#ifndef GOC_WINBASE_H
#define GOC_WINBASE_H

#include <windef.h>

/* goc winbase.h -- kernel32 API surface: process, file, console, module,
 * environment, time and memory primitives. Included by <windows.h>.
 *
 * Every function here is a kernel32 import; the import DLL is named inline on
 * the prototype itself, e.g.
 *     extern BOOL CloseHandle(HANDLE h), kernel32;
 * so each header is self-describing and no central win32.def is needed.
 */

/* ------------------------------------------------------------------ */
/* Error handling                                                      */
/* ------------------------------------------------------------------ */
extern DWORD  GetLastError(void), kernel32;
extern void   SetLastError(DWORD err), kernel32;

/* ------------------------------------------------------------------ */
/* Handles and devices                                                 */
/* ------------------------------------------------------------------ */
extern HANDLE GetStdHandle(DWORD nStdHandle), kernel32;
extern DWORD  GetFileType(HANDLE h), kernel32;
extern BOOL   CloseHandle(HANDLE h), kernel32;
extern BOOL   FlushFileBuffers(HANDLE h), kernel32;

/* ------------------------------------------------------------------ */
/* Console                                                             */
/* ------------------------------------------------------------------ */
extern BOOL   GetConsoleMode(HANDLE h, LPDWORD lpMode), kernel32;
extern BOOL   SetConsoleMode(HANDLE h, DWORD dwMode), kernel32;
extern BOOL   WriteConsoleA(HANDLE h, LPCVOID buf, DWORD n, LPDWORD written, LPVOID res), kernel32;

/* ------------------------------------------------------------------ */
/* File I/O                                                            */
/* ------------------------------------------------------------------ */
extern BOOL   WriteFile(HANDLE h, LPCVOID buf, DWORD n, LPDWORD written, LPVOID ovl), kernel32;
extern BOOL   ReadFile(HANDLE h, LPVOID buf, DWORD n, LPDWORD read, LPVOID ovl), kernel32;
extern HANDLE CreateFileA(LPCSTR name, DWORD access, DWORD share, LPVOID sec,
                   DWORD creation, DWORD flags, HANDLE tmpl), kernel32;
extern DWORD  GetFileSize(HANDLE h, LPDWORD high), kernel32;
extern BOOL   SetFilePointer(HANDLE h, LONG dist, LPLONG high, DWORD move), kernel32;
extern BOOL   GetFileTime(HANDLE h, LPVOID created, LPVOID accessed, LPVOID written), kernel32;
extern BOOL   DeleteFileA(LPCSTR name), kernel32;
extern BOOL   MoveFileA(LPCSTR from, LPCSTR to), kernel32;
extern BOOL   CopyFileA(LPCSTR from, LPCSTR to, BOOL failIfExists), kernel32;

/* ------------------------------------------------------------------ */
/* Process / module                                                    */
/* ------------------------------------------------------------------ */
extern void   ExitProcess(UINT code), kernel32;
extern HMODULE GetModuleHandleA(LPCSTR name), kernel32;
extern DWORD  GetModuleFileNameA(HMODULE h, LPSTR buf, DWORD n), kernel32;
extern LPSTR  GetCommandLineA(void), kernel32;
extern HMODULE LoadLibraryA(LPCSTR name), kernel32;
extern BOOL   FreeLibrary(HMODULE h), kernel32;

/* ------------------------------------------------------------------ */
/* Environment / paths                                                 */
/* ------------------------------------------------------------------ */
extern DWORD  GetEnvironmentVariableA(LPCSTR name, LPSTR buf, DWORD n), kernel32;
extern BOOL   SetEnvironmentVariableA(LPCSTR name, LPCSTR value), kernel32;
extern DWORD  GetCurrentDirectoryA(DWORD n, LPSTR buf), kernel32;
extern BOOL   SetCurrentDirectoryA(LPCSTR path), kernel32;
extern DWORD  GetTempPathA(DWORD n, LPSTR buf), kernel32;
extern BOOL   GetComputerNameA(LPSTR buf, LPDWORD n), kernel32;

/* ------------------------------------------------------------------ */
/* Time                                                                */
/* ------------------------------------------------------------------ */
extern DWORD  GetTickCount(void), kernel32;
extern void   Sleep(DWORD ms), kernel32;
extern void   GetSystemTime(LPVOID st), kernel32;   /* SYSTEMTIME*  */
extern void   GetLocalTime(LPVOID st), kernel32;    /* SYSTEMTIME*  */
extern void   GetSystemTimeAsFileTime(LPVOID ft), kernel32; /* FILETIME* */
extern BOOL   SystemTimeToFileTime(LPVOID st, LPVOID ft), kernel32;
extern BOOL   FileTimeToSystemTime(LPVOID ft, LPVOID st), kernel32;

/* ------------------------------------------------------------------ */
/* Memory                                                              */
/* ------------------------------------------------------------------ */
extern LPVOID HeapAlloc(HANDLE heap, DWORD flags, SIZE_T bytes), kernel32;
extern BOOL   HeapFree(HANDLE heap, DWORD flags, LPVOID mem), kernel32;
extern LPVOID HeapReAlloc(HANDLE heap, DWORD flags, LPVOID mem, SIZE_T bytes), kernel32;
extern HANDLE GetProcessHeap(void), kernel32;

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

/* FindFirstFileA / FindNextFileA fill this; layout matches winbase.h (LLP64). */
typedef struct {
    DWORD    dwFileAttributes;
    FILETIME ftCreationTime;
    FILETIME ftLastAccessTime;
    FILETIME ftLastWriteTime;
    DWORD    nFileSizeHigh;
    DWORD    nFileSizeLow;
    DWORD    dwReserved0;
    DWORD    dwReserved1;
    char     cFileName[260];        /* MAX_PATH */
    char     cAlternateFileName[14];
} WIN32_FIND_DATAA;

/* GetFileAttributesExA fills this. */
typedef struct {
    DWORD    dwFileAttributes;
    FILETIME ftCreationTime;
    FILETIME ftLastAccessTime;
    FILETIME ftLastWriteTime;
    DWORD    nFileSizeHigh;
    DWORD    nFileSizeLow;
} WIN32_FILE_ATTRIBUTE_DATA;

/* CreateProcessA parameter blocks (layout matches winbase.h, LLP64).
 * Only `cb` (the structure size) is consulted by our callers. */
typedef struct {
    DWORD  cb;
    LPCSTR lpReserved;
    LPCSTR lpDesktop;
    LPCSTR lpTitle;
    DWORD  dwX;
    DWORD  dwY;
    DWORD  dwXSize;
    DWORD  dwYSize;
    DWORD  dwXCountChars;
    DWORD  dwYCountChars;
    DWORD  dwFillAttribute;
    DWORD  dwFlags;
    WORD   wShowWindow;
    WORD   cbReserved2;
    LPBYTE lpReserved2;
    HANDLE hStdInput;
    HANDLE hStdOutput;
    HANDLE hStdError;
} STARTUPINFOA;

typedef struct {
    HANDLE hProcess;
    HANDLE hThread;
    DWORD  dwProcessId;
    DWORD  dwThreadId;
} PROCESS_INFORMATION;

extern BOOL   CreateProcessA(LPCSTR lpApplicationName, LPSTR lpCommandLine,
                             LPVOID lpProcessAttributes, LPVOID lpThreadAttributes,
                             BOOL bInheritHandles, DWORD dwCreationFlags,
                             LPVOID lpEnvironment, LPCSTR lpCurrentDirectory,
                             LPVOID lpStartupInfo, LPVOID lpProcessInformation),
                       kernel32;
extern DWORD  WaitForSingleObject(HANDLE hHandle, DWORD dwMilliseconds), kernel32;
extern BOOL   GetExitCodeProcess(HANDLE hProcess, LPDWORD lpExitCode), kernel32;

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
