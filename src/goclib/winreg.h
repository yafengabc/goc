#ifndef GOC_WINREG_H
#define GOC_WINREG_H

#include <windef.h>
#include <winbase.h>

/* goc winreg.h -- registry access (advapi32). Handles are declared as void *
 * so that HKEY_LOCAL_MACHINE &c->hKey (the "reserved" trick) type-checks the
 * same way it does in the real SDK. */

typedef void *HKEY;

#define HKEY_CLASSES_ROOT      ((HKEY)0x80000000L)
#define HKEY_CURRENT_USER      ((HKEY)0x80000001L)
#define HKEY_LOCAL_MACHINE     ((HKEY)0x80000002L)
#define HKEY_USERS             ((HKEY)0x80000003L)
#define HKEY_CURRENT_CONFIG    ((HKEY)0x80000005L)

#define REG_SZ                 1
#define REG_EXPAND_SZ          2
#define REG_BINARY             3
#define REG_DWORD              4
#define REG_MULTI_SZ           7
#define REG_QWORD              11

/* RegCreateKeyExW dwOptions. */
#define REG_OPTION_NON_VOLATILE  0
#define REG_OPTION_VOLATILE      1

/* Sub-key access rights, OR-ed into the samDesired of RegCreateKeyExW /
 * RegOpenKeyExW. KEY_READ and KEY_WRITE are ready-made combinations;
 * KEY_ALL_ACCESS is the union of everything. */
#define KEY_QUERY_VALUE         0x0001
#define KEY_SET_VALUE           0x0002
#define KEY_CREATE_SUB_KEY      0x0004
#define KEY_ENUMERATE_SUB_KEYS  0x0008
#define KEY_NOTIFY              0x0010
#define KEY_CREATE_LINK         0x0020
#define KEY_DELETE              0x0040
#define KEY_READ                0x20019
#define KEY_WRITE               0x20006
#define KEY_ALL_ACCESS          0xF003F

/* On 64-bit Windows the 32-bit registry view is the WOW6432Node hive. */
#define KEY_WOW64_64KEY         0x0100
#define KEY_WOW64_32KEY         0x0200

/* Return codes from the Reg* functions. Anything non-zero is a failure. */
#define ERROR_SUCCESS            0L
#define ERROR_FILE_NOT_FOUND     2L
#define ERROR_ACCESS_DENIED      5L
#define ERROR_INVALID_HANDLE     6L
#define ERROR_INVALID_PARAMETER  7L
#define ERROR_MORE_DATA          234L
#define ERROR_NO_MORE_ITEMS      259L

extern LONG RegCreateKeyExW(HKEY hKey, LPCWSTR lpSubKey, DWORD reserved, LPWSTR lpClass,
                            DWORD dwOptions, DWORD samDesired,
                            void *lpSecurityAttributes, HKEY *phkResult,
                            DWORD *lpdwDisposition), advapi32;
extern LONG RegOpenKeyExW(HKEY hKey, LPCWSTR lpSubKey, DWORD ulOptions, DWORD samDesired,
                          HKEY *phkResult), advapi32;
extern LONG RegCloseKey(HKEY hKey), advapi32;
extern LONG RegDeleteValueW(HKEY hKey, LPCWSTR lpValueName), advapi32;
extern LONG RegQueryValueExW(HKEY hKey, LPCWSTR lpValueName, LPDWORD lpReserved,
                             LPDWORD lpType, LPBYTE lpData, LPDWORD lpcbData), advapi32;
extern LONG RegSetValueExW(HKEY hKey, LPCWSTR lpValueName, DWORD dwReserved, DWORD dwType,
                           const BYTE *lpData, DWORD cbData), advapi32;
extern LONG RegQueryValueExA(HKEY hKey, LPCSTR lpValueName, LPDWORD lpReserved,
                             LPDWORD lpType, LPBYTE lpData, LPDWORD lpcbData), advapi32;
extern LONG RegSetValueExA(HKEY hKey, LPCSTR lpValueName, DWORD dwReserved, DWORD dwType,
                           const BYTE *lpData, DWORD cbData), advapi32;

#endif /* GOC_WINREG_H */
