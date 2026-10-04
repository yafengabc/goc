#include "goclib.h"

/* =============================================================================
 * args.c -- command-line argument bootstrap for the Windows entry stub.
 *
 * A PE entry point receives no argc/argv (unlike a SysV ELF, where the kernel
 * leaves them on the initial stack and goc's Linux stub reads them directly).
 * goc's Windows _start therefore calls __goclib_get_args once, before main, to
 * build a conventional argc/argv pair from GetCommandLineA.
 *
 * argv[0] is the program path exactly as GetCommandLineA reports it; the
 * returned array and every string are heap-allocated and never freed (the
 * process exits right after main). The parsing follows the Windows rules for
 * quotes and backslash escapes at a level that is correct for ordinary
 * arguments (spaces inside double quotes are preserved; "" and \"/"\\ emit a
 * literal quote/backslash).
 * ========================================================================== */
#ifdef _WIN32

extern char *GetCommandLineA(void);

static int cmdlen(const char *s) {
    int n = 0;
    while (s[n]) n++;
    return n;
}

/* Walk a Windows command line, counting tokens and the total storage (every
 * token plus its NUL terminator) they need. A token ends at an unquoted space
 * or tab; a double quote toggles quote mode, and inside it "" and \" / \\ emit
 * a literal quote / backslash. */
static void scan_args(const char *s, int *argc_out, int *bytes_out) {
    int argc = 0, bytes = 0;
    int i = 0, n = cmdlen(s);
    while (i < n) {
        while (i < n && (s[i] == ' ' || s[i] == '\t')) i++;
        if (i >= n) break;
        argc++;
        int quoted = 0;
        while (i < n) {
            char c = s[i];
            if (!quoted) {
                if (c == '"') { quoted = 1; i++; continue; }
                if (c == ' ' || c == '\t') break;
                bytes++; i++;
            } else {
                if (c == '"') {
                    if (i + 1 < n && s[i + 1] == '"') { bytes++; i += 2; continue; }
                    quoted = 0; i++; continue;
                }
                if (c == '\\' && i + 1 < n && s[i + 1] == '"') { bytes++; i += 2; continue; }
                if (c == '\\' && i + 1 < n && s[i + 1] == '\\') { bytes++; i += 2; continue; }
                bytes++; i++;
            }
        }
        bytes++; /* NUL terminator for this token */
    }
    *argc_out = argc;
    *bytes_out = bytes;
}

int __goclib_get_args(char ***argvp) {
    char *cmd = GetCommandLineA();
    int argc, bytes;
    scan_args(cmd, &argc, &bytes);
    char **argv = (char **)malloc((size_t)(argc + 1) * sizeof(char *));
    char *store = (char *)malloc((size_t)bytes);
    int i = 0, n = cmdlen(cmd), ai = 0, si = 0;
    while (i < n) {
        while (i < n && (cmd[i] == ' ' || cmd[i] == '\t')) i++;
        if (i >= n) break;
        argv[ai++] = store + si;
        int quoted = 0;
        while (i < n) {
            char c = cmd[i];
            if (!quoted) {
                if (c == '"') { quoted = 1; i++; continue; }
                if (c == ' ' || c == '\t') break;
                store[si++] = c; i++;
            } else {
                if (c == '"') {
                    if (i + 1 < n && cmd[i + 1] == '"') { store[si++] = '"'; i += 2; continue; }
                    quoted = 0; i++; continue;
                }
                if (c == '\\' && i + 1 < n && cmd[i + 1] == '"') { store[si++] = '"'; i += 2; continue; }
                if (c == '\\' && i + 1 < n && cmd[i + 1] == '\\') { store[si++] = '\\'; i += 2; continue; }
                store[si++] = c; i++;
            }
        }
        store[si++] = '\0';
    }
    argv[argc] = 0;
    *argvp = argv;
    return argc;
}

extern unsigned short *GetCommandLineW(void);

/* Return a pointer into the full command line just past argv[0], matching the
 * MSVC CRT's wWinMain/WinMain lpCmdLine contract (the program name is NOT
 * included). A quoted argv[0] is skipped correctly. */
static unsigned short *skip_prog_w(unsigned short *p) {
    if (*p == '"') {
        p++;
        while (*p && *p != '"') p++;
        if (*p == '"') p++;
    } else {
        while (*p && *p != ' ' && *p != '\t') p++;
    }
    while (*p == ' ' || *p == '\t') p++;
    return p;
}

static char *skip_prog_a(char *p) {
    if (*p == '"') {
        p++;
        while (*p && *p != '"') p++;
        if (*p == '"') p++;
    } else {
        while (*p && *p != ' ' && *p != '\t') p++;
    }
    while (*p == ' ' || *p == '\t') p++;
    return p;
}

unsigned short *__goclib_lp_cmdline_w(void) {
    return (unsigned short *)skip_prog_w(GetCommandLineW());
}

char *__goclib_lp_cmdline_a(void) {
    return (char *)skip_prog_a(GetCommandLineA());
}
#endif
