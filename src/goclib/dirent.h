#ifndef GOC_DIRENT_H
#define GOC_DIRENT_H

/* <dirent.h> -- directory enumeration.
 *
 * The shape is POSIX's, the implementation is not shared: Windows walks the
 * directory with FindFirstFileA/FindNextFileA while Linux reads it with the
 * getdents64 syscall. The DIR below carries both platforms' state so one
 * header can describe it, and the reader in dir.c owns the difference.
 *
 * The "." and ".." entries are NOT reported on either platform: Windows never
 * produces them and the Linux reader filters them out, so a program that
 * lists a directory sees the same names wherever it runs. */

struct dirent {
    unsigned long d_ino;      /* 0: neither platform hands out inode numbers */
    char          d_name[256];
};

typedef struct {
    void         *h;          /* Windows: search handle from FindFirstFileA */
    void         *data;       /* Windows: WIN32_FIND_DATAA* (heap) */
    long          fd;         /* Linux: directory fd from open(), else -1 */
    int           have;       /* an entry is live and not yet returned */
    int           pos;        /* Linux: read offset into buf */
    int           len;        /* Linux: valid bytes in buf */
    char          buf[1024];  /* Linux: raw getdents64 records */
    struct dirent ent;        /* the entry readdir() hands back */
} DIR;

DIR           *opendir(const char *name);
struct dirent *readdir(DIR *d);
int            closedir(DIR *d);

#if defined(_WIN32)
/* Windows directory enumeration rides on these kernel32 APIs. The import DLL
 * is named inline on each prototype, so no central win32.def entry is needed. */
#include <windef.h>
extern HANDLE FindFirstFileA(LPCSTR pattern, LPVOID data), kernel32;
extern BOOL   FindNextFileA(HANDLE h, LPVOID data), kernel32;
extern BOOL   FindClose(HANDLE h), kernel32;
extern BOOL   CreateDirectoryA(LPCSTR path, LPVOID secattr), kernel32;
extern BOOL   RemoveDirectoryA(LPCSTR path), kernel32;
#endif

#endif /* GOC_DIRENT_H */
