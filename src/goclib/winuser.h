#ifndef GOC_WINUSER_H
#define GOC_WINUSER_H

#include <windef.h>

/* goc winuser.h -- user32 API surface: window classes, message loop,
 * drawing primitives, system metrics and the GUI constants. Included by
 * <windows.h>. Every function is a user32 import declared inline, e.g.
 *     extern int MessageBoxA(HWND, LPCSTR, LPCSTR, UINT), user32;
 */

/* ------------------------------------------------------------------ */
/* PeekMessage remove flags (PM_)                                      */
/* ------------------------------------------------------------------ */
#define PM_NOREMOVE   0x0000
#define PM_REMOVE     0x0001
#define PM_NOYIELD    0x0002

/* ------------------------------------------------------------------ */
/* SetWindowPos flags (SWP_)                                           */
/* ------------------------------------------------------------------ */
#define SWP_NOSIZE        0x0001
#define SWP_NOMOVE        0x0002
#define SWP_NOZORDER      0x0004
#define SWP_NOREDRAW      0x0008
#define SWP_NOACTIVATE    0x0010
#define SWP_FRAMECHANGED  0x0020
#define SWP_SHOWWINDOW    0x0040
#define SWP_HIDEWINDOW    0x0080
#define SWP_NOOWNERZORDER 0x0200
#define SWP_NOSENDCHANGING 0x0400
#define HWND_TOP       ((HWND)0)
#define HWND_BOTTOM    ((HWND)1)
#define HWND_TOPMOST   ((HWND)-1)
#define HWND_NOTOPMOST ((HWND)-2)

/* ------------------------------------------------------------------ */
/* DrawText flags (DT_)                                                */
/* ------------------------------------------------------------------ */
#define DT_TOP            0x00000000
#define DT_LEFT           0x00000000
#define DT_CENTER         0x00000001
#define DT_RIGHT          0x00000002
#define DT_VCENTER        0x00000004
#define DT_BOTTOM         0x00000008
#define DT_WORDBREAK      0x00000010
#define DT_SINGLELINE     0x00000020
#define DT_EXPANDTABS     0x00000040
#define DT_TABSTOP        0x00000080
#define DT_NOCLIP         0x00000100
#define DT_EXTERNALLEADING 0x00000200
#define DT_CALCRECT       0x00000400
#define DT_NOPREFIX       0x00000800
#define DT_INTERNAL       0x00001000

/* ------------------------------------------------------------------ */
/* Cursor / icon resource IDs (IDC_ / IDI_)                            */
/* ------------------------------------------------------------------ */
#define IDC_ARROW      ((LPCSTR)32512)
#define IDC_IBEAM      ((LPCSTR)32513)
#define IDC_WAIT       ((LPCSTR)32514)
#define IDC_CROSS      ((LPCSTR)32515)
#define IDC_UPARROW    ((LPCSTR)32516)
#define IDC_SIZE       ((LPCSTR)32640)
#define IDC_ICON       ((LPCSTR)32641)
#define IDC_SIZENWSE   ((LPCSTR)32642)
#define IDC_SIZENESW   ((LPCSTR)32643)
#define IDC_SIZEWE     ((LPCSTR)32644)
#define IDC_SIZENS     ((LPCSTR)32645)
#define IDC_SIZEALL    ((LPCSTR)32646)
#define IDC_NO         ((LPCSTR)32648)
#define IDC_HAND       ((LPCSTR)32649)
#define IDC_APPSTARTING ((LPCSTR)32650)

#define IDI_APPLICATION ((LPCSTR)32512)
#define IDI_HAND        ((LPCSTR)32513)
#define IDI_QUESTION    ((LPCSTR)32514)
#define IDI_EXCLAMATION ((LPCSTR)32515)
#define IDI_ASTERISK    ((LPCSTR)32516)
#define IDI_WINLOGO     ((LPCSTR)32517)

/* ------------------------------------------------------------------ */
/* Window messages (WM_)                                               */
/* ------------------------------------------------------------------ */
#define WM_NULL             0x0000
#define WM_CREATE           0x0001
#define WM_DESTROY          0x0002
#define WM_MOVE             0x0003
#define WM_SIZE             0x0005
#define WM_ACTIVATE         0x0006
#define WM_SETTEXT          0x000C
#define WM_GETTEXT          0x000D
#define WM_GETTEXTLENGTH    0x000E
#define WM_PAINT            0x000F
#define WM_CLOSE            0x0010
#define WM_QUERYENDSESSION  0x0011
#define WM_QUIT             0x0012
#define WM_ERASEBKGND       0x0014
#define WM_SHOWWINDOW       0x0018
#define WM_KEYDOWN          0x0100
#define WM_KEYUP            0x0101
#define WM_CHAR             0x0102
#define WM_SYSKEYDOWN       0x0104
#define WM_COMMAND          0x0111
#define WM_TIMER            0x0113
#define WM_HSCROLL          0x0114
#define WM_VSCROLL          0x0115
#define WM_MOUSEMOVE        0x0200
#define WM_LBUTTONDOWN      0x0201
#define WM_LBUTTONUP        0x0202
#define WM_LBUTTONDBLCLK    0x0203
#define WM_RBUTTONDOWN      0x0204
#define WM_RBUTTONUP        0x0205
#define WM_RBUTTONDBLCLK    0x0206
#define WM_MBUTTONDOWN      0x0207
#define WM_MBUTTONUP        0x0208
#define WM_MOUSEWHEEL       0x020A
#define WM_USER             0x0400

/* ------------------------------------------------------------------ */
/* Window styles (WS_)                                                 */
/* ------------------------------------------------------------------ */
#define WS_OVERLAPPED       0x00000000
#define WS_POPUP            0x80000000
#define WS_CHILD            0x40000000
#define WS_MINIMIZE         0x20000000
#define WS_VISIBLE          0x10000000
#define WS_DISABLED         0x08000000
#define WS_CLIPSIBLINGS     0x04000000
#define WS_CLIPCHILDREN     0x02000000
#define WS_MAXIMIZE         0x01000000
#define WS_CAPTION          0x00C00000
#define WS_BORDER           0x00800000
#define WS_DLGFRAME         0x00400000
#define WS_VSCROLL          0x00200000
#define WS_HSCROLL          0x00100000
#define WS_SYSMENU          0x00080000
#define WS_THICKFRAME       0x00040000
#define WS_GROUP            0x00020000
#define WS_TABSTOP          0x00010000
#define WS_MINIMIZEBOX      0x00020000
#define WS_MAXIMIZEBOX      0x00010000
#define WS_OVERLAPPEDWINDOW (WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | \
                             WS_THICKFRAME | WS_MINIMIZEBOX | WS_MAXIMIZEBOX)

/* ------------------------------------------------------------------ */
/* ShowWindow commands (SW_)                                           */
/* ------------------------------------------------------------------ */
#define SW_HIDE             0
#define SW_SHOWNORMAL       1
#define SW_NORMAL           1
#define SW_SHOWMINIMIZED    2
#define SW_SHOWMAXIMIZED    3
#define SW_MAXIMIZE         3
#define SW_SHOWNOACTIVATE   4
#define SW_SHOW             5
#define SW_MINIMIZE         6
#define SW_SHOWMINNOACTIVE  7
#define SW_SHOWNA           8
#define SW_RESTORE          9
#define SW_SHOWDEFAULT      10
#define SW_MAX              10

/* ------------------------------------------------------------------ */
/* MessageBox and standard dialog results                              */
/* ------------------------------------------------------------------ */
#define MB_OK                 0x00000000
#define MB_OKCANCEL           0x00000001
#define MB_ABORTRETRYIGNORE   0x00000002
#define MB_YESNOCANCEL        0x00000003
#define MB_YESNO              0x00000004
#define MB_RETRYCANCEL        0x00000005
#define MB_ICONHAND           0x00000010
#define MB_ICONQUESTION       0x00000020
#define MB_ICONEXCLAMATION    0x00000030
#define MB_ICONASTERISK       0x00000040
#define MB_ICONWARNING        MB_ICONEXCLAMATION
#define MB_ICONERROR          MB_ICONHAND
#define MB_ICONINFORMATION    MB_ICONASTERISK
#define MB_DEFBUTTON1         0x00000000
#define MB_DEFBUTTON2         0x00000100
#define MB_DEFBUTTON3         0x00000200
#define MB_TOPMOST            0x00040000

#define IDOK      1
#define IDCANCEL  2
#define IDABORT   3
#define IDRETRY   4
#define IDIGNORE  5
#define IDYES     6
#define IDNO      7

/* ------------------------------------------------------------------ */
/* System metrics (SM_)                                                */
/* ------------------------------------------------------------------ */
#define SM_CXSCREEN          0
#define SM_CYSCREEN          1
#define SM_CXVSCROLL         2
#define SM_CYHSCROLL         3
#define SM_CYCAPTION         4
#define SM_CXBORDER          5
#define SM_CYBORDER          6
#define SM_CXDLGFRAME        7
#define SM_CYDLGFRAME        8
#define SM_CYVTHUMB          9
#define SM_CXHTHUMB          10
#define SM_CXICON            11
#define SM_CYICON            12
#define SM_CXCURSOR          13
#define SM_CYCURSOR          14
#define SM_CYMENU            15
#define SM_CXFULLSCREEN      16
#define SM_CYFULLSCREEN      17
#define SM_CYKANJIWINDOW     18
#define SM_MOUSEPRESENT      19
#define SM_CYVSCROLL         20
#define SM_CXHSCROLL         21
#define SM_DEBUG             22
#define SM_SWAPBUTTON        23
#define SM_CXMIN             28
#define SM_CYMIN             29
#define SM_CXSIZE            30
#define SM_CYSIZE            31
#define SM_CXFRAME           32
#define SM_CYFRAME           33
#define SM_CXMINTRACK        34
#define SM_CYMINTRACK        35
#define SM_CXDOUBLECLK       36
#define SM_CYDOUBLECLK       37
#define SM_CXICONSPACING     38
#define SM_CYICONSPACING     39
#define SM_MENUDROPALIGNMENT 40
#define SM_PENWINDOWS        41
#define SM_DBCSENABLED       42
#define SM_CMOUSEBUTTONS     43
#define SM_CXFIXEDFRAME      SM_CXDLGFRAME
#define SM_CYFIXEDFRAME      SM_CYDLGFRAME
#define SM_CXSIZEFRAME       SM_CXFRAME
#define SM_CYSIZEFRAME       SM_CYFRAME
#define SM_SECURE            44
#define SM_CXEDGE            45
#define SM_CYEDGE            46
#define SM_CXMINIMIZED       47
#define SM_CYMINIMIZED       48
#define SM_CXSMICON          49
#define SM_CYSMICON          50
#define SM_CYSMCAPTION       51
#define SM_CXSMSIZE          52
#define SM_CYSMSIZE          53
#define SM_CXMENUSIZE        54
#define SM_CYMENUSIZE        55
#define SM_ARRANGE           56
#define SM_CXMINIMUMSPACING  57
#define SM_CYMINIMUMSPACING  58
#define SM_CXMAXIMIZED       59
#define SM_CYMAXIMIZED       60
#define SM_CXMAXTRACK        61
#define SM_CYMAXTRACK        62
#define SM_CXICONMETRICS     63
#define SM_CYICONMETRICS     64
#define SM_CXWORKAREA        71
#define SM_CYWORKAREA        72
#define SM_CXSMICON          49
#define SM_CYSMICON          50

/* ------------------------------------------------------------------ */
/* GetSysColor indices (COLOR_)                                        */
/* ------------------------------------------------------------------ */
#define COLOR_SCROLLBAR          0
#define COLOR_BACKGROUND         1
#define COLOR_ACTIVECAPTION      2
#define COLOR_INACTIVECAPTION    3
#define COLOR_MENU               4
#define COLOR_WINDOW             5
#define COLOR_WINDOWFRAME        6
#define COLOR_MENUTEXT           7
#define COLOR_WINDOWTEXT         8
#define COLOR_CAPTIONTEXT        9
#define COLOR_ACTIVEBORDER       10
#define COLOR_INACTIVEBORDER     11
#define COLOR_APPWORKSPACE       12
#define COLOR_HIGHLIGHT          13
#define COLOR_HIGHLIGHTTEXT      14
#define COLOR_BTNFACE            15
#define COLOR_BTNSHADOW          16
#define COLOR_GRAYTEXT           17
#define COLOR_BTNTEXT            18
#define COLOR_INACTIVECAPTIONTEXT 19
#define COLOR_BTNHIGHLIGHT       20

/* ------------------------------------------------------------------ */
/* GetWindowLong / class-style flags (GWL_ / CS_)                      */
/* ------------------------------------------------------------------ */
#define GWL_STYLE     (-16)
#define GWL_EXSTYLE   (-20)
#define GWL_HINSTANCE (-6)
#define GWL_HWNDPARENT (-8)
#define GWL_ID        (-12)
#define GWL_USERDATA  (-21)
#define GWL_WNDPROC   (-4)

#define CS_VREDRAW        0x0001
#define CS_HREDRAW        0x0002
#define CS_DBLCLKS        0x0008
#define CS_OWNDC          0x0020
#define CS_CLASSDC        0x0040
#define CS_PARENTDC       0x0080
#define CS_NOCLOSE        0x0200
#define CS_SAVEBITS       0x0800
#define CS_BYTEALIGNCLIENT 0x1000
#define CS_GLOBALCLASS    0x4000

/* ------------------------------------------------------------------ */
/* Device-context coordinates / window origin (CW_ / PRF_)             */
/* ------------------------------------------------------------------ */
#define CW_USEDEFAULT ((int)0x80000000)

/* ------------------------------------------------------------------ */
/* Struct types                                                        */
/* ------------------------------------------------------------------ */

/* MSG -- message queue entry. Layout matches winuser.h on LLP64. */
typedef struct {
    HWND   hwnd;
    UINT   message;
    WPARAM wParam;
    LPARAM lParam;
    DWORD  time;
    POINT  pt;
} MSG;

/* PAINTSTRUCT -- BeginPaint/EndPaint context. */
typedef struct {
    HDC  hdc;
    BOOL fErase;
    RECT rcPaint;
    BOOL fRestore;
    BOOL fIncUpdate;
    BYTE rgbReserved[32];
} PAINTSTRUCT;

/* Window-class callback: LRESULT CALLBACK WndProc(HWND,UINT,WPARAM,LPARAM). */
typedef LRESULT (*WNDPROC)(HWND, UINT, WPARAM, LPARAM);

/* WNDCLASS -- classic window class. */
typedef struct {
    UINT     style;
    WNDPROC  lpfnWndProc;
    int      cbClsExtra;
    int      cbWndExtra;
    HINSTANCE hInstance;
    HICON    hIcon;
    HCURSOR  hCursor;
    HBRUSH   hbrBackground;
    LPCSTR   lpszMenuName;
    LPCSTR   lpszClassName;
} WNDCLASS;

/* WNDCLASSEX -- extended window class. */
typedef struct {
    UINT     cbSize;
    UINT     style;
    WNDPROC  lpfnWndProc;
    int      cbClsExtra;
    int      cbWndExtra;
    HINSTANCE hInstance;
    HICON    hIcon;
    HCURSOR  hCursor;
    HBRUSH   hbrBackground;
    LPCSTR   lpszMenuName;
    LPCSTR   lpszClassName;
    HICON    hIconSm;
} WNDCLASSEX;

/* ------------------------------------------------------------------ */
/* user32 functions                                                    */
/* ------------------------------------------------------------------ */

/* System metrics / colours */
extern int    GetSystemMetrics(int index), user32;
extern DWORD  GetSysColor(int index), user32;
extern UINT   GetDoubleClickTime(void), user32;

/* Message boxes */
extern int    MessageBoxA(HWND parent, LPCSTR text, LPCSTR caption, UINT flags), user32;
extern int    MessageBoxW(HWND parent, LPCWSTR text, LPCWSTR caption, UINT flags), user32;

/* Window enumeration / text */
extern HWND   FindWindowA(LPCSTR cls, LPCSTR title), user32;
extern HWND   FindWindowExA(HWND parent, HWND after, LPCSTR cls, LPCSTR title), user32;
extern int    GetWindowTextA(HWND h, LPSTR buf, int n), user32;
extern int    GetWindowTextLengthA(HWND h), user32;
extern BOOL   SetWindowTextA(HWND h, LPCSTR text), user32;
extern HWND   GetForegroundWindow(void), user32;
extern HWND   GetDesktopWindow(void), user32;
extern HWND   GetParent(HWND h), user32;
extern BOOL   IsWindow(HWND h), user32;
extern BOOL   EnableWindow(HWND h, BOOL enable), user32;
extern BOOL   IsWindowEnabled(HWND h), user32;
extern BOOL   IsWindowVisible(HWND h), user32;
extern HWND   SetFocus(HWND h), user32;
extern HWND   GetFocus(void), user32;

/* Visibility / position / size */
extern BOOL   ShowWindow(HWND h, int cmd), user32;
extern BOOL   UpdateWindow(HWND h), user32;
extern BOOL   GetClientRect(HWND h, LPRECT rc), user32;
extern BOOL   GetWindowRect(HWND h, LPRECT rc), user32;
extern BOOL   MoveWindow(HWND h, int x, int y, int w, int h, BOOL repaint), user32;
extern BOOL   InvalidateRect(HWND h, LPRECT rc, BOOL erase), user32;
extern BOOL   SetWindowPos(HWND h, HWND after, int x, int y, int w, int h, UINT flags), user32;
extern BOOL   ScreenToClient(HWND h, LPPOINT pt), user32;
extern BOOL   ClientToScreen(HWND h, LPPOINT pt), user32;
extern BOOL   SetCursorPos(int x, int y), user32;
extern BOOL   GetCursorPos(LPPOINT pt), user32;
extern HWND   WindowFromPoint(POINT pt), user32;

/* Messages */
extern LRESULT SendMessageA(HWND h, UINT msg, WPARAM wp, LPARAM lp), user32;
extern BOOL   PostMessageA(HWND h, UINT msg, WPARAM wp, LPARAM lp), user32;
extern BOOL   PostQuitMessage(int exitCode), user32;
extern BOOL   GetMessageA(MSG *msg, HWND h, UINT min, UINT max), user32;
extern BOOL   PeekMessageA(MSG *msg, HWND h, UINT min, UINT max, UINT remove), user32;
extern BOOL   TranslateMessage(const MSG *msg), user32;
extern LRESULT DispatchMessageA(const MSG *msg), user32;
extern LRESULT DefWindowProcA(HWND h, UINT msg, WPARAM wp, LPARAM lp), user32;
extern LRESULT CallWindowProcA(WNDPROC prev, HWND h, UINT msg, WPARAM wp, LPARAM lp), user32;

/* Timers (WM_TIMER is dispatched through the message loop) */
extern UINT_PTR SetTimer(HWND h, UINT_PTR id, UINT ms, void *proc), user32;
extern BOOL     KillTimer(HWND h, UINT_PTR id), user32;

/* Window classes */
extern WORD   RegisterClassA(const WNDCLASS *cls), user32;
extern WORD   RegisterClassExA(const WNDCLASSEX *cls), user32;
extern BOOL   UnregisterClassA(LPCSTR name, HINSTANCE h), user32;
extern ATOM   GlobalAddAtomA(LPCSTR name), user32;
extern ATOM   GlobalFindAtomA(LPCSTR name), user32;

/* Window creation: 12 args -- the longest Win32 API goc supports (maxArgs). */
extern HWND   CreateWindowExA(DWORD exStyle, LPCSTR cls, LPCSTR name, DWORD style,
                       int x, int y, int w, int h,
                       HWND parent, HMENU menu, HINSTANCE inst, LPVOID param), user32;
extern HWND   CreateWindowA(LPCSTR cls, LPCSTR name, DWORD style,
                     int x, int y, int w, int h,
                     HWND parent, HMENU menu, HINSTANCE inst, LPVOID param), user32;
extern HWND   DestroyWindow(HWND h), user32;
extern int    GetWindowLongA(HWND h, int index), user32;
extern int    SetWindowLongA(HWND h, int index, int value), user32;
extern LONG_PTR GetWindowLongPtrA(HWND h, int index), user32;
extern LONG_PTR SetWindowLongPtrA(HWND h, int index, LONG_PTR value), user32;

/* Cursors and icons */
extern HCURSOR LoadCursorA(HINSTANCE h, LPCSTR name), user32;
extern HICON  LoadIconA(HINSTANCE h, LPCSTR name), user32;
extern HCURSOR SetCursor(HCURSOR cur), user32;

/* DC access */
extern HDC    GetDC(HWND h), user32;
extern HDC    GetWindowDC(HWND h), user32;
extern int    ReleaseDC(HWND h, HDC dc), user32;
extern HDC    BeginPaint(HWND h, PAINTSTRUCT *ps), user32;
extern BOOL   EndPaint(HWND h, const PAINTSTRUCT *ps), user32;

/* Drawing / painting */
extern BOOL   DrawTextA(HDC dc, LPCSTR text, int len, LPRECT rc, UINT fmt), user32;
extern BOOL   FillRect(HDC dc, LPRECT rc, HBRUSH brush), user32;
extern BOOL   FrameRect(HDC dc, LPRECT rc, HBRUSH brush), user32;
extern BOOL   InvertRect(HDC dc, LPRECT rc), user32;
extern BOOL   InflateRect(LPRECT rc, int dx, int dy), user32;
extern BOOL   SetRect(LPRECT rc, int l, int t, int r, int b), user32;
extern int    DrawTextExA(HDC dc, LPSTR text, int len, LPRECT rc, UINT fmt, LPVOID prm), user32;
extern BOOL   DrawIcon(HDC dc, int x, int y, HICON icon), user32;
extern BOOL   DrawIconEx(HDC dc, int x, int y, HICON icon, int w, int h,
                  UINT step, HBRUSH brush, UINT flags), user32;
extern BOOL   SetLayeredWindowAttributes(HWND h, COLORREF key, BYTE alpha, DWORD flags), user32;

#endif /* GOC_WINUSER_H */
