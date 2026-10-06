#include "goclib.h"
#include <dirent.h>
#include <sys/stat.h>
#include <syscall.h>   /* the raw system calls, reached per host (see the file) */
#include <stdlib.h>
#include <string.h>

/* =============================================================================
 * dir.c -- <dirent.h> and <sys/stat.h>
 *
 * Two unrelated OS mechanisms behind one POSIX-shaped door:
 *
 *   Windows  FindFirstFileA opens a *search* (there is no directory fd to
 *            have); FindNextFileA walks it and FindClose ends it. The
 *            enumeration never yields "." or "..".
 *   Linux    open() gives a directory fd and getdents64() fills a buffer
 *            with variable-length records:
 *
 *                struct linux_dirent64 {
 *                    unsigned long  d_ino;    (offset 0)
 *                    long           d_off;    (offset 8)
 *                    unsigned short d_reclen; (offset 16)
 *                    unsigned char  d_type;   (offset 18)
 *                    char           d_name[]; (offset 19)
 *                };
 *
 *            "." and ".." are real records there, so the reader below drops
 *            them -- otherwise every listing would differ by two entries
 *            depending on which platform ran it.
 *
 * Only functions this file defines are emitted; a program that never touches
 * a directory pays nothing.
 * ========================================================================== */

#if defined(_WIN32)

#include <windows.h>

#define FILE_ATTRIBUTE_DIRECTORY 0x10
#define FILE_ATTRIBUTE_READONLY  0x1
#define INVALID_HANDLE_VALUE     ((HANDLE)-1)

extern DWORD  GetCurrentDirectoryA(DWORD nBufferLength, LPCSTR lpBuffer), kernel32;
extern BOOL   SetFileAttributesA(LPCSTR lpFileName, DWORD dwFileAttributes), kernel32;

/* FILETIME is 100ns ticks since 1601; Unix time is seconds since 1970. */
static long ft_to_unix(FILETIME ft) {
    unsigned long long t = ((unsigned long long)ft.dwHighDateTime << 32)
                         | (unsigned long long)ft.dwLowDateTime;
    return (long)(t / 10000000ULL - 11644473600ULL);
}

DIR *opendir(const char *name) {
    HANDLE h;
    void *data;
    DIR *d;
    char pat[520];
    int i, n;
    if (!name) return 0;
    n = 0;
    for (i = 0; name[i]; i++) pat[n++] = name[i];
    if (n > 0 && pat[n-1] != '/' && pat[n-1] != '\\')
        pat[n++] = '/';
    pat[n++] = '*';
    pat[n] = '\0';
    data = malloc(sizeof(WIN32_FIND_DATAA));
    if (!data) return 0;
    h = FindFirstFileA(pat, data);
    if (h == INVALID_HANDLE_VALUE) {
        free(data);
        return 0;
    }
    d = (DIR *)malloc(sizeof(DIR));
    if (!d) {
        FindClose(h);
        free(data);
        return 0;
    }
    memset(d, 0, sizeof(DIR));
    d->h = h;
    d->data = data;
    d->fd = -1;
    d->have = 1;                     /* FindFirstFileA already holds entry 1 */
    return d;
}

struct dirent *readdir(DIR *d) {
    WIN32_FIND_DATAA *fd;
    int i;
    if (!d || !d->have) return 0;
    fd = (WIN32_FIND_DATAA *)d->data;
    d->ent.d_ino = 0;
    for (i = 0; i < 255 && fd->cFileName[i] != '\0'; i++)
        d->ent.d_name[i] = fd->cFileName[i];
    d->ent.d_name[i] = '\0';
    d->have = FindNextFileA(d->h, d->data) != 0;
    return &d->ent;
}

int closedir(DIR *d) {
    if (!d) return -1;
    if (d->h) FindClose(d->h);
    if (d->data) free(d->data);
    free(d);
    return 0;
}

int stat(const char *path, struct stat *buf) {
    WIN32_FILE_ATTRIBUTE_DATA fa;
    if (!GetFileAttributesExA(path, 0, &fa)) return -1;
    buf->st_size = ((unsigned long)fa.nFileSizeHigh << 32) |
                   (unsigned long)fa.nFileSizeLow;
    if (fa.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY)
        buf->st_mode = S_IFDIR | 0777;
    else
        buf->st_mode = S_IFREG | 0666;
    buf->st_mtime = ft_to_unix(fa.ftLastWriteTime);
    return 0;
}

int mkdir(const char *path, unsigned int mode) {
    return CreateDirectoryA(path, 0) ? 0 : -1;
}

int rmdir(const char *path) {
    return RemoveDirectoryA(path) ? 0 : -1;
}

char *getcwd(char *buf, size_t size) {
    DWORD n = GetCurrentDirectoryA((DWORD)size, buf);
    if (n == 0 || n >= (DWORD)size) return 0;
    return buf;
}

int chmod(const char *path, int mode) {
    DWORD a = GetFileAttributesA(path);
    if (a == (DWORD)-1) return -1;
    /* Windows has no Unix permission bits: only the writable bit maps to the
     * read-only attribute, which is the most kernel32 can express here. */
    if (mode & 0222) a &= ~FILE_ATTRIBUTE_READONLY;
    else a |= FILE_ATTRIBUTE_READONLY;
    return SetFileAttributesA(path, a) ? 0 : -1;
}

int access(const char *path, int amode) {
    DWORD a = GetFileAttributesA(path);
    if (a == (DWORD)-1) return -1;
    (void)amode;               /* no Unix permission model: existence suffices */
    return 0;
}

int lstat(const char *path, struct stat *buf) {
    return stat(path, buf);    /* no symlink support: identical to stat */
}

int fstat(int fd, struct stat *buf) {
    (void)fd; (void)buf;
    return -1;                 /* Windows has no POSIX int-fd open(): use stat() */
}

#elif defined(__linux__)

/* The raw system calls. Each __goclib_ alias is the raw syscall under a name
 * that cannot collide with the public wrapper this same file defines (stat,
 * mkdir, rmdir); <syscall.h> says how each one is reached on each host. */

DIR *opendir(const char *name) {
    DIR *d;
    long fd;
    if (!name) return 0;
    fd = open(name, 0, 0);           /* O_RDONLY */
    if (fd < 0) return 0;
    d = (DIR *)malloc(sizeof(DIR));
    if (!d) {
        close(fd);
        return 0;
    }
    memset(d, 0, sizeof(DIR));
    d->fd = fd;
    d->have = 0;
    d->pos = 0;
    d->len = 0;
    return d;
}

struct dirent *readdir(DIR *d) {
    char *rec;
    if (!d) return 0;
    for (;;) {
        if (d->pos >= d->len) {
            long n = __goclib_getdents64(d->fd, d->buf, (long)sizeof(d->buf));
            if (n <= 0) return 0;
            d->pos = 0;
            d->len = (int)n;
        }
        rec = d->buf + d->pos;
        {
            unsigned short reclen = *(unsigned short *)(rec + 16);
            char *nm = rec + 19;
            int i;
            if (reclen <= 0) return 0;        /* malformed record: stop */
            d->pos = d->pos + (int)reclen;
            /* Skip "." and ".." so a listing is the same on both targets. */
            if (nm[0] == '.' && (nm[1] == '\0' ||
                                 (nm[1] == '.' && nm[2] == '\0')))
                continue;
            d->ent.d_ino = *(unsigned long *)rec;
            for (i = 0; i < 255 && nm[i] != '\0'; i++)
                d->ent.d_name[i] = nm[i];
            d->ent.d_name[i] = '\0';
            return &d->ent;
        }
    }
}

int closedir(DIR *d) {
    long r;
    if (!d) return -1;
    r = close(d->fd);
    free(d);
    return r == 0 ? 0 : -1;
}

int stat(const char *path, struct stat *buf) {
    return __goclib_stat(path, (void *)buf) == 0 ? 0 : -1;
}

int mkdir(const char *path, unsigned int mode) {
    return __goclib_mkdir(path, (long)mode) == 0 ? 0 : -1;
}

int rmdir(const char *path) {
    return __goclib_rmdir(path) == 0 ? 0 : -1;
}

char *getcwd(char *buf, size_t size) {
    if (__goclib_getcwd(buf, (long)size) < 0) return 0;
    return buf;
}

int chmod(const char *path, int mode) {
    return __goclib_chmod(path, (long)mode) == 0 ? 0 : -1;
}

int access(const char *path, int amode) {
    (void)amode;
    return __goclib_access(path, (long)amode) == 0 ? 0 : -1;
}

int lstat(const char *path, struct stat *buf) {
    return stat(path, buf);    /* no symlink support: identical to stat */
}

int fstat(int fd, struct stat *buf) {
    return __goclib_fstat((long)fd, (void *)buf) == 0 ? 0 : -1;
}

#endif
