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
/* Virtual-key codes (VK_), as delivered in wParam of WM_KEYDOWN /      */
/* WM_CHAR and returned by GetKeyState.                                 */
/* ------------------------------------------------------------------ */
#define VK_LBUTTON          0x01
#define VK_RBUTTON          0x02
#define VK_CANCEL           0x03
#define VK_MBUTTON          0x04
#define VK_BACK             0x08
#define VK_TAB              0x09
#define VK_CLEAR            0x0C
#define VK_RETURN           0x0D
#define VK_SHIFT            0x10
#define VK_CONTROL          0x11
#define VK_MENU             0x12
#define VK_PAUSE            0x13
#define VK_CAPITAL          0x14
#define VK_ESCAPE           0x1B
#define VK_SPACE            0x20
#define VK_PRIOR            0x21
#define VK_NEXT             0x22
#define VK_END              0x23
#define VK_HOME             0x24
#define VK_LEFT             0x25
#define VK_UP               0x26
#define VK_RIGHT            0x27
#define VK_DOWN             0x28
#define VK_SELECT           0x29
#define VK_PRINT            0x2A
#define VK_EXECUTE          0x2B
#define VK_SNAPSHOT         0x2C
#define VK_INSERT           0x2D
#define VK_DELETE           0x2E
#define VK_HELP             0x2F
#define VK_0                0x30
#define VK_1                0x31
#define VK_2                0x32
#define VK_3                0x33
#define VK_4                0x34
#define VK_5                0x35
#define VK_6                0x36
#define VK_7                0x37
#define VK_8                0x38
#define VK_9                0x39
#define VK_A                0x41
#define VK_B                0x42
#define VK_C                0x43
#define VK_D                0x44
#define VK_E                0x45
#define VK_F                0x46
#define VK_G                0x47
#define VK_H                0x48
#define VK_I                0x49
#define VK_J                0x4A
#define VK_K                0x4B
#define VK_L                0x4C
#define VK_M                0x4D
#define VK_N                0x4E
#define VK_O                0x4F
#define VK_P                0x50
#define VK_Q                0x51
#define VK_R                0x52
#define VK_S                0x53
#define VK_T                0x54
#define VK_U                0x55
#define VK_V                0x56
#define VK_W                0x57
#define VK_X                0x58
#define VK_Y                0x59
#define VK_Z                0x5A
#define VK_NUMPAD0          0x60
#define VK_NUMPAD1          0x61
#define VK_NUMPAD2          0x62
#define VK_NUMPAD3          0x63
#define VK_NUMPAD4          0x64
#define VK_NUMPAD5          0x65
#define VK_NUMPAD6          0x66
#define VK_NUMPAD7          0x67
#define VK_NUMPAD8          0x68
#define VK_NUMPAD9          0x69
#define VK_MULTIPLY         0x6A
#define VK_ADD              0x6B
#define VK_SEPARATOR        0x6C
#define VK_SUBTRACT         0x6D
#define VK_DECIMAL          0x6E
#define VK_DIVIDE           0x6F
#define VK_F1               0x70
#define VK_F2               0x71
#define VK_F3               0x72
#define VK_F4               0x73
#define VK_F5               0x74
#define VK_F6               0x75
#define VK_F7               0x76
#define VK_F8               0x77
#define VK_F9               0x78
#define VK_F10              0x79
#define VK_F11              0x7A
#define VK_F12              0x7B
#define VK_F13              0x7C
#define VK_F14              0x7D
#define VK_F15              0x7E
#define VK_F16              0x7F
#define VK_NUMLOCK          0x90
#define VK_SCROLL           0x91
#define VK_OEM_1            0xBA
#define VK_OEM_PLUS         0xBB
#define VK_OEM_COMMA        0xBC
#define VK_OEM_MINUS        0xBD
#define VK_OEM_PERIOD       0xBE
#define VK_OEM_2            0xBF
#define VK_OEM_3            0xC0
#define VK_OEM_4            0xDB
#define VK_OEM_5            0xDC
#define VK_OEM_6            0xDD
#define VK_OEM_7            0xDE
#define VK_OEM8             0xDF

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

/* Extended window styles (WS_EX_), OR-ed into the exStyle argument.        */
#define WS_EX_DLGMODALFRAME  0x00000001
#define WS_EX_NOPARENTNOTIFY 0x00000004
#define WS_EX_TOPMOST        0x00000008
#define WS_EX_ACCEPTFILES    0x00000010
#define WS_EX_TRANSPARENT    0x00000020
#define WS_EX_MDICHILD       0x00000040
#define WS_EX_TOOLWINDOW     0x00000080
#define WS_EX_WINDOWEDGE     0x00000100
#define WS_EX_CLIENTEDGE     0x00000200
#define WS_EX_CONTEXTHELP    0x00000400
#define WS_EX_RIGHT          0x00001000
#define WS_EX_LEFT           0x00000000
#define WS_EX_RTLREADING     0x00002000
#define WS_EX_LEFTSCROLLBAR  0x00004000
#define WS_EX_CONTROLPARENT  0x00010000
#define WS_EX_STATICEDGE     0x00020000
#define WS_EX_APPWINDOW      0x00040000
#define WS_EX_LAYERED        0x00080000
#define WS_EX_NOINHERITLAYOUT 0x00100000
#define WS_EX_NOREDIRECTIONBITMAP 0x00200000
#define WS_EX_LAYOUTRTL      0x00400000
#define WS_EX_COMPOSITED     0x02000000
#define WS_EX_NOACTIVATE     0x08000000

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
extern ATOM   GlobalAddAtomA(LPCSTR name), kernel32;
extern ATOM   GlobalFindAtomA(LPCSTR name), kernel32;
extern ATOM   GlobalAddAtomW(LPCWSTR name), kernel32;
extern ATOM   GlobalFindAtomW(LPCWSTR name), kernel32;

/* Window creation: 12 args -- the longest Win32 API goc supports (maxArgs). */
extern HWND   CreateWindowExA(DWORD exStyle, LPCSTR cls, LPCSTR name, DWORD style,
                       int x, int y, int w, int h,
                       HWND parent, HMENU menu, HINSTANCE inst, LPVOID param), user32;
/* There is no CreateWindowA/W export on Windows: the plain CreateWindow is a
 * macro over CreateWindowEx with exStyle 0, and the SDK only ever emits the
 * Ex forms. goclib provides the macro so ported code compiles. */
#define CreateWindowA(cls, name, style, x, y, w, h, parent, menu, inst, param) \
    CreateWindowExA(0, (cls), (name), (style), (x), (y), (w), (h), \
                    (parent), (menu), (inst), (param))
#define CreateWindowW(cls, name, style, x, y, w, h, parent, menu, inst, param) \
    CreateWindowExW(0, (cls), (name), (style), (x), (y), (w), (h), \
                    (parent), (menu), (inst), (param))
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

/* ------------------------------------------------------------------ */
/* Additional window messages (WM_)                                    */
/* ------------------------------------------------------------------ */
#define WM_CONTEXTMENU      0x007B
#define WM_GETMINMAXINFO    0x0024
#define WM_SETCURSOR        0x0020
#define WM_NOTIFY           0x004E
#define WM_DROPFILES        0x0233
#define WM_NCCREATE         0x0081
#define WM_NCDESTROY        0x0082
#define WM_SETFONT          0x0030
#define WM_GETFONT          0x0031
#define WM_INITDIALOG       0x0110
#define WM_MOUSEFIRST       0x0200

/* ------------------------------------------------------------------ */
/* Scrollbar constants (SB_ / SIF_)                                     */
/* ------------------------------------------------------------------ */
#define SB_HORZ             0
#define SB_VERT             1
#define SB_CTL              2
#define SB_BOTH             3
#define SB_LINEUP           0
#define SB_LINELEFT         0
#define SB_LINEDOWN         1
#define SB_LINERIGHT        1
#define SB_PAGEUP           2
#define SB_PAGELEFT         2
#define SB_PAGEDOWN         3
#define SB_PAGERIGHT        3
#define SB_THUMBPOSITION    4
#define SB_THUMBTRACK       8
#define SB_TOP              0
#define SB_LEFT             0
#define SB_BOTTOM           1
#define SB_RIGHT            1
#define SB_LEFTCLICK        1
#define SB_RIGHTCLICK       2
#define SB_THUMB            4
#define SB_TOPLEFT          6
#define SB_TOPRIGHT         7
#define SB_BOTTOMLEFT       8
#define SB_BOTTOMRIGHT     9
#define SIF_RANGE           0x0001
#define SIF_PAGE            0x0002
#define SIF_POS             0x0004
#define SIF_DISABLENOSCROLL 0x0008
#define SIF_TRACKPOS        0x0010
#define SIF_ALL             (SIF_RANGE | SIF_PAGE | SIF_POS | SIF_TRACKPOS)

/* ------------------------------------------------------------------ */
/* Menu flags (MF_) and TrackPopupMenu flags (TPM_)                      */
/* ------------------------------------------------------------------ */
#define MF_STRING           0x00000000L
#define MF_SEPARATOR        0x00000800L
#define MF_POPUP            0x00000010L
#define MF_GRAYED           0x00000001L
#define MF_DISABLED         0x00000002L
#define MF_CHECKED          0x00000008L
#define MF_UNCHECKED         0x00000000L
#define MF_BYPOSITION       0x00000400L
#define MF_BYCOMMAND        0x00000000L

#define TPM_LEFTALIGN       0x0000
#define TPM_CENTERALIGN     0x0004
#define TPM_RIGHTALIGN      0x0008
#define TPM_TOPALIGN        0x0000
#define TPM_VCENTERALIGN    0x0010
#define TPM_BOTTOMALIGN     0x0020
#define TPM_RETURNCMD       0x0100
#define TPM_NONOTIFY        0x0080
#define TPM_LEFTBUTTON      0x0000
#define TPM_RIGHTBUTTON     0x0002

#define MAKEINTRESOURCEW(x) ((LPCWSTR)((UINT_PTR)(x)))

/* ------------------------------------------------------------------ */
/* Extra struct types                                                   */
/* ------------------------------------------------------------------ */

/* WNDCLASSEXW -- Unicode extended window class (lpsz* are wide). */
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
    LPCWSTR  lpszMenuName;
    LPCWSTR  lpszClassName;
    HICON    hIconSm;
} WNDCLASSEXW;

/* MINMAXINFO -- WM_GETMINMAXINFO payload. */
typedef struct {
    POINT ptReserved;
    POINT ptMaxSize;
    POINT ptMaxPosition;
    POINT ptMinTrackSize;
    POINT ptMaxTrackSize;
} MINMAXINFO;

/* NMHDR -- notification header prepended to every WM_NOTIFY struct. */
typedef struct {
    HWND     hwndFrom;
    UINT_PTR idFrom;
    UINT     code;
} NMHDR;

/* SCROLLINFO -- SetScrollInfo / GetScrollInfo payload. */
typedef struct {
    UINT cbSize;
    UINT fMask;
    int  nMin;
    int  nMax;
    UINT nPage;
    int  nPos;
    int  nTrackPos;
} SCROLLINFO;

/* ------------------------------------------------------------------ */
/* Unicode (W) window APIs                                              */
/* ------------------------------------------------------------------ */

/* Window classes / creation (W variants) */
extern ATOM   RegisterClassExW(const WNDCLASSEXW *cls), user32;
extern HWND   CreateWindowExW(DWORD exStyle, LPCWSTR cls, LPCWSTR name, DWORD style,
                       int x, int y, int w, int h,
                       HWND parent, HMENU menu, HINSTANCE inst, LPVOID param), user32;
extern LRESULT DefWindowProcW(HWND h, UINT msg, WPARAM wp, LPARAM lp), user32;
extern LRESULT CallWindowProcW(WNDPROC prev, HWND h, UINT msg, WPARAM wp, LPARAM lp), user32;
extern LONG_PTR GetWindowLongPtrW(HWND h, int index), user32;
extern LONG_PTR SetWindowLongPtrW(HWND h, int index, LONG_PTR value), user32;

/* Messages (W variants) */
extern LRESULT SendMessageW(HWND h, UINT msg, WPARAM wp, LPARAM lp), user32;
extern BOOL   PostMessageW(HWND h, UINT msg, WPARAM wp, LPARAM lp), user32;
extern BOOL   GetMessageW(MSG *msg, HWND h, UINT min, UINT max), user32;
extern BOOL   PeekMessageW(MSG *msg, HWND h, UINT min, UINT max, UINT remove), user32;
extern LRESULT DispatchMessageW(const MSG *msg), user32;

/* Cursors / icons (W variants) */
extern HCURSOR LoadCursorW(HINSTANCE h, LPCWSTR name), user32;
extern HICON  LoadIconW(HINSTANCE h, LPCWSTR name), user32;

/* Text / dialogs (W variants) */
extern BOOL   SetWindowTextW(HWND h, LPCWSTR text), user32;
extern int    MessageBoxW(HWND parent, LPCWSTR text, LPCWSTR caption, UINT flags), user32;
extern HICON  LoadImageW(HINSTANCE h, LPCWSTR name, UINT type, int cx, int cy, UINT fuLoad), user32;

/* LoadImage type / fuLoad selectors. */
#define IMAGE_BITMAP     0
#define IMAGE_ICON       1
#define IMAGE_CURSOR     2
#define LR_DEFAULTCOLOR  0x00000000
#define LR_MONOCHROME    0x00000001
#define LR_DIBANDDEVMAP  0x00000002
#define LR_LOADFROMFILE  0x00000010
#define LR_LOADFROMRESOURCE 0x00000008
#define LR_DEFAULTSIZE   0x00000040
#define LR_USEDEFERDC    0x00000000
#define LR_SCREEN        0x00000080

/* Menus */
extern HMENU  CreateMenu(void), user32;
extern HMENU  CreatePopupMenu(void), user32;
extern BOOL   DestroyMenu(HMENU h), user32;
extern BOOL   AppendMenuW(HMENU h, UINT flags, UINT_PTR id, LPCWSTR text), user32;
extern BOOL   SetMenu(HWND h, HMENU menu), user32;
extern BOOL   DrawMenuBar(HWND h), user32;
extern BOOL   TrackPopupMenu(HMENU h, UINT flags, int x, int y, int reserved, HWND wnd, LPVOID prc), user32;

/* Clipboard */
/* Standard clipboard formats (CF_). Custom formats must be >= CF_PRIVATEFIRST;
 * registering a string name gives a handle usable with any of the CF_ APIs. */
#define CF_TEXT             1
#define CF_BITMAP           2
#define CF_METAFILEPICT     3
#define CF_SYLK             4
#define CF_DIF              5
#define CF_TIFF             6
#define CF_OEMTEXT          7
#define CF_DIB              8
#define CF_PALETTE          9
#define CF_PENDATA          10
#define CF_RIFF             11
#define CF_WAVE             12
#define CF_UNICODETEXT      13
#define CF_ENHMETAFILE      14
#define CF_HDROP            15
#define CF_LOCALE           16
#define CF_DIBV5            17
#define CF_OWNERDISPLAY     0x80
#define CF_DSPBITMAP        0x82
#define CF_DSPTEXT          0x83
#define CF_DSPMETAFILEPICT  0x83
#define CF_DSPENHMETAFILE   0x8E
#define CF_PRIVATEFIRST     0x200
#define CF_PRIVATELAST      0x2FF
#define CF_GDIOBJFIRST      0x300
#define CF_GDIOBJLAST       0x3FF

extern BOOL   OpenClipboard(HWND h), user32;
extern BOOL   CloseClipboard(void), user32;
extern BOOL   EmptyClipboard(void), user32;
extern HANDLE SetClipboardData(UINT fmt, HANDLE data), user32;
extern HANDLE GetClipboardData(UINT fmt), user32;
extern UINT   RegisterClipboardFormatW(LPCWSTR name), user32;
extern int    CountClipboardFormats(void), user32;
extern UINT   EnumClipboardFormats(UINT fmt), user32;
extern BOOL   IsClipboardFormatAvailable(UINT fmt), user32;

/* Misc */
extern BOOL   SetForegroundWindow(HWND h), user32;
extern HWND   SetCapture(HWND h), user32;
extern BOOL   ReleaseCapture(void), user32;
extern SHORT  GetKeyState(int vkey), user32;
extern BOOL   SetScrollInfo(HWND h, int bar, const SCROLLINFO *si, BOOL redraw), user32;
extern int    GetScrollInfo(HWND h, int bar, SCROLLINFO *si), user32;
extern BOOL   InvalidateRgn(HWND h, HANDLE rgn, BOOL erase), user32;

#endif /* GOC_WINUSER_H */
