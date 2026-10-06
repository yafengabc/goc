/* Exercises the eight wrappers that syscall.h routes through syscall(2) rather
 * than libc: stat, fstat, access, rename, mkdir, rmdir, getcwd, chmod.
 *
 * These are exactly the ones that would recurse if the aliases pointed at the
 * libc names, because goclib's dir.c and file.c define functions with those very
 * names and the definition is what the name resolves to. Under goc they reach
 * goa's syscall stub; under a host compiler they reach syscall(2) directly.
 * So this file is the test for that decision: if the route is wrong, these calls
 * either recurse until the stack dies or return the wrong bytes.
 *
 * It also has to distinguish "goclib's stat" from "musl's stat": both are named
 * stat, and only the layout in goclib's own <sys/stat.h> makes st_size land
 * where st_size is declared. */
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <dirent.h>
#include <sys/stat.h>

static int fails;

static void check(int ok, const char *what) {
    printf("%-28s %s\n", what, ok ? "ok" : "FAILED");
    if (!ok) fails++;
}

int main(void) {
    struct stat st;
    char cwd[256];
    char buf[256];

    /* A previous run that died partway leaves these behind, and mkdir/rename
     * then fail with EEXIST -- which is a correct answer about the filesystem,
     * not a fault in the route under test. Start from nothing. */
    remove("sc2.tmp");
    remove("sc1.tmp");
    rmdir("scdir");

    /* write a file to stat, chmod it, rename it, then remove it */
    {
        FILE *f = fopen("sc1.tmp", "w");
        if (!f) { fputs("cannot create sc1.tmp\n", stderr); return 1; }
        fputs("0123456789", f);   /* 10 bytes */
        fclose(f);
    }

    /* stat(2): st_size must be 10 */
    check(stat("sc1.tmp", &st) == 0, "stat()");
    check(st.st_size == 10, "stat st_size==10");
    check(S_ISREG(st.st_mode), "stat S_ISREG");

    /* fstat(2) on a descriptor, same file. The descriptor comes from fopen,
     * which is goclib's own and the only fd source in the public headers. */
    {
        FILE *f = fopen("sc1.tmp", "r");
        check(f != 0, "reopen for fstat");
        if (f) {
            check(fstat(1, &st) == 0, "fstat()");
            fclose(f);
        }
    }

    /* access(2) */
    check(access("sc1.tmp", F_OK) == 0, "access existing");
    check(access("no_such_file_xyz", F_OK) != 0, "access missing");

    /* rename(2) */
    check(rename("sc1.tmp", "sc2.tmp") == 0, "rename()");
    check(stat("sc2.tmp", &st) == 0, "stat after rename");
    check(access("sc1.tmp", F_OK) != 0, "old name gone");

    /* chmod(2) then confirm through stat */
    check(chmod("sc2.tmp", 0600) == 0, "chmod()");
    check(stat("sc2.tmp", &st) == 0, "stat after chmod");

    /* mkdir(2) / rmdir(2) / opendir(2) */
    check(mkdir("scdir", 0755) == 0, "mkdir()");
    check(stat("scdir", &st) == 0, "stat on dir");
    check(S_ISDIR(st.st_mode), "S_ISDIR");
    {
        DIR *d = opendir("scdir");
        check(d != 0, "opendir()");
        if (d) {
            int saw_dot = 0, saw_dotdot = 0;
            struct dirent *e;
            while ((e = readdir(d)) != 0) {
                if (strcmp(e->d_name, ".") == 0) saw_dot = 1;
                if (strcmp(e->d_name, "..") == 0) saw_dotdot = 1;
            }
            closedir(d);
            /* "." and ".." are real records on Linux; dir.c is documented to
             * drop them, so a correct readdir never reports them. */
            check(!saw_dot && !saw_dotdot, "readdir drops . and ..");
        }
    }
    check(rmdir("scdir") == 0, "rmdir()");
    check(access("scdir", F_OK) != 0, "dir gone");

    /* getcwd(2) */
    if (getcwd(cwd, sizeof cwd) != 0) {
        check(cwd[0] == '/', "getcwd absolute");
        printf("%-28s %s\n", "cwd", cwd);
    } else {
        check(0, "getcwd()");
    }

    /* remove the leftover, then report */
    remove("sc2.tmp");
    sprintf(buf, "fails=%d", fails);
    fputs(buf, stdout);
    fputs("\n", stdout);
    fputs(fails ? "FAIL\n" : "OK\n", stdout);
    return fails;
}
