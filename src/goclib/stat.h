#ifndef GOC_STAT_H
#define GOC_STAT_H

#include <stddef.h>   /* size_t, for getcwd() below */

/* <sys/stat.h> -- file metadata.
 *
 * Only the three fields a portable program can rely on are named the same on
 * both platforms: st_size, st_mode and st_mtime. The Linux layout is pinned
 * to the kernel's x86-64 `struct stat` (144 bytes) because the stat syscall
 * fills it directly -- reordering those fields would silently read the wrong
 * bytes. The Windows layout is our own and is filled from
 * GetFileAttributesExA.
 *
 * mkdir()'s mode argument is kept for source compatibility and ignored:
 * neither platform's directory creation takes a permission mask here. */

#define S_IFMT   61440           /* 0170000: file type field */
#define S_IFDIR  16384           /* 0040000 */
#define S_IFREG  32768           /* 0100000 */
#define S_ISDIR(m)  (((m) & S_IFMT) == S_IFDIR)
#define S_ISREG(m)  (((m) & S_IFMT) == S_IFREG)

/* access() request bits. */
#define F_OK 0
#define X_OK 1
#define W_OK 2
#define R_OK 4

#if defined(_WIN32)

struct stat {
    unsigned long st_size;
    unsigned int  st_mode;
    long          st_mtime;
};

#else

struct stat {
    unsigned long st_dev;
    unsigned long st_ino;
    unsigned long st_nlink;
    unsigned int  st_mode;       /* offset 24 */
    unsigned int  st_uid;
    unsigned int  st_gid;
    unsigned int  st_pad0;
    unsigned long st_rdev;
    long          st_size;       /* offset 48 */
    long          st_blksize;
    long          st_blocks;
    long          st_atim_sec;
    long          st_atim_nsec;
    long          st_mtim_sec;   /* offset 88 */
    long          st_mtim_nsec;
    long          st_ctim_sec;
    long          st_ctim_nsec;
    long          st_pad1;
    long          st_pad2;
    long          st_pad3;
};

#endif

#if defined(_WIN32)
/* Windows metadata is read through these kernel32 APIs; the import DLL is named
 * inline so no central win32.def entry is required. */
#include <windef.h>
extern DWORD  GetFileAttributesA(LPCSTR name), kernel32;
extern BOOL   GetFileAttributesExA(LPCSTR name, long level, LPVOID data), kernel32;
#endif

int stat(const char *path, struct stat *buf);
int mkdir(const char *path, unsigned int mode);
int rmdir(const char *path);

/* POSIX additions. lstat is identical to stat here (no symlink support);
 * fstat on Linux reads the kernel's struct stat via the fd, and on Windows
 * returns -1 because goc's Windows port has no POSIX int-fd open(). */
int    lstat(const char *path, struct stat *buf);
int    fstat(int fd, struct stat *buf);
/* Current working directory into buf (size bytes); NULL on error. */
char  *getcwd(char *buf, size_t size);
/* chmod maps only the writable bit to the read-only attribute on Windows. */
int    chmod(const char *path, int mode);
/* access checks existence (no Unix permission model on either target). */
int    access(const char *path, int amode);

#endif /* GOC_STAT_H */
