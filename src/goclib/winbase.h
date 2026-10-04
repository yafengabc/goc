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
extern HANDLE CreateFileW(LPCWSTR name, DWORD access, DWORD share, LPVOID sec,
                   DWORD creation, DWORD flags, HANDLE tmpl), kernel32;
extern BOOL   ReadFile(HANDLE h, LPVOID buf, DWORD n, LPDWORD got, LPVOID overlapped), kernel32;
extern BOOL   WriteFile(HANDLE h, LPCVOID buf, DWORD n, LPDWORD put, LPVOID overlapped), kernel32;
extern DWORD  GetFileSize(HANDLE h, LPDWORD high), kernel32;
extern BOOL   SetFilePointer(HANDLE h, LONG dist, LPLONG high, DWORD move), kernel32;
extern BOOL   GetFileTime(HANDLE h, LPVOID created, LPVOID accessed, LPVOID written), kernel32;
extern BOOL   SetFileTime(HANDLE h, LPVOID created, LPVOID accessed, LPVOID written), kernel32;
extern BOOL   DeleteFileA(LPCSTR name), kernel32;
extern BOOL   DeleteFileW(LPCWSTR name), kernel32;
extern BOOL   MoveFileA(LPCSTR from, LPCSTR to), kernel32;
extern BOOL   MoveFileW(LPCWSTR from, LPCWSTR to), kernel32;
extern BOOL   CopyFileA(LPCSTR from, LPCSTR to, BOOL failIfExists), kernel32;
extern BOOL   CopyFileW(LPCWSTR from, LPCWSTR to, BOOL failIfExists), kernel32;
extern BOOL   FlushFileBuffers(HANDLE h), kernel32;
extern HANDLE CreateFileMappingA(HANDLE file, LPVOID attr, DWORD protect,
                                 DWORD maxHigh, DWORD maxLow, LPCSTR name), kernel32;
extern LPVOID  MapViewOfFile(HANDLE mapping, DWORD access, DWORD offHigh,
                             DWORD offLow, SIZE_T bytes), kernel32;
extern BOOL   UnmapViewOfFile(LPVOID base), kernel32;

/* The value CreateFile returns on failure. It is not NULL, so a plain
 * `if (h == 0)` test silently accepts an unopened file. */
#ifndef INVALID_HANDLE_VALUE
#define INVALID_HANDLE_VALUE ((HANDLE)(LONG_PTR)-1)
#endif

/* ------------------------------------------------------------------ */
/* Process / module                                                    */
/* ------------------------------------------------------------------ */
extern void   ExitProcess(UINT code), kernel32;
extern HMODULE GetModuleHandleA(LPCSTR name), kernel32;
extern DWORD  GetModuleFileNameA(HMODULE h, LPSTR buf, DWORD n), kernel32;
extern LPSTR  GetCommandLineA(void), kernel32;
extern FARPROC GetProcAddress(HMODULE hModule, LPCSTR lpProcName), kernel32;
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
extern DWORD  GetTempPathW(DWORD n, LPWSTR buf), kernel32;
extern UINT   GetTempFileNameW(LPCWSTR dir, LPCWSTR prefix, UINT unique,
                               LPWSTR buf, UINT bufsize), kernel32;
extern BOOL   GetComputerNameA(LPSTR buf, LPDWORD n), kernel32;
extern BOOL   GetComputerNameW(LPWSTR buf, LPDWORD n), kernel32;

/* The W spellings of the environment / directory queries. A GUI program almost
 * always wants these: the paths it handles are already wide, and narrowing a
 * path through the ANSI code page loses anything outside it. */
extern DWORD  GetEnvironmentVariableW(LPCWSTR name, LPWSTR buf, DWORD n), kernel32;
extern BOOL   SetEnvironmentVariableW(LPCWSTR name, LPCWSTR value), kernel32;
extern DWORD  ExpandEnvironmentStringsW(LPCWSTR src, LPWSTR dst, DWORD n), kernel32;

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

/* The W variant carries the file name as UTF-16. Same offsets as the A form
 * except cFileName / cAlternateFileName double in element width. */
typedef struct {
    DWORD              dwFileAttributes;
    FILETIME           ftCreationTime;
    FILETIME           ftLastAccessTime;
    FILETIME           ftLastWriteTime;
    DWORD              nFileSizeHigh;
    DWORD              nFileSizeLow;
    DWORD              dwReserved0;
    DWORD              dwReserved1;
    WCHAR              cFileName[260];       /* MAX_PATH */
    WCHAR              cAlternateFileName[14];
} WIN32_FIND_DATAW;

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

/* File-access / creation disposition constants for CreateFileA/W. */
#define GENERIC_READ    0x80000000
#define GENERIC_WRITE   0x40000000
#define GENERIC_EXECUTE 0x20000000
#define GENERIC_ALL     0x10000000
#define FILE_SHARE_READ    0x00000001
#define FILE_SHARE_WRITE   0x00000002
#define FILE_SHARE_DELETE  0x00000004
#define FILE_SHARE_ALL     0x00000007
#define CREATE_NEW         1
#define CREATE_ALWAYS      2
#define OPEN_EXISTING      3
#define OPEN_ALWAYS        4
#define TRUNCATE_EXISTING  5
#define FILE_ATTRIBUTE_READONLY  0x00000001
#define FILE_ATTRIBUTE_HIDDEN    0x00000002
#define FILE_ATTRIBUTE_SYSTEM    0x00000004
#define FILE_ATTRIBUTE_DIRECTORY 0x00000010
#define FILE_ATTRIBUTE_ARCHIVE   0x00000020
#define FILE_ATTRIBUTE_NORMAL    0x00000080
#define FILE_FLAG_SEQUENTIAL_SCAN 0x08000000
#define FILE_FLAG_WRITE_THROUGH   0x80000000
#define INVALID_SET_FILE_POINTER ((DWORD)-1)

#define FILE_BEGIN   0
#define FILE_CURRENT 1
#define FILE_END     2

/* ------------------------------------------------------------------ */
/* Global / local memory (HGLOBAL used by the clipboard)                  */
/* ------------------------------------------------------------------ */
typedef HANDLE HGLOBAL;

#define GMEM_FIXED      0x0000
#define GMEM_MOVEABLE   0x0002
#define GMEM_ZEROINIT   0x0040
#define GMEM_MODIFY     0x0080
#define GMEM_DISCARDABLE 0x0100
#define GMEM_NOT_BANKED 0x1000
#define GMEM_SHARE      0x2000
#define GMEM_DDESHARE   0x2000
#define GMEM_LOWER      0x1000
#define GMEM_VALID_FLAGS 0x7F72
#define GMEM_INVALID_HANDLE ((HGLOBAL)-1)
#define GHND            (GMEM_MOVEABLE | GMEM_ZEROINIT)
#define GPTR            (GMEM_FIXED | GMEM_ZEROINIT)

#define LMEM_FIXED      0x0000
#define LMEM_MOVEABLE   0x0002
#define LMEM_ZEROINIT   0x0040
#define LMEM_MODIFY     0x0080
#define LMEM_DISCARDABLE 0x0F00
#define LMEM_VALID_FLAGS 0x0F72
#define LMEM_INVALID_HANDLE ((HLOCAL)-1)
#define LHND            (LMEM_MOVEABLE | LMEM_ZEROINIT)
#define LPTR            (LMEM_FIXED | LMEM_ZEROINIT)

extern HGLOBAL GlobalAlloc(UINT flags, SIZE_T bytes), kernel32;
extern LPVOID  GlobalLock(HGLOBAL h), kernel32;
extern BOOL    GlobalUnlock(HGLOBAL h), kernel32;
extern HGLOBAL GlobalFree(HGLOBAL h), kernel32;
extern SIZE_T  GlobalSize(HGLOBAL h), kernel32;
extern HGLOBAL GlobalReAlloc(HGLOBAL h, SIZE_T bytes, UINT flags), kernel32;

/* The Local* heap is a separate arena from Global*, but on a desktop process
 * both sit in the same per-process heap, so the two families are
 * interchangeable in practice. HLOCAL is a distinct handle type so that code
 * which mixes the two families still type-checks. */
typedef HANDLE HLOCAL;

extern HLOCAL  LocalAlloc(UINT flags, SIZE_T bytes), kernel32;
extern HLOCAL  LocalReAlloc(HLOCAL h, SIZE_T bytes, UINT flags), kernel32;
extern LPVOID  LocalLock(HLOCAL h), kernel32;
extern BOOL    LocalUnlock(HLOCAL h), kernel32;
extern SIZE_T  LocalSize(HLOCAL h), kernel32;
extern HLOCAL  LocalFree(HLOCAL h), kernel32;

/* ------------------------------------------------------------------ */
/* Code pages and ANSI <-> Unicode conversion                            */
/* ------------------------------------------------------------------ */
/* Identifiers accepted as the CodePage argument. Only the two that matter
 * for text that goc itself produces are named here: CP_ACP is the machine's
 * default ANSI code page, CP_UTF8 is UTF-8 with no BOM / no surrogate
 * translation. CP_OEM is the console code page. */
#define CP_ACP             0
#define CP_OEMCP           1
#define CP_MACCP           2
#define CP_IBM437          437
#define CP_UTF8            65001
#define CP_UTF7            65000

/* Encoding flags (the dwFlags argument). */
#define MB_PRECOMPOSED         0x00000001
#define MB_COMPOSITE           0x00000002
#define MB_USEGLYPHCHARS       0x00000004
#define MB_ERR_INVALID_CHARS   0x00000008

extern int    MultiByteToWideChar(UINT codepage, DWORD flags, LPCSTR src, int srclen,
                                  LPWSTR dst, int dstlen), kernel32;
extern int    WideCharToMultiByte(UINT codepage, DWORD flags, LPCWSTR src, int srclen,
                                  LPSTR dst, int dstlen, LPCSTR defchar, LPBOOL used), kernel32;

extern HMODULE GetModuleHandleW(LPCWSTR name), kernel32;
extern DWORD   GetModuleFileNameW(HMODULE h, LPWSTR buf, DWORD n), kernel32;
extern LPWSTR  GetCommandLineW(void), kernel32;
extern DWORD   GetFileAttributesW(LPCWSTR name), kernel32;
extern BOOL    SetFileAttributesW(LPCWSTR name, DWORD attr), kernel32;
extern HANDLE  FindFirstFileW(LPCWSTR pattern, LPVOID data), kernel32;
extern BOOL    FindNextFileW(HANDLE h, LPVOID data), kernel32;
extern BOOL    FindClose(HANDLE h), kernel32;
extern DWORD   GetFullPathNameW(LPCWSTR name, DWORD n, LPWSTR buf, LPWSTR *filePart), kernel32;
extern DWORD   GetCurrentDirectoryW(DWORD n, LPWSTR buf), kernel32;
extern BOOL    SetCurrentDirectoryW(LPCWSTR path), kernel32;
extern UINT    GetWindowsDirectoryW(LPWSTR buf, UINT n), kernel32;
extern UINT    GetSystemDirectoryW(LPWSTR buf, UINT n), kernel32;

/* INVALID_FILE_ATTRIBUTES -- GetFileAttributesW returns this on failure. */
#ifndef INVALID_FILE_ATTRIBUTES
#define INVALID_FILE_ATTRIBUTES ((DWORD)-1)
#endif
#ifndef FILE_ATTRIBUTE_DIRECTORY
#define FILE_ATTRIBUTE_DIRECTORY 0x00000010
#endif
#ifndef FILE_ATTRIBUTE_NORMAL
#define FILE_ATTRIBUTE_NORMAL    0x00000080
#endif

#endif /* GOC_WINBASE_H */
