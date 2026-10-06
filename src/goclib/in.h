#ifndef GOC_IN_H
#define GOC_IN_H

/* =============================================================================
 * in.h -- IPv4 addresses, ports and the byte-order conversions between them.
 *
 * Reached as <netinet/in.h> from a host compiler and as <in.h> from goc: the
 * preprocessor matches an angled include on its basename, so both spellings
 * land here.
 *
 * Nothing in this file calls the platform. The byte-order functions are shifts
 * and the address conversions are arithmetic on the four octets, because the
 * platform's own versions are exactly the things that differ: Winsock exports
 * htons/htonl from ws2_32 while Linux has no syscall for them at all, and
 * inet_ntoa is a static buffer in one libc and a thread-local one in another.
 * Implementing them here makes the behaviour of a program identical on both.
 *
 * The byte order is little-endian, which is what both targets are: x86-64
 * defines it. Network order is big-endian, so every conversion is a byte
 * reversal on this architecture and the macros below say so rather than
 * pretending to be portable to a big-endian machine.
 * ========================================================================== */

#include <stddef.h>
#include <socket.h>

typedef unsigned int in_addr_t;
typedef unsigned short in_port_t;

/* A 32-bit IPv4 address, kept in network byte order -- the four bytes in the
 * order they appear on the wire. s_addr reads as a host integer only after
 * ntohl(), which is why every comparison against INADDR_* has to go through
 * the byte order first. */
struct in_addr {
    in_addr_t s_addr;
};

/* AF_INET's overlay on the generic struct sockaddr: the same 16 bytes, with
 * the 14 bytes of sa_data read as a port, an address, and 8 bytes of padding
 * that exist because sockaddr used to be larger. Casting between the two is
 * the intended use, and is why both are 16 bytes. */
struct sockaddr_in {
    unsigned short   sin_family;
    in_port_t        sin_port;
    struct in_addr   sin_addr;
    char             sin_zero[8];
};

/* ------------------------------------------------------------------ */
/* Well-known addresses                                                */
/* ------------------------------------------------------------------ */

/* These are host-order integers, not values ready to store: they are the
 * numbers the address means, in the order you would say it aloud, and a
 * struct in_addr wants its s_addr in network order. So the idiom is
 *
 *     sa.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
 *
 * and skipping the htonl() is the classic bug: on a little-endian machine it
 * puts 1.0.0.127 on the wire instead of 127.0.0.1, which is a socket that
 * connects to something else entirely rather than one that fails.
 *
 * INADDR_ANY and INADDR_BROADCAST are unaffected -- 0 and all-ones read the
 * same in both orders -- which is why they are the two that look right when
 * the conversion is forgotten. */
#define INADDR_ANY       0x00000000u
#define INADDR_LOOPBACK  0x7f000001u
#define INADDR_BROADCAST 0xffffffffu
/* inet_addr()'s failure value, and deliberately the same as the broadcast
 * address -- which is the historical wart, kept because that is what callers
 * test against. "255.255.255.255" is therefore both a valid input and the
 * error return. */
#define INADDR_NONE      0xffffffffu

#define INET_ADDRSTRLEN 16   /* "255.255.255.255" plus the terminator */

/* ------------------------------------------------------------------ */
/* Byte order                                                          */
/* ------------------------------------------------------------------ */
/*
 * Host-to-network and network-to-host are the same operation on a
 * little-endian machine, so each pair shares one definition rather than being
 * written twice with a comment saying they happen to match.
 */
unsigned short __goclib_bswap16(unsigned short v);
unsigned int   __goclib_bswap32(unsigned int v);

#define htons(x) __goclib_bswap16((unsigned short)(x))
#define htonl(x) __goclib_bswap32((unsigned int)(x))
#define ntohs(x) __goclib_bswap16((unsigned short)(x))
#define ntohl(x) __goclib_bswap32((unsigned int)(x))

/* ------------------------------------------------------------------ */
/* Address text                                                        */
/* ------------------------------------------------------------------ */

/* "203.0.113.7" -> network-order address; INADDR_NONE if it does not parse.
 * Accepts the four-octet form only -- no hostnames, no shorthand such as
 * "127.1", because those rules are a historical mess the two platforms never
 * agreed on. */
in_addr_t inet_addr(const char *cp);

/* Network-order address -> "203.0.113.7" in a static buffer, overwritten by
 * the next call. Provided because so much existing code uses it; inet_ntop()
 * below is the one to write new code against, since it does not hand out a
 * buffer the caller does not own. */
char *inet_ntoa(struct in_addr addr);

/* The modern pair, and the reason to prefer them: the caller owns the buffer
 * and says how big it is. Both are AF_INET only -- there is no IPv6 here.
 * inet_pton returns 1 on success, 0 if the text is not an address, -1 if the
 * family is not AF_INET; inet_ntop returns dst, or a null pointer if the
 * buffer is too small. */
int  inet_pton(int af, const char *src, void *dst);
const char *inet_ntop(int af, const void *src, char *dst, socklen_t size);

#endif /* GOC_IN_H */
