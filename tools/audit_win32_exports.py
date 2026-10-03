#!/usr/bin/env python3
"""Audit every Win32 import goclib declares against the real Windows DLLs.

goclib's headers declare imported functions as `extern RET name(args), dll;`.
goa turns each of those into a PE import-table entry, and the Windows loader
resolves every one of them at process start. A name that is not actually
exported by that DLL is therefore not a compile error but a *load-time*
failure: the process dies before its first instruction with
STATUS_ENTRYPOINT_NOT_FOUND (0xC0000139) and no diagnostic of its own.

That failure mode is expensive to debug from the symptom, so this script
checks the whole surface at once. It is the check that found the real
spellings:

  gdi32.GetObject        -> GetObjectA / GetObjectW (GetObject is an SDK macro)
  gdi32.CreateDC         -> CreateDCA / CreateDCW
  user32.CreateWindowA/W -> CreateWindowExA/W (no plain CreateWindow export)
  user32.GlobalAddAtomA  -> kernel32 (the atom table lives in kernel32)
  user32.GlobalFindAtomA -> kernel32

Usage:  python tools/audit_win32_exports.py [--json]
Exit:   0 when every declaration resolves, 1 otherwise.
"""

import argparse
import ctypes
import json
import os
import re
import sys

# goclib's header directory, relative to the repository root.
REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
GOCLIB = os.path.join(REPO, "src", "goclib")
SYSTEM32 = os.path.join(os.environ.get("SystemRoot", r"C:\Windows"), "System32")

# `extern RET name(args), dll;` -- the return type is a chain of words/pointers
# and the argument list may wrap across lines, so match up to the `;`.
DECL = re.compile(
    r"extern\s+[A-Za-z_][A-Za-z_0-9_ \t*]*?\b([A-Za-z_][A-Za-z_0-9_]*)\s*"
    r"\(([^;]*?)\)\s*,\s*([A-Za-z0-9_]+)\s*;",
    re.S,
)
# A declaration is a prototype (a bare `;` body) when nothing follows the `;`
# but whitespace/comment before the next declaration. The goclib convention is
# that headers only ever declare, never define, so every hit is an import.
COMMENT_BLOCK = re.compile(r"/\*.*?\*/", re.S)
COMMENT_LINE = re.compile(r"//[^\n]*")


def collect_declarations():
    """Return {dll_lower: {name, ...}} for every Win32 import prototype."""
    found = {}
    for fname in sorted(os.listdir(GOCLIB)):
        if not fname.endswith(".h"):
            continue
        text = open(os.path.join(GOCLIB, fname), encoding="utf-8", errors="ignore").read()
        text = COMMENT_BLOCK.sub(" ", text)
        text = COMMENT_LINE.sub(" ", text)
        for m in DECL.finditer(text):
            name, dll = m.group(1), m.group(3).lower()
            if dll == "syscall":
                continue  # the Linux target, not a PE import
            found.setdefault(dll, set()).add(name)
    return found


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--json", action="store_true", help="machine-readable output")
    args = ap.parse_args()

    k32 = ctypes.WinDLL("kernel32")
    k32.GetProcAddress.restype = ctypes.c_void_p
    k32.GetProcAddress.argtypes = [ctypes.c_void_p, ctypes.c_char_p]
    k32.LoadLibraryW.restype = ctypes.c_void_p
    k32.LoadLibraryW.argtypes = [ctypes.c_wchar_p]

    declared = collect_declarations()
    problems = []
    total = 0

    for dll, names in sorted(declared.items()):
        path = os.path.join(SYSTEM32, dll + ".dll")
        if not os.path.exists(path):
            problems.append({"dll": dll, "symbol": None, "why": "DLL not found"})
            continue
        handle = k32.LoadLibraryW(path)
        if not handle:
            problems.append({"dll": dll, "symbol": None, "why": "LoadLibrary failed"})
            continue
        for name in sorted(names):
            total += 1
            if not k32.GetProcAddress(handle, name.encode()):
                problems.append({"dll": dll, "symbol": name, "why": "not exported"})

    if args.json:
        print(json.dumps({"audited": total, "problems": problems}, indent=2))
    else:
        print("audited %d extern declarations across %d DLLs" % (total, len(declared)))
        if not problems:
            print("OK: every declared Win32 import resolves on this machine")
        else:
            print("PROBLEMS:")
            for p in problems:
                sym = p["symbol"] or "<dll>"
                print("  %-12s %-34s %s" % (p["dll"], sym, p["why"]))
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
