/* goclib/netinet/in.h -- path shim for host compilers.
 *
 * The counterpart to sys/socket.h: goc matches <netinet/in.h> on its basename
 * and reads goclib/in.h; gcc and clang want the directory spelled out.
 */
#include "../in.h"
