#ifndef GOC_SHLOBJ_H
#define GOC_SHLOBJ_H

#include <windef.h>
#include <winuser.h>
#include <wingdi.h>

/* goc shlobj.h -- shell browse dialog (shell32). */

typedef void *PIDLIST_RELATIVE;
typedef void *PIDLIST_ABSOLUTE;
typedef void *PCIDLIST_ABSOLUTE;

typedef int (*BFFCALLBACK)(HWND hwnd, UINT uMsg, LPARAM lParam, LPARAM lpData);

typedef struct {
    HWND           hwndOwner;
    PIDLIST_ABSOLUTE pidlRoot;
    LPWSTR         pszDisplayName;
    LPCWSTR        lpszTitle;
    UINT           ulFlags;
    BFFCALLBACK    lpfn;
    LPARAM         lParam;
    int            iImage;
} BROWSEINFOW;

#define BIF_RETURNONLYFSDIRS    0x00000001
#define BIF_DONTGOBELOWDOMAIN   0x00000002
#define BIF_STATUSTEXT          0x00000004
#define BIF_RETURNFSANCESTORS   0x00000008
#define BIF_EDITBOX             0x00000010
#define BIF_VALIDATE            0x00000020
#define BIF_NEWDIALOGSTYLE      0x00000040
#define BIF_BROWSEINCLUDEURLS   0x00000080
#define BIF_USENEWUI            (BIF_EDITBOX | BIF_NEWDIALOGSTYLE)
#define BIF_BROWSEFORCOMPUTER   0x00001000
#define BIF_BROWSEFORPRINTER    0x00002000
#define BIF_BROWSEINCLUDEFILES  0x00004000
#define BIF_SHAREABLE           0x00008000
#define BIF_NONEWFOLDERBUTTON   0x00000100
#define BIF_NOTRANSLATETARGETS  0x00000400

#define BFFM_INITIALIZED   1
#define BFFM_SELCHANGED    2
#define BFFM_VALIDATEFAILED 3
#define BFFM_SETSTATUSTEXTW (WM_USER + 101)
#define BFFM_ENABLEOK       (WM_USER + 102)
#define BFFM_SETSELECTIONW  (WM_USER + 103)

extern PIDLIST_ABSOLUTE SHBrowseForFolderW(BROWSEINFOW *bi), shell32;
extern BOOL SHGetPathFromIDListW(PIDLIST_ABSOLUTE pidl, LPWSTR pszPath), shell32;
extern void CoTaskMemFree(LPVOID pv), ole32;

#endif /* GOC_SHLOBJ_H */
