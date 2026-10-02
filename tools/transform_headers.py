#!/usr/bin/env python3
"""Convert Win32 API prototypes in goc headers to carry their DLL name inline.

Before:  BOOL CloseHandle(HANDLE h);
After:   extern BOOL CloseHandle(HANDLE h), kernel32;

The DLL is chosen per file. This lets each header be self-describing so the
centralised win32.def can be deleted.

Robust against:
  * /* */ block comments (incl. multi-line and ones containing '(' or 'extern'),
  * multi-line prototypes (paren-depth tracking),
  * trailing block comments after the ';',
  * typedefs / struct-union-enum definitions (left untouched).
"""
import sys

DLL_BY_FILE = {
    "winbase.h": "kernel32",
    "winuser.h": "user32",
    "wingdi.h":  "gdi32",
}

out_lines = []


def is_blank_or_preproc(line):
    s = line.strip()
    return s == "" or s.startswith("#")


def complete_decl(decl_lines, dll):
    decl = "\n".join(decl_lines)
    # Guard: typedefs (incl. function-pointer typedefs) and struct/union/enum
    # definitions are not prototypes; leave them untouched.
    head = decl.split("(")[0]
    if "typedef" in head or "{" in decl:
        out_lines.extend(decl_lines)
        return
    first = decl_lines[0]
    rest = decl_lines[1:]
    if not first.lstrip().startswith("extern "):
        first = "extern " + first.lstrip()
    if rest:
        # extern on the first line; DLL on the last line.
        last = rest[-1]
        idx = last.rfind(";")
        last = last[:idx] + ", " + dll + last[idx:]
        rest[-1] = last
        out_lines.append(first)
        out_lines.extend(rest)
    else:
        idx = first.rfind(";")
        first = first[:idx] + ", " + dll + first[idx:]
        out_lines.append(first)


def transform(text, dll):
    global out_lines
    out_lines = []
    buf = []          # accumulated ORIGINAL lines of the current declaration
    depth = 0         # parenthesis depth across buf (computed from code view)
    in_comment = False

    def code_of(line):
        """Return the comment-stripped view of `line`, updating in_comment."""
        nonlocal in_comment
        res = []
        i = 0
        n = len(line)
        while i < n:
            if in_comment:
                j = line.find("*/", i)
                if j == -1:
                    in_comment = True
                    return "".join(res)
                res.append(" " * (j + 2 - i))
                i = j + 2
                in_comment = False
            else:
                j = line.find("/*", i)
                if j == -1:
                    res.append(line[i:])
                    i = n
                else:
                    res.append(line[i:j])
                    in_comment = True
                    i = j + 2
        return "".join(res)

    for raw in text.split("\n"):
        code = code_of(raw)
        if not buf:
            if in_comment or code.strip() == "" or "(" not in code:
                out_lines.append(raw)
                continue
            buf = [raw]
            depth = code.count("(") - code.count(")")
            stripped = code.rstrip()
            if depth <= 0 and stripped.endswith(";"):
                complete_decl(buf, dll)
                buf = []
        else:
            buf.append(raw)
            depth += code.count("(") - code.count(")")
            stripped = code.rstrip()
            if depth <= 0 and stripped.endswith(";"):
                complete_decl(buf, dll)
                buf = []
    if buf:
        out_lines.extend(buf)  # unbalanced: emit as-is
    return "\n".join(out_lines)


def main():
    for fname, dll in DLL_BY_FILE.items():
        path = sys.argv[1] + "/" + fname
        with open(path, "r", encoding="utf-8") as f:
            text = f.read()
        with open(path, "w", encoding="utf-8", newline="") as f:
            f.write(transform(text, dll))
        print("transformed", path)


if __name__ == "__main__":
    main()
