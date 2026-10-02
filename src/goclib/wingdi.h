#ifndef GOC_WINGDI_H
#define GOC_WINGDI_H

#include <windef.h>

/* goc wingdi.h -- gdi32 API surface: stock objects, device contexts,
 * raster ops and the bitmap info struct. Included by <windows.h>.
 * Every function is a gdi32 import declared inline, e.g.
 *     extern HDC GetDC(HWND), gdi32;
 */

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

typedef struct {
    BITMAPINFOHEADER bmiHeader;
} BITMAPINFO;

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
extern HBITMAP CreateCompatibleBitmap(HDC dc, int w, int h), gdi32;
extern BOOL    DeleteDC(HDC dc), gdi32;
extern HDC     CreateDC(LPCSTR driver, LPCSTR device, LPCSTR output, LPVOID init), gdi32;
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

#endif /* GOC_WINGDI_H */
