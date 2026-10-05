/* C23 feature: enum with explicit underlying type (enum E : T)
 * Clause:     C23 6.7.2.2 enumeration specifiers; 6.7.2.2p4 fixed underlying type
 * Strategy:   fixed underlying types int / unsigned char / long long, enumerators
 *             beyond int range, sizeof the enum, and enum-to-int conversion.
 *             goc parses the syntax; whether it sizes the enum to T is tested.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

enum EInt : int { EI_A = 1, EI_B = 2 };
enum EUChar : unsigned char { EC_A = 250, EC_B = 251 };
enum ELLong : long long { EL_A = 9000000000LL, EL_B = 9000000001LL };
enum EUInt : unsigned int { EU_BIG = 4000000000U };

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int sInt = (int)sizeof(enum EInt);
    int sUChar = (int)sizeof(enum EUChar);
    int sLL = (int)sizeof(enum ELLong);
    int sUInt = (int)sizeof(enum EUInt);
    printf("case%d: sizes int=%d uchar=%d llong=%d uint=%d\n",
           total, sInt, sUChar, sLL, sUInt);
    if (sInt == 4 && sUChar == 1 && sLL == 8 && sUInt == 4) passed++;

    ++total;
    printf("case%d: EI_A=%d EI_B=%d EC_A=%d EC_B=%d\n",
           total, (int)EI_A, (int)EI_B, (int)EC_A, (int)EC_B);
    if (EI_A == 1 && EI_B == 2 && EC_A == 250 && EC_B == 251) passed++;

    ++total;
    long long la = EL_A;
    long long lb = EL_B;
    printf("case%d: EL_A=%lld EL_B=%lld\n", total, la, lb);
    if (la == 9000000000LL && lb == 9000000001LL) passed++;

    ++total;
    unsigned int big = EU_BIG;
    printf("case%d: EU_BIG=%u\n", total, big);
    if (big == 4000000000U) passed++;

    ++total;
    enum EInt ev = EI_B;
    int converted = (int)ev;
    printf("case%d: enum->int converted=%d\n", total, converted);
    if (converted == 2) passed++;

    /* the underlying type must also drive struct layout ... */
    ++total;
    struct H { enum EUChar a; enum EInt b; enum ELLong c; };
    struct H h;
    h.a = EC_A; h.b = EI_B; h.c = EL_A;
    printf("case%d: member sizeof=%d %d %d struct=%d\n", total,
           (int)sizeof(h.a), (int)sizeof(h.b), (int)sizeof(h.c),
           (int)sizeof(struct H));
    if ((int)sizeof(h.a) == 1 && (int)sizeof(h.b) == 4 &&
        (int)sizeof(h.c) == 8 && (int)sizeof(struct H) == 16) passed++;

    /* ... and objects of the enum type: arrays, and ++ on a narrow enum */
    ++total;
    enum EUChar arr[3];
    arr[0] = EC_A; arr[1] = EC_B; arr[2] = EC_A;
    enum EUChar narrow = EC_B;
    narrow++;
    printf("case%d: arr=%d %d %d narrow++=%d\n", total,
           (int)arr[0], (int)arr[1], (int)arr[2], (int)narrow);
    if (arr[0] == 250 && arr[1] == 251 && arr[2] == 250 && narrow == 252) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
