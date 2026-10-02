/* C23 feature: #embed advanced options prefix()/suffix()/if_empty() and __has_embed
 * Clause:     C23 6.10.11.3 (embed options), 6.10.11.2 (__has_embed)
 * Strategy:   verify prefix/suffix token splicing (the separator comma must live
 *             INSIDE the option token list, gcc inserts none between the option
 *             tokens and the byte stream), if_empty() supplying tokens when an
 *             embed yields no bytes (limit(0)), and probe __has_embed under a
 *             defined() guard so a compiler lacking it takes the #else branch.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

/* prefix(11,) -> { 11, 65, 66, 67, 10 } */
unsigned char pre[] = {
    #embed "embed_data.txt" prefix(11,)
};

/* suffix(,22) -> { 65, 66, 67, 10, 22 } */
unsigned char suf[] = {
    #embed "embed_data.txt" suffix(, 22)
};

/* prefix(0xA,) suffix(,0xB) -> { 10, 65, 66, 67, 10, 11 } */
unsigned char both[] = {
    #embed "embed_data.txt" prefix(0xA,) suffix(, 0xB)
};

/* limit(0) forces an empty embed; if_empty(0xEE,) supplies the token list */
unsigned char empt[] = {
    #embed "embed_data.txt" limit(0) if_empty(0xEE,)
};

/* __has_embed is ONLY valid inside a preprocessing directive, so expand it in
 * #if blocks into plain integer macros that main() can print. */
#if defined(__has_embed)
#  if __has_embed("embed_data.txt")
#    define HE_EXIST 1
#  else
#    define HE_EXIST 0
#  endif
#  if __has_embed("does_not_exist_zzz.txt")
#    define HE_MISS 1
#  else
#    define HE_MISS 0
#  endif
#  define HE_AVAIL 1
#else
#  define HE_EXIST (-1)
#  define HE_MISS  (-2)
#  define HE_AVAIL 0
#endif

int main(void) {
    int passed = 0, total = 0;
    int ok;

    ++total;
    ok = (sizeof(pre)==5 && pre[0]==11 && pre[1]==65 && pre[4]==10);
    printf("case%d: prefix sizeof=%d bytes=%d,%d,%d,%d,%d ok=%d\n",
           total, (int)sizeof(pre), pre[0], pre[1], pre[2], pre[3], pre[4], ok);
    if (ok) passed++;

    ++total;
    ok = (sizeof(suf)==5 && suf[0]==65 && suf[3]==10 && suf[4]==22);
    printf("case%d: suffix sizeof=%d head=%d,%d tail=%d,%d ok=%d\n",
           total, (int)sizeof(suf), suf[0], suf[1], suf[3], suf[4], ok);
    if (ok) passed++;

    ++total;
    ok = (sizeof(both)==6 && both[0]==0xA && both[1]==65 && both[5]==0xB);
    printf("case%d: prefix+suffix sizeof=%d head=%d mid=%d tail=%d ok=%d\n",
           total, (int)sizeof(both), both[0], both[1], both[5], ok);
    if (ok) passed++;

    ++total;
    ok = (sizeof(empt)==1 && empt[0]==0xEE);
    printf("case%d: if_empty sizeof=%d val=%d ok=%d\n",
           total, (int)sizeof(empt), empt[0], ok);
    if (ok) passed++;

    /* __has_embed: gcc returns 1 for a found file and 0 for a missing one.
     * A compiler that does not provide __has_embed reports HE_AVAIL=0. */
    ++total;
    ok = (HE_AVAIL && HE_EXIST == 1 && HE_MISS == 0);
    printf("case%d: __has_embed avail=%d exist=%d missing=%d ok=%d\n",
           total, HE_AVAIL, HE_EXIST, HE_MISS, ok);
    if (ok) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
