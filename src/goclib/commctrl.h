#ifndef GOC_COMMCTRL_H
#define GOC_COMMCTRL_H

#include <windef.h>
#include <winuser.h>
#include <wingdi.h>

/* goc commctrl.h -- common controls (comctl32): list-view, toolbars, status
 * bars, imagelist. Layouts follow the Win64 SDK. */

typedef HANDLE HIMAGELIST;
typedef unsigned int *PUINT;
typedef unsigned int *LPUINT;

/* ListView column / item descriptors. */
typedef struct {
    UINT   mask;
    int    cx;
    LPCWSTR pszText;
    HANDLE hbm;
    int    cchTextMax;
    int    fmt;
    int    iSubItem;
    int    iImage;
    int    iOrder;
    int    cxMin;
    int    cxDefault;
    int    cxIdeal;
} LVCOLUMNW;

typedef struct {
    UINT   mask;
    int    iItem;
    int    iSubItem;
    UINT   state;
    UINT   stateMask;
    LPCWSTR pszText;
    int    cchTextMax;
    int    iImage;
    LPARAM lParam;
    int    iIndent;
    int    iGroupId;
    UINT   cColumns;
    PUINT  puColumns;
    int   *piColFmt;
    int    iGroup;
} LVITEMW;

typedef struct {
    DWORD dwSize;
    DWORD dwICC;
} INITCOMMONCONTROLSEX;

/* Control window-class names. */
#define WC_LISTVIEWW    L"SysListView32"
#define WC_TREEVIEWW    L"SysTreeView32"
#define WC_TOOLBARW     L"ToolbarWindow32"
#define WC_HEADERW      L"SysHeader32"
#define WC_STATUSBARW    L"msctls_statusbar32"
#define STATUSCLASSNAMEW WC_STATUSBARW
#define TOOLBARCLASSNAMEW WC_TOOLBARW

/* ListView column formats (LVCFMT_*), stored in LVCOLUMNW.fmt. The low 2
 * bits carry the alignment; the rest are text-behaviour flags. */
#define LVCFMT_LEFT         0x0000
#define LVCFMT_RIGHT        0x0001
#define LVCFMT_CENTER       0x0002
#define LVCFMT_COLLAPSE     0x0020
#define LVCFMT_SPLITBUTTONS 0x0040
#define LVCFMT_HTML         0x0200
#define LVCFMT_SORTUP       0x0800
#define LVCFMT_SORTDOWN     0x1000

/* ListView column mask (LVCFMT_*), item mask (LVIF_*), state (LVIS_*). */
#define LVCF_FMT        0x0001
#define LVCF_WIDTH      0x0002
#define LVCF_TEXT       0x0004
#define LVCF_SUBITEM    0x0008
#define LVCF_IMAGE      0x0010
#define LVCF_ORDER      0x0020

#define LVIF_TEXT       0x0001
#define LVIF_IMAGE      0x0002
#define LVIF_PARAM      0x0004
#define LVIF_STATE      0x0008
#define LVIF_INDENT     0x0010
#define LVIF_NORECOMPUTE 0x0800
#define LVIF_GROUPID    0x0100
#define LVIF_COLUMNS    0x0200

#define LVIS_FOCUSED    0x0001
#define LVIS_SELECTED   0x0002
#define LVIS_CUT        0x0004
#define LVIS_DROPHILITED 0x0008
#define LVIS_OVERLAYMASK 0x0F00
#define LVIS_STATEIMAGEMASK 0xF000

/* ListView styles (LVS_*) and extended styles (LVS_EX_*). */
#define LVS_ICON        0x0000
#define LVS_REPORT      0x0001
#define LVS_SMALLICON   0x0002
#define LVS_LIST        0x0003
#define LVS_TYPEMASK    0x0003
#define LVS_SINGLESEL   0x0004
#define LVS_SHOWSELALWAYS 0x0008
#define LVS_SORTASCENDING 0x0010
#define LVS_SORTDESCENDING 0x0020
#define LVS_SHAREIMAGELISTS 0x0040
#define LVS_NOLABELWRAP 0x0080
#define LVS_AUTOARRANGE 0x0100
#define LVS_EDITLABELS  0x0200
#define LVS_OWNERDATA   0x1000
#define LVS_NOSCROLL    0x2000
#define LVS_TYPESTYLEMASK 0xfc00
#define LVS_ALIGNTOP    0x0000
#define LVS_ALIGNLEFT   0x0800
#define LVS_ALIGNMASK   0x0c00
#define LVS_OWNERDRAWFIXED 0x0400
#define LVS_NOCOLUMNHEADER 0x4000
#define LVS_NOSORTHEADER 0x8000

#define LVS_EX_GRIDLINES       0x00000001
#define LVS_EX_SUBITEMIMAGES   0x00000002
#define LVS_EX_CHECKBOXES      0x00000004
#define LVS_EX_TRACKSELECT     0x00000008
#define LVS_EX_HEADERDRAGDROP  0x00000010
#define LVS_EX_FULLROWSELECT   0x00000020
#define LVS_EX_ONECLICKACTIVATE 0x00000040
#define LVS_EX_TWOCLICKACTIVATE 0x00000080
#define LVS_EX_FLATSB          0x00000100
#define LVS_EX_REGIONAL        0x00000200
#define LVS_EX_INFOTIP         0x00000400
#define LVS_EX_UNDERLINEHOT    0x00000800
#define LVS_EX_DOUBLEBUFFER    0x00010000

/* ListView messages (LVM_*). */
#define LVM_FIRST       0x1000
#define LVM_GETITEMW            (LVM_FIRST + 75)   /* 0x104B */
#define LVM_SETITEMW            (LVM_FIRST + 76)   /* 0x104C */
#define LVM_INSERTITEMW         (LVM_FIRST + 77)   /* 0x104D */
#define LVM_DELETEITEM           (LVM_FIRST + 8)   /* 0x1008 */
#define LVM_DELETEALLITEMS       (LVM_FIRST + 9)   /* 0x1009 */
#define LVM_GETNEXTITEM          (LVM_FIRST + 12)  /* 0x100C */
#define LVM_SETITEMSTATE         (LVM_FIRST + 43)  /* 0x102B */
#define LVM_SETITEMTEXTW         (LVM_FIRST + 119) /* 0x1077 */
#define LVM_SETCOLUMNWIDTH       (LVM_FIRST + 30)  /* 0x101E */
#define LVM_INSERTCOLUMNW        (LVM_FIRST + 97)  /* 0x1061 */
#define LVM_SETEXTENDEDLISTVIEWSTYLE (LVM_FIRST + 54) /* 0x1036 */
#define LVM_GETEXTENDEDLISTVIEWSTYLE (LVM_FIRST + 55) /* 0x1037 */
#define LVM_ENSUREVISIBLE        (LVM_FIRST + 19)  /* 0x1013 */
#define LVM_GETITEMCOUNT         (LVM_FIRST + 4)   /* 0x1004 */
#define LVM_GETITEMSTATE         (LVM_FIRST + 44)  /* 0x102C */
#define LVM_GETSELECTEDCOUNT     (LVM_FIRST + 50)  /* 0x1032 */
#define LVM_SETITEMPOSITION      (LVM_FIRST + 15)  /* 0x100F */
#define LVM_REDRAWITEMS          (LVM_FIRST + 21)  /* 0x1015 */
#define LVM_SCROLL               (LVM_FIRST + 20)  /* 0x1014 */
#define LVM_SETBKCOLOR           (LVM_FIRST + 1)   /* 0x1001 */
#define LVM_SETTEXTCOLOR         (LVM_FIRST + 36)  /* 0x1024 */
#define LVM_SETTEXTBKCOLOR       (LVM_FIRST + 38)  /* 0x1026 */

#define LVNI_ALL         0x0000
#define LVNI_FOCUSED     0x0001
#define LVNI_SELECTED    0x0002
#define LVNI_CUT         0x0004
#define LVNI_DROPHILITED 0x0008
#define LVNI_ABOVE       0x0100
#define LVNI_BELOW       0x0200
#define LVNI_TOLEFT      0x0400
#define LVNI_TORIGHT     0x0800

/* Notification codes (WM_NOTIFY -> NMHDR.code). */
#define NM_FIRST         (0U)
#define NM_LAST          (0U - 99)
#define NM_OUTOFMEMORY   (NM_FIRST - 1)
#define NM_CLICK         (NM_FIRST - 2)
#define NM_DBLCLK        (NM_FIRST - 3)
#define NM_RETURN        (NM_FIRST - 4)
#define NM_RCLICK        (NM_FIRST - 5)
#define NM_RDBLCLK       (NM_FIRST - 6)
#define NM_SETFOCUS      (NM_FIRST - 7)
#define NM_KILLFOCUS     (NM_FIRST - 8)
#define LVN_FIRST        (0U - 100)
#define LVN_ITEMCHANGING  (LVN_FIRST - 0)
#define LVN_ITEMCHANGED   (LVN_FIRST - 1)
#define LVN_INSERTITEM    (LVN_FIRST - 2)
#define LVN_DELETEITEM    (LVN_FIRST - 3)
#define LVN_DELETEALLITEMS (LVN_FIRST - 4)
#define LVN_COLUMNCLICK   (LVN_FIRST - 8)
#define LVN_GETDISPINFO   (LVN_FIRST - 77)
#define LVN_ODCACHEHINT   (LVN_FIRST - 79)

/* INITCOMMONCONTROLSEX flags (ICC_*) and toolbar button styles (BTNS_*). */
#define ICC_LISTVIEW_CLASSES   0x00000001
#define ICC_TREEVIEW_CLASSES   0x00000002
#define ICC_BAR_CLASSES        0x00000004
#define ICC_TAB_CLASSES        0x00000008
#define ICC_UPDOWN_CLASS       0x00000010
#define ICC_PROGRESS_CLASS     0x00000020
#define ICC_HOTKEY_CLASS       0x00000040
#define ICC_ANIMATE_CLASS      0x00000080
#define ICC_WIN95_CLASSES      0x000000FF
#define ICC_COOL_CLASSES       0x00000400
#define ICC_USEREX_CLASSES     0x00000200
#define ICC_STANDARD_CLASSES   0x00004000

#define BTNS_BUTTON    0x0000
#define BTNS_SEP       0x0001
#define BTNS_CHECK     0x0002
#define BTNS_GROUP     0x0004
#define BTNS_CHECKGROUP 0x0006
#define BTNS_DROPDOWN  0x0008
#define BTNS_AUTOSIZE  0x0010
#define BTNS_NOPREFIX  0x0020
#define BTNS_SHOWTEXT  0x0040
#define BTNS_WHOLEDROPDOWN 0x0080

/* Status bar messages. Note these are window messages, not the SB_* scroll
 * orientation codes in winuser.h -- same prefix, different header. */
#define SB_SETTEXTW        (WM_USER + 6)      /* 0x0406 */
#define SB_SETTEXT         (WM_USER + 4)      /* ANSI */
#define SB_SETPARTS        (WM_USER + 4)      /* 0x0404 */
#define SB_GETPARTS        (WM_USER + 6)      /* 0x0406 */
#define SB_GETTEXTW        (WM_USER + 7)      /* 0x0407 */
#define SB_GETTEXTLENGTHW  (WM_USER + 8)      /* 0x0408 */
#define SB_GETRECT         (WM_USER + 10)     /* 0x040A */
#define SB_SETTEXTW_PART   (WM_USER + 4)      /* 0x0404, wParam = part idx */

/* Toolbar button descriptor. The real SDK's TBBUTTON grew over time and the
 * control is told which one is in use via TB_BUTTONSTRUCTSIZE; this is the
 * 32-bit-era layout, which is what code that does not target TB_GETBUTTONSIZE
 * expects. All members are naturally aligned, giving 32 bytes on Win64. */
typedef struct {
    int     iBitmap;       /* index into the toolbar image list, or I_IMAGENONE */
    int     idCommand;     /* command id sent back on click, or 0 for a spacer */
    BYTE    fsState;       /* TBSTATE_* below */
    BYTE    fsStyle;       /* BTNS_* above */
    /* the 16 reserved bytes are what the original struct reserved for a
     * pointer to a parallel state bitmap; goc has no need for them but the
     * field must exist for the layout to match */
    DWORD   bReserved[2];
    DWORD_PTR dwData;      /* app-private, passed back via TB_GETBUTTON */
    INT_PTR iString;       /* index into the toolbar string pool */
} TBBUTTON, *PTBBUTTON;

#define I_IMAGENONE        (-2)

/* Button state (TBSTATE_*). */
#define TBSTATE_CHECKED       0x01
#define TBSTATE_PRESSED       0x02
#define TBSTATE_ENABLED       0x04
#define TBSTATE_HIDDEN        0x08
#define TBSTATE_INDETERMINATE 0x10
#define TBSTATE_WRAP          0x20
#define TBSTATE_ELLIPSES      0x40
#define TBSTATE_MARKED        0x80

/* Toolbar extended styles (TBSTYLE_*). Each is an alias for the matching
 * WS_EX_* value, which is why the high bit is set -- they are OR-ed into the
 * window's exStyle. */
#define TBSTYLE_TOOLTIPS   0x10000000
#define TBSTYLE_LIST       0x10000008
#define TBSTYLE_ALIGNSBOTTOM 0x00000003
#define TBSTYLE_FLAT       0x00002000
#define TBSTYLE_TRANSPARENT 0x00010000
#define TBSTYLE_EX_DRAWDDL  0x00040000
#define TBSTYLE_EX_MIXEDBUTTONS 0x00080000
#define TBSTYLE_EX_DOUBLEBUFFER 0x80000000

/* Toolbar messages (TB_). */
#define TB_BUTTONSTRUCTSIZE  (WM_USER + 44)
#define TB_AUTOSIZE          (WM_USER + 83)
#define TB_SETIMAGELIST      (WM_USER + 48)
#define TB_GETIMAGELIST      (WM_USER + 89)
#define TB_GETBUTTON         (WM_USER + 28)
#define TB_BUTTONCOUNT       (WM_USER + 5)
#define TB_GETBUTTONINFOW    (WM_USER + 61)
#define TB_SETBUTTONINFOW    (WM_USER + 62)
#define TB_ADDBUTTONSW       (WM_USER + 68)
#define TB_INSERTBUTTONW     (WM_USER + 79)
#define TB_SETSTATE          (WM_USER + 24)
#define TB_GETSTATE          (WM_USER + 25)
#define TB_SETTOOLTIPS       (WM_USER + 12)
#define TB_SETCURFOCUS       (WM_USER + 48)
#define TB_GETTEXTROWS       (WM_USER + 91)
#define TB_GETMAXSIZE        (WM_USER + 73)
#define TB_GETRECT           (WM_USER + 51)
#define TB_ENABLEBUTTON      (WM_USER + 21)
#define TB_PRESSBUTTON       (WM_USER + 3)
#define TB_GETVERSION        (WM_USER + 96)

/* Imagelist creation flags (ILC_*). */
#define ILC_COLOR32   0x00000020
#define ILC_COLOR24   0x00000018
#define ILC_COLOR16   0x00000010
#define ILC_COLOR8    0x00000008
#define ILC_COLOR4    0x00000004
#define ILC_COLOR2    0x00000002
#define ILC_COLOR1    0x00000001
#define ILC_MASK      0x00000001
#define ILC_COLORDDB  0x000000FE

extern BOOL InitCommonControlsEx(const INITCOMMONCONTROLSEX *icc), comctl32;
extern HIMAGELIST ImageList_Create(int cx, int cy, UINT flags, int cInitial, int cGrow), comctl32;
extern BOOL ImageList_Destroy(HIMAGELIST h), comctl32;
extern int  ImageList_Add(HIMAGELIST h, HBITMAP bmp, HBITMAP mask), comctl32;
extern int  ImageList_AddMasked(HIMAGELIST h, HBITMAP bmp, COLORREF mask), comctl32;
extern int  ImageList_GetImageCount(HIMAGELIST h), comctl32;
extern BOOL ImageList_ReplaceIcon(HIMAGELIST h, int i, HICON icon), comctl32;

#endif /* GOC_COMMCTRL_H */
