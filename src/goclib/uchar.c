/* goc uchar.c -- char16_t/char32_t conversion entry points (C11 7.28.1).
 *
 * The four functions convert between the multibyte (UTF-8) encoding and
 * UTF-16/32 code units, following the C11 return protocol:
 *   - mbrtoc16: bytes consumed; (size_t)-3 with a high surrogate stored when
 *     the code point is non-BMP (a following s==NULL call yields the low
 *     surrogate and (size_t)-1); (size_t)-2 incomplete; (size_t)-1 error.
 *   - c16rtomb: bytes written; (size_t)-3 with the high surrogate held in
 *     *ps until its low surrogate arrives; (size_t)-1 for a lone surrogate.
 *   - mbrtoc32 / c32rtomb: direct code point <-> UTF-8 (stateless).
 * mbstate_t carries only the pending-surrogate half of a UTF-16 pair.
 */
#include <stddef.h>
#include <uchar.h>

/* number of UTF-8 bytes led by a byte (0 = invalid lead) */
static int utf8_len(unsigned char lead) {
    if (lead < 0x80) return 1;
    if ((lead & 0xe0) == 0xc0) return 2;
    if ((lead & 0xf0) == 0xe0) return 3;
    if ((lead & 0xf8) == 0xf0) return 4;
    return 0;
}

/* decode one UTF-8 char from s (at most len bytes). Returns the code point,
 * -1 on an encoding error (*consumed = bytes to skip), -2 if incomplete
 * (*consumed = 0). */
static long decode_utf8(const char *s, int len, int *consumed) {
    unsigned char b0 = (unsigned char)s[0];
    long cp;
    if (b0 < 0x80) { *consumed = 1; return (long)b0; }
    if (utf8_len(b0) == 0) { *consumed = 1; return -1; }
    if (len < 2) { *consumed = 0; return -2; }
    if ((b0 & 0xe0) == 0xc0) {
        if (((unsigned char)s[1] & 0xc0) != 0x80) { *consumed = 1; return -1; }
        cp = ((long)(b0 & 0x1f) << 6) | ((unsigned char)s[1] & 0x3f);
        if (cp < 0x80) { *consumed = 2; return -1; } /* overlong */
        *consumed = 2; return cp;
    }
    if (len < 3) { *consumed = 0; return -2; }
    if ((b0 & 0xf0) == 0xe0) {
        unsigned char b1 = (unsigned char)s[1];
        unsigned char b2 = (unsigned char)s[2];
        if (((b1 & 0xc0) != 0x80) || ((b2 & 0xc0) != 0x80)) { *consumed = 1; return -1; }
        cp = ((long)(b0 & 0x0f) << 12) | ((long)(b1 & 0x3f) << 6) | (b2 & 0x3f);
        if (cp < 0x800 || (cp >= 0xd800 && cp <= 0xdfff)) { *consumed = 3; return -1; }
        *consumed = 3; return cp;
    }
    if (len < 4) { *consumed = 0; return -2; }
    if ((b0 & 0xf8) == 0xf0) {
        unsigned char b1 = (unsigned char)s[1];
        unsigned char b2 = (unsigned char)s[2];
        unsigned char b3 = (unsigned char)s[3];
        if (((b1 & 0xc0) != 0x80) || ((b2 & 0xc0) != 0x80) || ((b3 & 0xc0) != 0x80)) { *consumed = 1; return -1; }
        cp = ((long)(b0 & 0x07) << 18) | ((long)(b1 & 0x3f) << 12) |
             ((long)(b2 & 0x3f) << 6) | (b3 & 0x3f);
        if (cp < 0x10000 || cp > 0x10ffff) { *consumed = 4; return -1; }
        *consumed = 4; return cp;
    }
    *consumed = 1; return -1;
}

/* encode a code point as UTF-8 into s; returns bytes written (1..4) or -1 */
static int encode_utf8(char *s, long cp) {
    if (cp < 0 || cp > 0x10ffff || (cp >= 0xd800 && cp <= 0xdfff)) return -1;
    if (cp < 0x80) { s[0] = (char)cp; return 1; }
    if (cp < 0x800) {
        s[0] = (char)(0xc0 | (cp >> 6));
        s[1] = (char)(0x80 | (cp & 0x3f));
        return 2;
    }
    if (cp < 0x10000) {
        s[0] = (char)(0xe0 | (cp >> 12));
        s[1] = (char)(0x80 | ((cp >> 6) & 0x3f));
        s[2] = (char)(0x80 | (cp & 0x3f));
        return 3;
    }
    s[0] = (char)(0xf0 | (cp >> 18));
    s[1] = (char)(0x80 | ((cp >> 12) & 0x3f));
    s[2] = (char)(0x80 | ((cp >> 6) & 0x3f));
    s[3] = (char)(0x80 | (cp & 0x3f));
    return 4;
}

size_t mbrtoc16(char16_t *restrict pc16, const char *restrict s,
                size_t n, mbstate_t *restrict ps) {
    long cp;
    int consumed;
    if (s == NULL) {
        /* second half of a surrogate pair: deliver the pending low half */
        if (ps->__c != 0) { *pc16 = (char16_t)ps->__c; ps->__c = 0; return (size_t)-1; }
        return 0;
    }
    if (n == 0) return (size_t)-2;
    cp = decode_utf8(s, (int)(n > 4 ? 4 : n), &consumed);
    if (cp == -1) return (size_t)-1;
    if (cp == -2) return (size_t)-2;
    if (cp > 0xffff) {
        *pc16 = (char16_t)(0xd800 + ((cp - 0x10000) >> 10));
        ps->__c = 0xdc00 + ((cp - 0x10000) & 0x3ff);
        return (size_t)-3;
    }
    *pc16 = (char16_t)cp;
    return (size_t)consumed;
}

size_t c16rtomb(char *restrict s, char16_t wc16, mbstate_t *restrict ps) {
    long cp;
    if (s == NULL) { ps->__c = 0; return 1; }
    if (wc16 >= 0xd800 && wc16 <= 0xdbff) {
        /* high surrogate: hold it until the low half arrives */
        ps->__c = wc16;
        return (size_t)-3;
    }
    if (wc16 >= 0xdc00 && wc16 <= 0xdfff) {
        if (ps->__c != 0) {
            cp = 0x10000 + ((ps->__c - 0xd800) << 10) + (wc16 - 0xdc00);
            ps->__c = 0;
        } else {
            cp = -1; /* lone low surrogate */
        }
    } else {
        cp = (long)wc16;
    }
    if (cp < 0) return (size_t)-1;
    return (size_t)encode_utf8(s, cp);
}

size_t mbrtoc32(char32_t *restrict pc32, const char *restrict s,
                size_t n, mbstate_t *restrict ps) {
    long cp;
    int consumed;
    (void)ps;
    if (s == NULL) return 0;
    if (n == 0) return (size_t)-2;
    cp = decode_utf8(s, (int)(n > 4 ? 4 : n), &consumed);
    if (cp == -1) return (size_t)-1;
    if (cp == -2) return (size_t)-2;
    *pc32 = (char32_t)cp;
    return (size_t)consumed;
}

size_t c32rtomb(char *restrict s, char32_t wc32, mbstate_t *restrict ps) {
    (void)ps;
    if (s == NULL) return 1;
    return (size_t)encode_utf8(s, (long)wc32);
}
