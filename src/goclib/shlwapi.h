#ifndef GOC_SHLWAPI_H
#define GOC_SHLWAPI_H

#include <windef.h>
#include <winuser.h>
#include <wingdi.h>

/* goc shlwapi.h -- light-weight shell utility APIs (shlwapi). Only the
 * path helpers MDQuickViewer references are declared. */

extern BOOL PathFileExistsW(LPCWSTR pszPath), shlwapi;
extern BOOL PathIsDirectoryW(LPCWSTR pszPath), shlwapi;
extern BOOL PathAppendW(LPWSTR pszPath, LPCWSTR pszMore), shlwapi;
extern int  PathCommonPrefixW(LPCWSTR pszFile1, LPCWSTR pszFile2, LPWSTR pszBuf), shlwapi;
extern BOOL PathRemoveFileSpecW(LPWSTR pszPath), shlwapi;
extern LPWSTR PathFindFileNameW(LPCWSTR pszPath), shlwapi;

#endif /* GOC_SHLWAPI_H */
