#ifndef GOC_IO_H
#define GOC_IO_H

/* goc io.h -- the low-level I/O surface Windows C programs reach for.
 *
 * goc has no file-descriptor layer: FILE is its own struct and the OS handle
 * lives inside it, so there is no descriptor to hand out. The three standard
 * streams are the ones every caller actually names, and they are reported by
 * their conventional numbers; anything else answers -1. _setmode has nothing
 * to switch in the first place -- goc never translates newlines on write --
 * so it reports the mode it was handed.
 *
 * Real code calls these: Nim's std/syncio puts stdin/stdout/stderr into binary
 * mode through exactly this pair at startup.
 */

#include <stdio.h>

#define _O_TEXT    0x4000
#define _O_BINARY  0x8000
#define _O_WTEXT   0x10000
#define _O_U16TEXT 0x20000
#define O_TEXT     _O_TEXT
#define O_BINARY   _O_BINARY

extern int _fileno(FILE *f);
extern int _setmode(int fd, int mode);

#endif /* GOC_IO_H */
