/* goclib/sys/socket.h -- path shim for host compilers.
 *
 * goc resolves an angled include on its basename, so <sys/socket.h> finds
 * goclib/socket.h directly and this file is never consulted; //go:embed
 * goclib/* does not reach into subdirectories, so it is not even embedded.
 *
 * It exists for gcc and clang, which want the POSIX path spelled out. It holds
 * no declarations of its own on purpose: a second copy of the socket API is a
 * second copy to keep in step, and the one place the two can disagree is
 * exactly where this library earns its keep.
 */
#include "../socket.h"
