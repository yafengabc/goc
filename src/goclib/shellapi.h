#ifndef GOC_SHELLAPI_H
#define GOC_SHELLAPI_H

#include <windef.h>
#include <winuser.h>
#include <wingdi.h>

/* goc shellapi.h -- shell functions (shell32): execute, drag-drop. */

typedef HANDLE HDROP;

extern HINSTANCE ShellExecuteW(HWND hwnd, LPCWSTR lpOperation, LPCWSTR lpFile,
                        LPCWSTR lpParameters, LPCWSTR lpDirectory, int nShowCmd), shell32;
extern void DragAcceptFiles(HWND hWnd, BOOL fAccept), shell32;
extern void DragFinish(HDROP hDrop), shell32;
extern UINT DragQueryFileW(HDROP hDrop, UINT iFile, LPWSTR lpszFile, UINT cch), shell32;
extern UINT DragQueryFileA(HDROP hDrop, UINT iFile, LPSTR lpszFile, UINT cch), shell32;

/* CommandLineToArgvW converts a Unicode command line into an argv array. */
extern LPWSTR *CommandLineToArgvW(LPCWSTR lpCmdLine, int *pNumArgs), shell32;

/* SHFileOperation etc. omitted -- not used by MDQuickViewer. */
extern BOOL SHGetSpecialFolderPathW(HWND hwnd, LPWSTR pszPath, int csidl, BOOL fCreate), shell32;

#endif /* GOC_SHELLAPI_H */
