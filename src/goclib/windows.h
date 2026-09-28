#ifndef GOC_WINDOWS_H
#define GOC_WINDOWS_H

/* goc windows.h -- umbrella header for the Win32 API surface.
 *
 * The old single-file windows.h was written before goc had structs, so it
 * could only declare APIs whose parameters and returns were scalars or
 * pointers, and explicitly deferred the struct-heavy parts (CreateWindow,
 * WNDCLASS, GetMessage, ...). Struct support landed (2026-09-28), so this
 * header is now reorganised one DLL per header, matching the real Windows
 * SDK layout:
 *
 *   <windows.h>   umbrella: pulls in the four below in dependency order
 *   <windef.h>    scalar types, handles, RECT/POINT/SIZE, decorations
 *   <winbase.h>   kernel32: files, console, modules, env, time, memory
 *   <winuser.h>   user32: messages, windows, classes, cursors, DCs
 *   <wingdi.h>    gdi32: stock objects, DCs, bitmap info, raster ops
 *
 * Each DLL's functions import through the goa PE import directory; the
 * "which DLL" table is goclib/win32.def, the single source of truth. A
 * prototype may exist here without a def entry (it just fails to link), but
 * every name in win32.def should have a prototype somewhere under these
 * headers. Struct layouts follow LLP64 and MSVC x64 packing, so they match
 * what the OS writes.
 *
 * Calling convention: Win64 has a single flat convention, so WINAPI and
 * CALLBACK expand to nothing.
 */

#include <windef.h>
#include <winbase.h>
#include <winuser.h>
#include <wingdi.h>

#endif /* GOC_WINDOWS_H */
