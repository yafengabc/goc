/* ls -- list directory contents.
 *
 * A goc coreutils: compiled by goc and by gocl, and the output is meant to be
 * byte-identical on Windows PE and Linux ELF.
 *
 * Options:  -a  show entries whose name starts with '.'
 *           -l  long form (type, size, mtime, name)
 *           -1  one entry per line
 *           -F  mark directories with a trailing '/'
 *           -h  sizes in human units (with -l)
 *           --help
 *
 * Two deliberate differences from GNU ls, both forced by what goclib's
 * <sys/stat.h> can honestly report:
 *
 *   - The long form has no permission column and no owner/group column. On
 *     Windows goclib fills st_mode from GetFileAttributesExA, which has no
 *     Unix permission bits and no owner at all: it hands out a constant
 *     S_IFDIR|0777 or S_IFREG|0666. Printing "drwxrwxrwx" for every directory
 *     would be a lie the header itself cannot back, so the column is absent
 *     rather than invented. (The same reasoning is why -F marks directories
 *     only and not executables: the Windows branch cannot tell an executable
 *     from any other file, and guessing from the extension is a guess.)
 *
 *   - GNU ls writes one entry per line when its output is not a terminal.
 *     goclib has no isatty, so this ls cannot tell, and always uses the
 *     column layout unless -1 or -l was given. Set COLUMNS to choose the
 *     width; it defaults to 80.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dirent.h>
#include <sys/stat.h>
#include <time.h>
#include <errno.h>

/* An entry name is bounded by the 256 bytes <dirent.h> puts in d_name, and a
 * command-line operand is stored the same way, so a path longer than that is
 * truncated rather than written past the end of the array. */
#define NAME_MAX_GOC 256

static int opt_all;    /* -a */
static int opt_long;   /* -l */
static int opt_one;    /* -1 */
static int opt_class;  /* -F */
static int opt_human;  /* -h */
static int opt_cols = 80;

struct ent {
    char          name[NAME_MAX_GOC];
    unsigned long size;
    long          mtime;
    int           isdir;
};

static struct ent *ents;
static size_t      nents;
static size_t      cap;

static void *xrealloc(void *p, size_t n) {
    void *q = realloc(p, n);
    if (!q) {
        fprintf(stderr, "ls: out of memory\n");
        exit(2);
    }
    return q;
}

static void clear_ents(void) { nents = 0; }

/* add appends one entry. The name is copied, so the caller may reuse its
 * buffer -- readdir() hands back the same struct every time. */
static void add(const char *name, unsigned long size, long mtime, int isdir) {
    struct ent *e;
    size_t i;
    if (nents == cap) {
        cap = cap ? cap * 2 : 64;
        ents = (struct ent *)xrealloc(ents, cap * sizeof(struct ent));
    }
    e = &ents[nents++];
    for (i = 0; i < NAME_MAX_GOC - 1 && name[i]; i++)
        e->name[i] = name[i];
    e->name[i] = '\0';
    e->size = size;
    e->mtime = mtime;
    e->isdir = isdir;
}

static int cmp_ent(const void *pa, const void *pb) {
    const struct ent *a = (const struct ent *)pa;
    const struct ent *b = (const struct ent *)pb;
    return strcmp(a->name, b->name);
}

/* fmt_size writes a size, in human units when -h was given.
 *
 * The scaled forms are computed with integers: printf's %f would pull the
 * floating-point exponent machinery into every binary that lists a file, and
 * one decimal digit is all the format promises anyway. n % 1024 is at most
 * 1023, so (n % 1024) * 10 / 1024 is at most 9 and the fraction can never
 * round up into a carry. */
static void fmt_size(unsigned long n, char *out, size_t outsz) {
    const char *unit = "";
    unsigned long whole = n, frac = 0;
    if (opt_human) {
        if (n >= 1024UL * 1024 * 1024 * 1024) {
            whole = n / (1024UL * 1024 * 1024 * 1024);
            frac = (n % (1024UL * 1024 * 1024 * 1024)) * 10 /
                   (1024UL * 1024 * 1024 * 1024);
            unit = "T";
        } else if (n >= 1024UL * 1024 * 1024) {
            whole = n / (1024UL * 1024 * 1024);
            frac = (n % (1024UL * 1024 * 1024)) * 10 / (1024UL * 1024 * 1024);
            unit = "G";
        } else if (n >= 1024UL * 1024) {
            whole = n / (1024UL * 1024);
            frac = (n % (1024UL * 1024)) * 10 / (1024UL * 1024);
            unit = "M";
        } else if (n >= 1024UL) {
            whole = n / 1024UL;
            frac = (n % 1024UL) * 10 / 1024UL;
            unit = "K";
        }
    }
    if (unit[0])
        snprintf(out, outsz, "%ld.%ld%s", (long)whole, (long)frac, unit);
    else
        snprintf(out, outsz, "%ld", (long)whole);
}

static void fmt_time(long t, char *out, size_t outsz) {
    struct tm tm;
    time_t tt = (time_t)t;
    localtime_r(&tt, &tm);
    strftime(out, outsz, "%Y-%m-%d %H:%M", &tm);
}

/* disp_width is how many columns one entry occupies in the column layout:
 * the name, plus the '/' that -F appends to a directory. */
static size_t disp_width(const struct ent *e) {
    size_t n = strlen(e->name);
    if (opt_class && e->isdir) n += 1;
    return n;
}

static void print_name(const struct ent *e) {
    printf("%s", e->name);
    if (opt_class && e->isdir) printf("/");
}

/* print_columns writes the entries across the terminal and down, which is how
 * GNU ls fills them: the first column holds the first ceil(n/cols) names, not
 * the first n/cols. Reading down each column rather than across each row is
 * what makes the sorted order visible. */
static void print_columns(void) {
    size_t colw = 0, i, cols, rows, r, c;
    for (i = 0; i < nents; i++) {
        size_t w = disp_width(&ents[i]);
        if (w > colw) colw = w;
    }
    colw += 2;
    if (colw < 1) colw = 1;
    cols = (size_t)opt_cols / colw;
    if (cols == 0) cols = 1;
    if (cols > nents) cols = nents ? nents : 1;
    rows = (nents + cols - 1) / cols;
    for (r = 0; r < rows; r++) {
        for (c = 0; c < cols; c++) {
            size_t idx = c * rows + r;
            size_t w;
            if (idx >= nents) continue;
            print_name(&ents[idx]);
            /* No padding after the last entry on a line: a trailing run of
             * spaces is invisible but not identical, and these listings are
             * compared byte for byte between the two targets. */
            if (c + 1 < cols && idx + rows < nents) {
                w = disp_width(&ents[idx]);
                while (w < colw) { printf(" "); w++; }
            }
        }
        printf("\n");
    }
}

static void print_long(void) {
    char sz[32], tmstr[32];
    size_t i, maxw = 0, w;
    /* Two passes: the size column is right-aligned, so its width is not known
     * until every size has been formatted. The second pass reformats rather
     * than caching the strings, which would need an array sized by nents and
     * a bound that a large directory can exceed. */
    for (i = 0; i < nents; i++) {
        fmt_size(ents[i].size, sz, sizeof sz);
        w = strlen(sz);
        if (w > maxw) maxw = w;
    }
    for (i = 0; i < nents; i++) {
        fmt_size(ents[i].size, sz, sizeof sz);
        fmt_time(ents[i].mtime, tmstr, sizeof tmstr);
        printf("%c ", ents[i].isdir ? 'd' : '-');
        w = strlen(sz);
        while (w < maxw) { printf(" "); w++; }
        printf("%s %s ", sz, tmstr);
        print_name(&ents[i]);
        printf("\n");
    }
}

static void print_entries(void) {
    if (nents == 0) return;
    qsort(ents, nents, sizeof(struct ent), cmp_ent);
    if (opt_long)
        print_long();
    else if (opt_one || opt_cols <= 0) {
        size_t i;
        for (i = 0; i < nents; i++) { print_name(&ents[i]); printf("\n"); }
    } else
        print_columns();
}

/* join_dir builds "<dir>/<name>" into out. A directory operand that already
 * ends in a separator keeps its single one -- "C:/" would otherwise become
 * "C://", which Windows accepts and Linux does not treat as the same path. */
static void join_dir(char *out, size_t outsz, const char *dir, const char *name) {
    size_t n = 0, i;
    for (i = 0; dir[i] && n + 1 < outsz; i++) out[n++] = dir[i];
    if (n > 0 && out[n - 1] != '/' && out[n - 1] != '\\' && n + 1 < outsz)
        out[n++] = '/';
    for (i = 0; name[i] && n + 1 < outsz; i++) out[n++] = name[i];
    out[n < outsz ? n : outsz - 1] = '\0';
}

static int list_dir(const char *dir) {
    DIR *d = opendir(dir);
    struct dirent *de;
    char path[NAME_MAX_GOC * 2];
    if (!d) {
        fprintf(stderr, "ls: %s: %s\n", dir, strerror(errno));
        return 2;
    }
    clear_ents();
    while ((de = readdir(d)) != 0) {
        struct stat st;
        if (!opt_all && de->d_name[0] == '.') continue;
        join_dir(path, sizeof path, dir, de->d_name);
        /* A name that vanished between readdir and stat, or one whose
         * metadata this process may not read, is still listed -- its size and
         * date are reported as 0, which is what "unknown" looks like here
         * rather than an error that hides the entry entirely. */
        if (stat(path, &st) != 0) {
            add(de->d_name, 0, 0, 0);
            continue;
        }
        add(de->d_name, (unsigned long)st.st_size, (long)st.st_mtime,
            S_ISDIR(st.st_mode) ? 1 : 0);
    }
    closedir(d);
    print_entries();
    return 0;
}

static void usage(void) {
    printf("usage: ls [-a] [-l] [-1] [-F] [-h] [--help] [file ...]\n");
    printf("  -a  show entries starting with '.'\n");
    printf("  -l  long form: type, size, mtime, name\n");
    printf("  -1  one entry per line\n");
    printf("  -F  mark directories with '/'\n");
    printf("  -h  human-readable sizes (with -l)\n");
    printf("  -C  column layout (the default; width from COLUMNS, else 80)\n");
}

int main(int argc, char **argv) {
    char *cols;
    int i, status = 0, nops = 0;
    int printed = 0, nfiles = 0;
    /* Operands, sorted into the two kinds GNU ls separates: plain files it
     * lists first, directories it lists afterwards each under its own
     * "<dir>:" header. */
    const char **ops;
    int *op_isdir;
    int ndirs = 0;

    cols = getenv("COLUMNS");
    if (cols) {
        long v = strtol(cols, 0, 10);
        if (v > 0 && v < 100000) opt_cols = (int)v;
    }

    ops = (const char **)malloc((size_t)(argc > 0 ? argc : 1) * sizeof(char *));
    op_isdir = (int *)malloc((size_t)(argc > 0 ? argc : 1) * sizeof(int));
    if (!ops || !op_isdir) {
        fprintf(stderr, "ls: out of memory\n");
        return 2;
    }

    for (i = 1; i < argc; i++) {
        const char *a = argv[i];
        if (a[0] == '-' && a[1] != '\0') {
            int j;
            if (strcmp(a, "--help") == 0) { usage(); return 0; }
            for (j = 1; a[j]; j++) {
                switch (a[j]) {
                case 'a': opt_all = 1; break;
                case 'l': opt_long = 1; break;
                case '1': opt_one = 1; break;
                case 'F': opt_class = 1; break;
                case 'h': opt_human = 1; break;
                case 'C': opt_one = 0; break;
                default:
                    fprintf(stderr, "ls: unknown option '-%c'\n", a[j]);
                    usage();
                    return 2;
                }
            }
            continue;
        }
        ops[nops] = a;
        op_isdir[nops] = 0;
        nops++;
    }

    /* Classify the operands before printing anything: whether a header is
     * needed depends on how many of them are directories, which is not known
     * until the last one has been stat()ed. */
    for (i = 0; i < nops; i++) {
        struct stat st;
        if (stat(ops[i], &st) != 0) {
            fprintf(stderr, "ls: %s: %s\n", ops[i], strerror(errno));
            status = 2;
            ops[i] = 0;
            continue;
        }
        op_isdir[i] = S_ISDIR(st.st_mode) ? 1 : 0;
        if (op_isdir[i]) ndirs++; else nfiles++;
    }

    /* Plain file operands, listed as one group. */
    clear_ents();
    for (i = 0; i < nops; i++) {
        struct stat st;
        if (!ops[i] || op_isdir[i]) continue;
        if (stat(ops[i], &st) != 0) continue;
        add(ops[i], (unsigned long)st.st_size, (long)st.st_mtime, 0);
    }
    if (nents) {
        print_entries();
        printed = 1;
    }

    for (i = 0; i < nops; i++) {
        int rc;
        if (!ops[i] || !op_isdir[i]) continue;
        /* A lone directory operand gets no header, and neither the file group
         * nor the previous directory needs a blank line before the first
         * thing printed. GNU ls draws the same line between two blocks. */
        if (printed) printf("\n");
        if (printed || ndirs > 1 || nfiles) printf("%s:\n", ops[i]);
        rc = list_dir(ops[i]);
        if (rc > status) status = rc;
        printed = 1;
    }

    /* No operands at all: the current directory, with no header. */
    if (nops == 0) {
        int rc = list_dir(".");
        if (rc > status) status = rc;
    }

    free(ents);
    free(ops);
    free(op_isdir);
    return status;
}
