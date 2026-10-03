#!/usr/bin/env python3
"""Launch a goc-built GUI exe, find its main window, screenshot it, then close it.

Written for the goc Win32 tests: the program under test registers a real window
class and paints into WM_PAINT, so "did it actually draw?" is answered by pixels
rather than by a return value. Uses PrintWindow with PW_RENDERFULLCONTENT so
the capture works even when the window is behind others or not yet foreground.

    python tools/shot_gui.py <exe> <out.png> [--wait SEC] [--title SUBSTR]
                             [--keep-open]

Prints one line per top-level window it finds, then exits nonzero if no window
matching --title showed up.
"""

import argparse
import ctypes
import struct
import sys
import time
import zlib
from ctypes import wintypes

u = ctypes.windll.user32
g = ctypes.windll.gdi32


class BITMAPINFOHEADER(ctypes.Structure):
    _fields_ = [
        ("biSize", wintypes.DWORD), ("biWidth", wintypes.LONG),
        ("biHeight", wintypes.LONG), ("biPlanes", wintypes.WORD),
        ("biBitCount", wintypes.WORD), ("biCompression", wintypes.DWORD),
        ("biSizeImage", wintypes.DWORD), ("biXPelsPerMeter", wintypes.LONG),
        ("biYPelsPerMeter", wintypes.LONG), ("biClrUsed", wintypes.DWORD),
        ("biClrImportant", wintypes.DWORD),
    ]


def top_windows(pid):
    """(hwnd, class, title, visible) for every top-level window of `pid`."""
    found = []
    proc = ctypes.WINFUNCTYPE(ctypes.c_bool, ctypes.c_void_p, ctypes.c_void_p)

    def cb(h, _):
        wpid = ctypes.c_ulong()
        u.GetWindowThreadProcessId(h, ctypes.byref(wpid))
        if wpid.value == pid:
            cls = ctypes.create_unicode_buffer(256)
            u.GetClassNameW(h, cls, 256)
            title = ctypes.create_unicode_buffer(512)
            u.GetWindowTextW(h, title, 512)
            found.append((h, cls.value, title.value, bool(u.IsWindowVisible(h))))
        return True

    u.EnumWindows(proc(cb), 0)
    return found


def capture(hwnd):
    """Return PNG bytes for the window's client area, or None."""
    rect = wintypes.RECT()
    if not u.GetWindowRect(hwnd, ctypes.byref(rect)):
        return None
    w, h = rect.right - rect.left, rect.bottom - rect.top
    if w <= 0 or h <= 0:
        return None

    src = u.GetWindowDC(hwnd)
    mem = g.CreateCompatibleDC(src)
    bmp = g.CreateCompatibleBitmap(src, w, h)
    g.SelectObject(mem, bmp)
    # flag 2 = PW_RENDERFULLCONTENT: ask the window to redraw itself into the
    # memory DC, which is what makes this work for a window we never foreground.
    u.PrintWindow(hwnd, mem, 2)

    bi = BITMAPINFOHEADER()
    bi.biSize = ctypes.sizeof(BITMAPINFOHEADER)
    bi.biWidth = w
    bi.biHeight = -h          # negative => top-down rows, no flip needed
    bi.biPlanes = 1
    bi.biBitCount = 32
    bi.biCompression = 0      # BI_RGB
    buf = ctypes.create_string_buffer(w * h * 4)
    lines = g.GetDIBits(mem, bmp, 0, h, buf, ctypes.byref(bi), 0)

    g.DeleteObject(bmp)
    g.DeleteDC(mem)
    u.ReleaseDC(hwnd, src)
    if lines != h:
        return None

    raw = bytes(buf)
    rows = []
    for y in range(h):
        row = bytearray(b"\x00")          # PNG filter type 0 for every scanline
        base = y * w * 4
        for x in range(w):
            b_, g_, r_, _ = raw[base + x * 4: base + x * 4 + 4]
            row += bytes((r_, g_, b_))   # GDI hands us BGRA, PNG wants RGB
        rows.append(bytes(row))

    def chunk(tag, data):
        body = tag + data
        return (struct.pack(">I", len(data)) + body
                + struct.pack(">I", zlib.crc32(body) & 0xffffffff))

    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(b"".join(rows), 9))
            + chunk(b"IEND", b""))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("exe")
    ap.add_argument("out")
    ap.add_argument("--wait", type=float, default=1.5,
                    help="seconds to let the window come up and paint")
    ap.add_argument("--title", default=None,
                    help="only capture the window whose title contains this")
    ap.add_argument("--keep-open", action="store_true",
                    help="leave the process running after the screenshot")
    args = ap.parse_args()

    import subprocess
    proc = subprocess.Popen([args.exe])

    try:
        time.sleep(args.wait)

        windows = top_windows(proc.pid)
        for h, cls, title, vis in windows:
            print("window hwnd=0x%x class=%r title=%r visible=%s"
                  % (h, cls, title, vis))

        candidates = [w for w in windows if w[3]]
        if args.title:
            candidates = [w for w in candidates if args.title in w[2]]
        if not candidates:
            print("ERROR: no visible window%s"
                  % (" titled %r" % args.title if args.title else ""))
            return 1

        hwnd = candidates[0][0]
        png = capture(hwnd)
        if png is None:
            print("ERROR: PrintWindow/GetDIBits produced no pixels")
            return 1

        with open(args.out, "wb") as f:
            f.write(png)
        print("wrote %s (%d bytes) from hwnd=0x%x" % (args.out, len(png), hwnd))
        return 0
    finally:
        if not args.keep_open:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()


if __name__ == "__main__":
    sys.exit(main())
