#ifndef GOC_WINGDI_H
#define GOC_WINGDI_H

#include <windef.h>

/* goc wingdi.h -- gdi32 API surface: stock objects, device contexts,
 * raster ops and the bitmap info struct. Included by <windows.h>.
 * Every function is a gdi32 import declared inline, e.g.
 *     extern HDC GetDC(HWND), gdi32;
 */

/* COLORREF packs R/G/B into the low 24 bits as 0x00BBGGRR (little-endian
 * channel order, matching Windows). RGB() is the standard construction macro. */
#ifndef RGB
#define RGB(r, g, b) ((COLORREF)(((r) & 0xFF) | (((g) & 0xFF) << 8) | (((b) & 0xFF) << 16)))
#endif

/* ------------------------------------------------------------------ */
/* Stock objects (GetStockObject)                                      */
/* ------------------------------------------------------------------ */
#define WHITE_BRUSH          0
#define LTGRAY_BRUSH         1
#define GRAY_BRUSH           2
#define DKGRAY_BRUSH         3
#define BLACK_BRUSH          4
#define NULL_BRUSH           5
#define WHITE_PEN            6
#define BLACK_PEN            7
#define NULL_PEN             8
#define OEM_FIXED_FONT       10
#define ANSI_FIXED_FONT      11
#define ANSI_VAR_FONT        12
#define SYSTEM_FONT          13
#define DEVICE_DEFAULT_FONT  14
#define DEFAULT_PALETTE      15
#define SYSTEM_FIXED_FONT    16
#define DEFAULT_GUI_FONT     17

/* Object-type tags returned by GetObject and accepted by GetCurrentObject /
 * EnumObjects. Each identifies which of the per-type header structs the
 * buffer should hold (BITMAP, LOGFONTW, ...). */
#define OBJ_PEN         1
#define OBJ_BRUSH       2
#define OBJ_DC          3
#define OBJ_METADC      4
#define OBJ_PAL         5
#define OBJ_FONT        6
#define OBJ_BITMAP      7
#define OBJ_REGION      8
#define OBJ_METAFILE    9
#define OBJ_MEMDC       10
#define OBJ_EXTPEN      11
#define OBJ_ENHMETADC   12
#define OBJ_ENHMETAFILE 13
#define OBJ_COLORSPACE  14
#define GDI_MIN_OBJ_TYPE OBJ_PEN
#define GDI_MAX_OBJ_TYPE OBJ_COLORSPACE

/* GetObjectType tags a GDI object's real type; HGDI_ERROR is the sentinel
 * GetObject/GetCurrentObject return when the handle is invalid. */
extern DWORD GetObjectType(HGDIOBJ h), gdi32;
#define HGDI_ERROR ((HGDIOBJ)0x80000001)

/* ------------------------------------------------------------------ */
/* Background / text modes                                             */
/* ------------------------------------------------------------------ */
#define OPAQUE        2
#define TRANSPARENT   1

/* ------------------------------------------------------------------ */
/* Raster operations (ROP2 / ternary ROP codes)                        */
/* ------------------------------------------------------------------ */
#define R2_BLACK       1
#define R2_NOTMERGEPEN 2
#define R2_MASKNOTPEN  3
#define R2_NOTCOPYPEN  4
#define R2_MASKPENNOT  5
#define R2_NOT         6
#define R2_XORPEN      7
#define R2_NOTMASKPEN  8
#define R2_MASKPEN     9
#define R2_NOTXORPEN   10
#define R2_NOP         11
#define R2_MERGENOTPEN 12
#define R2_COPYPEN     13
#define R2_MERGEPENNOT 14
#define R2_MERGEPEN    15
#define R2_WHITE       16

#define SRCCOPY         0x00CC0020
#define SRCPAINT        0x00EE0086
#define SRCAND          0x008800C6
#define SRCINVERT       0x00660046
#define SRCERASE        0x00440328
#define NOTSRCCOPY      0x00330008
#define NOTSRCERASE     0x001100A6
#define MERGECOPY       0x00C000CA
#define MERGEPAINT      0x00BB0226
#define PATCOPY         0x00F00021
#define PATPAINT        0x00FB0A09
#define PATINVERT       0x005A0049
#define DSTINVERT       0x00550009
#define BLACKNESS       0x00000042
#define WHITENESS       0x00FF0062

/* ------------------------------------------------------------------ */
/* GetDeviceCaps indices                                               */
/* ------------------------------------------------------------------ */
#define DRIVERVERSION   0
#define TECHNOLOGY      2
#define HORZSIZE        4
#define VERTSIZE        6
#define HORZRES         8
#define VERTRES         10
#define BITSPIXEL       12
#define PLANES          14
#define NUMBRUSHES      16
#define NUMPENS         18
#define NUMFONTS        22
#define NUMCOLORS       24
#define ASPECTX         40
#define ASPECTY         42
#define ASPECTXY        44
#define PDEVICESIZE     26
#define CLIPCAPS        36
#define SIZEPALETTE     104
#define NUMRESERVED     106
#define COLORRES        108

/* ------------------------------------------------------------------ */
/* Brush creation constants                                            */
/* ------------------------------------------------------------------ */
#define BS_SOLID        0
#define BS_NULL         1
#define BS_HOLLOW       BS_NULL
#define BS_HATCHED      2
#define BS_PATTERN      3

/* ------------------------------------------------------------------ */
/* BITMAPINFOHEADER -- DIB header layout (matches wingdi.h on LLP64).  */
/* ------------------------------------------------------------------ */
typedef struct {
    DWORD biSize;
    LONG  biWidth;
    LONG  biHeight;
    WORD  biPlanes;
    WORD  biBitCount;
    DWORD biCompression;
    DWORD biSizeImage;
    LONG  biXPelsPerMeter;
    LONG  biYPelsPerMeter;
    DWORD biClrUsed;
    DWORD biClrImportant;
} BITMAPINFOHEADER;

/* One palette entry. Declared before BITMAPINFO because that struct's
 * bmiColors member is an array of these. Note the member order is BGR, not
 * RGB: a 32-bit DIB stores pixels little-endian, so the first byte in memory
 * is the blue channel. The names match the real SDK exactly, since code that
 * walks a 32-bit DIB's pixels uses them directly. */
typedef struct {
    BYTE  rgbBlue;
    BYTE  rgbGreen;
    BYTE  rgbRed;
    BYTE  rgbReserved;
} RGBQUAD;

/* bmiColors is a variable-length array in practice (one entry per palette
 * colour, omitted entirely for BI_RGB 24/32bpp). Declaring it as a 1-element
 * array reproduces the real sizeof(BITMAPINFO) == 44, which matters because
 * callers pass the struct straight to CreateDIBSection. */
typedef struct {
    BITMAPINFOHEADER bmiHeader;
    RGBQUAD          bmiColors[1];
} BITMAPINFO;

/* BITMAP is the GDI's own bitmap object header -- the thing a GetObject on a
 * HBITMAP fills in. It is NOT the DIB: the pixels live in the GDI's own
 * storage, so only the dimensions and the handle are visible here. The struct
 * is 4-byte aligned with a trailing WORD bmBytesPixel that older code reads,
 * which brings the real size to 32 bytes on Win64. */
typedef struct {
    LONG  bmType;          /* 0 = bitmap */
    LONG  bmWidth;
    LONG  bmHeight;
    WORD  bmWidthBytes;
    WORD  bmPlanes;
    WORD  bmBitsPixel;
    void *bmBits;          /* points into GDI memory; not a client pointer */
} BITMAP;

#define BI_RGB       0
#define BI_RLE8      1
#define BI_RLE4      2
#define BI_BITFIELDS 3

/* ------------------------------------------------------------------ */
/* gdi32 functions                                                     */
/* ------------------------------------------------------------------ */
extern HGDIOBJ GetStockObject(int obj), gdi32;
extern HGDIOBJ SelectObject(HDC dc, HGDIOBJ obj), gdi32;
extern BOOL    DeleteObject(HGDIOBJ obj), gdi32;

extern HDC     CreateCompatibleDC(HDC dc), gdi32;
extern HGDIOBJ GetCurrentObject(HDC dc, UINT type), gdi32;
extern HBITMAP CreateCompatibleBitmap(HDC dc, int w, int h), gdi32;
extern BOOL    DeleteDC(HDC dc), gdi32;
/* The device-context constructors are CreateDCA / CreateDCW; the unsuffixed
 * CreateDC is an SDK macro, not an export. */
extern HDC     CreateDCA(LPCSTR driver, LPCSTR device, LPCSTR output, LPVOID init), gdi32;
extern HDC     CreateDCW(LPCWSTR driver, LPCWSTR device, LPCWSTR output, LPVOID init), gdi32;
#define CreateDC(driver, device, output, init) \
    CreateDCA((driver), (device), (output), (init))
extern int     GetDeviceCaps(HDC dc, int index), gdi32;

extern BOOL    SetBkColor(HDC dc, COLORREF color), gdi32;
extern COLORREF GetBkColor(HDC dc), gdi32;
extern BOOL    SetTextColor(HDC dc, COLORREF color), gdi32;
extern COLORREF GetTextColor(HDC dc), gdi32;
extern int     SetBkMode(HDC dc, int mode), gdi32;
extern int     GetBkMode(HDC dc), gdi32;
extern int     SetROP2(HDC dc, int rop), gdi32;
extern int     GetROP2(HDC dc), gdi32;

extern BOOL    TextOutA(HDC dc, int x, int y, LPCSTR text, int len), gdi32;
extern BOOL    LineTo(HDC dc, int x, int y), gdi32;
extern BOOL    MoveToEx(HDC dc, int x, int y, LPPOINT old), gdi32;
extern BOOL    Rectangle(HDC dc, int l, int t, int r, int b), gdi32;
extern BOOL    Ellipse(HDC dc, int l, int t, int r, int b), gdi32;
extern BOOL    PatBlt(HDC dc, int x, int y, int w, int h, DWORD rop), gdi32;
extern BOOL    BitBlt(HDC dst, int x, int y, int w, int h, HDC src,
               int sx, int sy, DWORD rop), gdi32;
extern BOOL    StretchBlt(HDC dst, int x, int y, int w, int h, HDC src,
                   int sx, int sy, int sw, int sh, DWORD rop), gdi32;
extern BOOL    SetPixel(HDC dc, int x, int y, COLORREF color), gdi32;
extern COLORREF GetPixel(HDC dc, int x, int y), gdi32;
extern BOOL    RoundRect(HDC dc, int l, int t, int r, int b, int w, int h), gdi32;
extern BOOL    Polygon(HDC dc, LPPOINT pts, int count), gdi32;
extern BOOL    Polyline(HDC dc, LPPOINT pts, int count), gdi32;
extern BOOL    Arc(HDC dc, int l, int t, int r, int b, int x1, int y1, int x2, int y2), gdi32;
extern BOOL    Pie(HDC dc, int l, int t, int r, int b, int x1, int y1, int x2, int y2), gdi32;
extern BOOL    ExtTextOutA(HDC dc, int x, int y, UINT opts, LPRECT rc,
                    LPCSTR text, UINT len, LPVOID gaps), gdi32;
extern BOOL    TextOutW(HDC dc, int x, int y, LPCWSTR text, int len), gdi32;

/* ------------------------------------------------------------------ */
/* LOGFONTW -- logical font description (lfFaceName is LF_FACESIZE=32 WCHAR). */
/* ------------------------------------------------------------------ */
typedef struct {
    LONG   lfHeight;
    LONG   lfWidth;
    LONG   lfEscapement;
    LONG   lfOrientation;
    LONG   lfWeight;
    BYTE   lfItalic;
    BYTE   lfUnderline;
    BYTE   lfStrikeOut;
    BYTE   lfCharSet;
    BYTE   lfOutPrecision;
    BYTE   lfClipPrecision;
    BYTE   lfQuality;
    BYTE   lfPitchAndFamily;
    WCHAR  lfFaceName[32];
} LOGFONTW;

typedef SIZE *LPSIZE;
typedef LOGFONTW *LPLOGFONTW;

/* ------------------------------------------------------------------ */
/* Font / pen / brush creation constants                                */
/* ------------------------------------------------------------------ */
#define PS_SOLID           0
#define PS_DASH            1
#define PS_DOT             2
#define PS_DASHDOT         3
#define PS_DASHDOTDOT      4
#define PS_NULL            5
#define PS_INSIDEFRAME     6

#define FW_DONTCARE        0
#define FW_NORMAL          400
#define FW_BOLD            700

#define DEFAULT_CHARSET     1
#define ANSI_CHARSET       0
#define DEFAULT_PITCH      0
#define FF_DONTCARE        0
#define OUT_DEFAULT_PRECIS  0
#define CLIP_DEFAULT_PRECIS 0
#define DEFAULT_QUALITY     0
#define CLEARTYPE_QUALITY   5

#define DIB_RGB_COLORS     0
#define DIB_PAL_COLORS     1

/* ------------------------------------------------------------------ */
/* Object-creation gdi32 APIs                                           */
/* ------------------------------------------------------------------ */
extern HPEN    CreatePen(int style, int width, COLORREF color), gdi32;
extern HBRUSH  CreateSolidBrush(COLORREF color), gdi32;
extern HBRUSH  CreatePatternBrush(HBITMAP bmp), gdi32;
extern HFONT   CreateFontIndirectW(const LOGFONTW *lf), gdi32;
extern BOOL    GetTextExtentPoint32W(HDC dc, LPCWSTR text, int len, LPSIZE size), gdi32;
extern HBITMAP CreateDIBSection(HDC dc, const BITMAPINFO *info, UINT usage,
                        void **bits, LPVOID section, DWORD offset), gdi32;
extern int     SaveDC(HDC dc), gdi32;
extern BOOL    RestoreDC(HDC dc, int saved), gdi32;

/* GDI object interrogation and DIB transfer. GetObject{A,W} fills a
 * caller-sized buffer with the object's type-specific header -- BITMAP for a
 * HBITMAP, LOGFONTW for an HFONT -- and returns how many bytes it wrote, or 0
 * if the buffer is too small. The unsuffixed GetObject is a macro in the real
 * SDK and not an exported symbol, so the A and W entry points are declared
 * separately here and the header below spells the macro. */
extern int     GetObjectA(HGDIOBJ obj, int bufSize, LPVOID buf), gdi32;
extern int     GetObjectW(HGDIOBJ obj, int bufSize, LPVOID buf), gdi32;
extern int     GetDIBits(HDC dc, HBITMAP bmp, UINT start, UINT cLines,
                         LPVOID bits, BITMAPINFO *info, UINT usage), gdi32;
extern int     SetDIBits(HDC dc, HBITMAP bmp, UINT start, UINT cLines,
                         LPVOID bits, BITMAPINFO *info, UINT usage), gdi32;
extern HBITMAP CreateBitmap(int w, int h, UINT planes, UINT bitsPixel, LPVOID bits), gdi32;

/* GetObject is a function-like macro in the real SDK, not an export. It
 * dispatches on UNICODE, which goc always defines, so it resolves to the wide
 * entry point unconditionally -- the buffer it fills is a plain byte buffer
 * whose interpretation the caller decides from the object type. */
#define GetObject(obj, size, buf) GetObjectW((obj), (size), (buf))

#endif /* GOC_WINGDI_H */
