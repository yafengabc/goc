#include "goclib.h"
#include <stdarg.h>
#include <stdio.h>

/* =============================================================================
 * file.c -- the buffered FILE layer behind fopen()/fread()/fwrite()/...
 *
 * FILE is an opaque struct (typedef'd in stdio.h). _fd is the OS identity of
 * the stream: on Linux an int file descriptor from open(); on Windows a HANDLE
 * from CreateFileA. Both fit losslessly in a long, so the whole library is
 * portable C that reaches the OS through __goclib_* heap primitives plus the
 * per-platform externs declared below (kernel32 on Windows, syscalls on Linux).
 *
 * Each stream carries a small I/O buffer. Read streams fill the buffer from the
 * OS and hand out bytes; write streams accumulate and flush in one OS call.
 * Buffering keeps fgetc/fputc cheap without changing observable behaviour.
 * ========================================================================== */

typedef struct __goclib_FILE {
    long  _fd;        /* OS handle: fd (Linux) or HANDLE (Windows) */
    int   _readable;  /* opened for reading */
    int   _writable;  /* opened for writing */
    int   _append;    /* writes always go to the current end of file */
    int   _eof;      /* end-of-file flag */
    int   _err;      /* error flag */
    char *_base;      /* buffer (0 if unbuffered) */
    long  _size;      /* buffer capacity in bytes */
    long  _pos;       /* read: next byte to return; write: buffered bytes */
    long  _len;       /* read: valid bytes held in the buffer */
    long  _off;       /* read: file offset of buffer[0]; write: next write target;
                         -1 = sequential stream (std*): never seek, OS-positioned */
    int   _unget;     /* one pushed-back char, or -1 */
    int   _own;       /* 1 if _base and the FILE itself were heap-allocated */
} __goclib_FILE;

#if defined(_WIN32)
/* ---- Windows: kernel32 externs (mapped to DLLs by win32.def) ------------- */
extern void *CreateFileA(const char *name, long access, long share,
                        long secattr, long disp, long flags, long templ);
extern long  ReadFile(void *h, void *buf, long n, long *got, long overlapped);
extern long  WriteFile(void *h, const void *buf, long n, long *written, long overlapped);
extern long  CloseHandle(void *h);
extern long  SetFilePointer(void *h, long lo, long *hi, long whence);
extern long  GetFileSize(void *h, long *hi);
extern long  FlushFileBuffers(void *h);
extern void *GetStdHandle(long which);
extern long  DeleteFileA(const char *name);
extern long  MoveFileA(const char *oldp, const char *newp);

#define FA_READ          0x80000000L
#define FA_WRITE         0x40000000L
#define FA_SHARE         0x00000003L  /* FILE_SHARE_READ|WRITE */
#define FA_OPEN_EXISTING 3
#define FA_CREATE_ALWAYS 2
#define FA_OPEN_ALWAYS   4
#define FA_NORMAL        0x80L
#define FILE_BEGIN 0
#define FILE_CURRENT 1
#define FILE_END   2
#else
/* ---- Linux: syscall stubs (goa turns these into `mov rax,N; syscall`) ---- */
extern long open(const char *path, long flags, long mode);
extern long read(long fd, void *buf, long n);
extern long write(long fd, const void *buf, long n);
extern long close(long fd);
extern long lseek(long fd, long offset, long whence);
extern long unlink(const char *path);
/* The rename syscall carries the __goclib_ prefix on purpose: this file also
 * defines the public rename() wrapper, and a plain `extern rename` would
 * resolve to it -- an infinite recursion (the wrapper calling itself) that
 * burns the whole stack. Same trick musl uses for its internal names. */
extern long __goclib_rename(const char *oldp, const char *newp);

#define LO_RDONLY 0
#define LO_WRONLY 1
#define LO_RDWR   2
#define LO_CREAT  0x40
#define LO_TRUNC  0x200
#define LO_APPEND 0x400
#define SEEK_SET  0
#define SEEK_CUR  1
#define SEEK_END  2
#endif

/* ---- OS raw I/O helpers (insulate the buffered layer from the platform) --- */

static long __goclib_os_read_at(long fd, void *buf, long len, long offset) {
#if defined(_WIN32)
    void *h = (void *)fd;
    long hi = 0, lo = SetFilePointer(h, offset, &hi, FILE_BEGIN);
    if (lo == -1) return -1;
    long got = 0;
    if (!ReadFile(h, buf, len, &got, 0)) return -1;
    return got;
#else
    lseek(fd, offset, SEEK_SET);
    return read(fd, buf, len);
#endif
}

static long __goclib_os_write_at(long fd, const void *buf, long len, long offset) {
#if defined(_WIN32)
    void *h = (void *)fd;
    long hi = 0;
    if (SetFilePointer(h, offset, &hi, FILE_BEGIN) == -1) return -1;
    const char *p = (const char *)buf;
    long total = 0;
    while (len > 0) {
        long w = 0;
        if (!WriteFile(h, p, len, &w, 0)) return -1;
        if (w <= 0) return -1;
        p += w; len -= w; total += w;
    }
    return total;
#else
    lseek(fd, offset, SEEK_SET);
    const char *p = (const char *)buf;
    long total = 0;
    while (len > 0) {
        long w = write(fd, p, len);
        if (w <= 0) return -1;
        p += w; len -= w; total += w;
    }
    return total;
#endif
}

static long __goclib_os_seek(long fd, long offset, long whence) {
#if defined(_WIN32)
    void *h = (void *)fd;
    long hi = 0, lo = SetFilePointer(h, offset, &hi, whence);
    return lo;  /* assume file < 4GB: high 32 bits unused */
#else
    return lseek(fd, offset, whence);
#endif
}

/* Sequential (no-seek) I/O for the standard streams. Seeking a console or
 * pipe fails outright, and seeking a redirected FILE destroys the handle's
 * current position -- which the os.c write path (print builtin) relies on.
 * Sequential streams therefore read/write at the OS's own position and set
 * _off = -1 so the buffered layer knows never to seek or track an offset. */
static long __goclib_os_read_seq(long fd, void *buf, long len) {
#if defined(_WIN32)
    long got = 0;
    if (!ReadFile((void *)fd, buf, len, &got, 0)) return -1;
    return got;
#else
    return read(fd, buf, len);
#endif
}

static long __goclib_os_write_seq(long fd, const void *buf, long len) {
#if defined(_WIN32)
    const char *p = (const char *)buf;
    long total = 0;
    while (len > 0) {
        long w = 0;
        if (!WriteFile((void *)fd, p, len, &w, 0)) return -1;
        if (w <= 0) return -1;
        p += w; len -= w; total += w;
    }
    return total;
#else
    const char *p = (const char *)buf;
    long total = 0;
    while (len > 0) {
        long w = write(fd, p, len);
        if (w <= 0) return -1;
        p += w; len -= w; total += w;
    }
    return total;
#endif
}

/* Buffered-layer I/O: honours the _off < 0 sequential convention and keeps
 * _off updated for seekable streams. */
static long __goclib_file_read(__goclib_FILE *f, void *buf, long len) {
    if (f->_off < 0) return __goclib_os_read_seq(f->_fd, buf, len);
    return __goclib_os_read_at(f->_fd, buf, len, f->_off + f->_len);
}

static long __goclib_file_write(__goclib_FILE *f, const void *buf, long len) {
    long w;
    if (f->_off < 0) return __goclib_os_write_seq(f->_fd, buf, len);
    w = __goclib_os_write_at(f->_fd, buf, len, f->_off);
    if (w > 0) f->_off += w;
    return w;
}

static long __goclib_os_size(long fd) {
#if defined(_WIN32)
    void *h = (void *)fd;
    long hi = 0, lo = GetFileSize(h, &hi);
    return lo;
#else
    long cur = lseek(fd, 0, SEEK_CUR);
    long end = lseek(fd, 0, SEEK_END);
    lseek(fd, cur, SEEK_SET);
    return end;
#endif
}

static long __goclib_os_close(long fd) {
#if defined(_WIN32)
    return CloseHandle((void *)fd) ? 0 : -1;
#else
    return close(fd);
#endif
}

static int __goclib_file_flush(__goclib_FILE *f) {
    if (f->_writable && f->_pos > 0 && f->_base) {
        if (f->_off >= 0 && f->_append) {
            long off = __goclib_os_seek(f->_fd, 0, SEEK_END);
            if (off < 0) { f->_err = 1; return -1; }
            f->_off = off;
        }
        long done = __goclib_file_write(f, f->_base, f->_pos);
        if (done != f->_pos) {
            f->_err = 1;
            return -1;
        }
        f->_pos = 0;
    }
    return 0;
}

/* ----------------------------- fopen / fclose ----------------------------- */

FILE *fopen(const char *path, const char *mode) {
    __goclib_FILE *f;
    int readable = 0, writable = 0, creat = 0, trunc = 0, append = 0;
    long fd, i;

    if (mode == 0 || path == 0) return 0;
    switch (mode[0]) {
        case 'r': readable = 1; break;
        case 'w': writable = 1; creat = 1; trunc = 1; break;
        case 'a': writable = 1; creat = 1; append = 1; break;
        default:  return 0;
    }
    /* '+' adds the missing direction; 'b'/'t' are accepted and ignored. */
    for (i = 0; mode[i]; i++) {
        if (mode[i] == 'b' || mode[i] == 't') continue;
        if (mode[i] == '+') {
            if (readable) writable = 1;
            else if (writable) readable = 1;
        }
    }

#if defined(_WIN32)
    {
        long access = 0, disp = 0;
        if (readable && writable) access = FA_READ | FA_WRITE;
        else if (readable)       access = FA_READ;
        else                     access = FA_WRITE;
        disp = creat ? ((trunc || append) ? FA_CREATE_ALWAYS : FA_OPEN_ALWAYS)
                     : FA_OPEN_EXISTING;
        {
            void *h = CreateFileA(path, access, FA_SHARE, 0, disp, FA_NORMAL, 0);
            if ((long)h == -1) return 0;
            fd = (long)h;
        }
    }
#else
    {
        long flags = 0;
        if (readable && writable) flags = LO_RDWR;
        else if (readable)       flags = LO_RDONLY;
        else                     flags = LO_WRONLY;
        if (creat)  flags |= LO_CREAT;
        if (trunc)  flags |= LO_TRUNC;
        if (append) flags |= LO_APPEND;
        fd = open(path, flags, 0644);
        if (fd < 0) return 0;
    }
#endif

    f = (__goclib_FILE *)__goclib_heap_alloc(sizeof(__goclib_FILE));
    if (f == 0) { __goclib_os_close(fd); return 0; }
    f->_fd = fd;
    f->_readable = readable; f->_writable = writable; f->_append = append;
    f->_eof = 0; f->_err = 0; f->_unget = -1;
    f->_pos = 0; f->_len = 0; f->_off = 0;
    f->_base = (char *)__goclib_heap_alloc(4096);
    f->_own = 1;
    if (f->_base == 0) { f->_size = 0; f->_own = 0; }  /* fall back to unbuffered */
    else f->_size = 4096;
    if (append) f->_off = __goclib_os_size(fd);  /* ftell reflects the end */
    return (FILE *)f;
}

int fclose(FILE *stream) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    if (f == 0) return -1;
    __goclib_file_flush(f);
    if (f->_own) {                 /* heap-allocated stream (not a std stream) */
        __goclib_os_close(f->_fd);
        if (f->_base) __goclib_heap_free(f->_base);
        __goclib_heap_free(f);
    }
    return 0;
}

/* freopen re-associates an existing FILE (typically stdout) with a new file:
 * flush, drop the old association, then adopt the descriptor and buffer that
 * a fresh fopen() produced. The FILE * keeps its identity and its own flag
 * values (_own among them), so closing it later stays safe.
 *
 * A standard stream's _fd is the shared OS handle the os.c direct-write path
 * also uses (GetStdHandle), so it must NOT be closed here -- same rule as
 * fclose, which only closes _own streams. The old descriptor leaks, which is
 * harmless for a process about to be redirected or exiting. */
FILE *freopen(const char *path, const char *mode, FILE *stream) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    __goclib_FILE *nf;
    if (f == 0 || path == 0 || mode == 0) return 0;
    __goclib_file_flush(f);
    nf = (__goclib_FILE *)fopen(path, mode);
    if (nf == 0) return 0;
    if (f->_own) __goclib_os_close(f->_fd);
    if (f->_own && f->_base) __goclib_heap_free(f->_base);
    f->_fd = nf->_fd;
    f->_readable = nf->_readable;
    f->_writable = nf->_writable;
    f->_append = nf->_append;
    f->_eof = 0;
    f->_err = 0;
    f->_base = nf->_base;
    f->_size = nf->_size;
    f->_pos = 0;
    f->_len = 0;
    f->_off = nf->_off;
    f->_unget = -1;
    /* nf's buffer now belongs to f; only the FILE shell itself is dropped */
    __goclib_heap_free(nf);
    return (FILE *)f;
}

/* --------------------------- read / write --------------------------------- */

long fread(void *ptr, long size, long nmemb, FILE *stream) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    char *p = (char *)ptr;
    long want, done = 0;
    if (f == 0 || !f->_readable || size <= 0 || nmemb <= 0) return 0;
    want = size * nmemb;
    while (done < want) {
        long avail = f->_len - f->_pos;
        if (avail <= 0) {
            long got = __goclib_os_read_at(f->_fd, f->_base, f->_size,
                                           f->_off + f->_len);
            if (got < 0) { f->_err = 1; break; }
            if (got == 0) { f->_eof = 1; break; }
            f->_off += f->_len;
            f->_len = got; f->_pos = 0;
            avail = f->_len;
        }
        {
            long take = want - done;
            if (take > avail) take = avail;
            memcpy(p, f->_base + f->_pos, take);
            p += take; f->_pos += take; done += take;
        }
    }
    return (size > 0) ? (done / size) : 0;
}

long fwrite(const void *ptr, long size, long nmemb, FILE *stream) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    const char *p = (const char *)ptr;
    long want, done = 0;
    if (f == 0 || !f->_writable || size <= 0 || nmemb <= 0) return 0;
    want = size * nmemb;
    while (done < want) {
        if (f->_size > 0) {
            long space = f->_size - f->_pos;
            if (space <= 0) { if (__goclib_file_flush(f) != 0) break; space = f->_size; }
            {
                long take = want - done;
                if (take > space) take = space;
                memcpy(f->_base + f->_pos, p, take);
                p += take; f->_pos += take; done += take;
                if (f->_pos >= f->_size) __goclib_file_flush(f);
            }
        } else {
            long take = want - done;
            long w = __goclib_file_write(f, p, take);
            if (w <= 0) { f->_err = 1; break; }
            p += w; done += w;
        }
    }
    return (size > 0) ? (done / size) : 0;
}

int fgetc(FILE *stream) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    if (f == 0 || !f->_readable) { if (f) f->_err = 1; return -1; }
    if (f->_unget >= 0) { int c = f->_unget; f->_unget = -1; return (unsigned char)c; }
    if (f->_pos >= f->_len) {
        long got = __goclib_file_read(f, f->_base, f->_size);
        if (got < 0) { f->_err = 1; return -1; }
        if (got == 0) { f->_eof = 1; return -1; }
        if (f->_off >= 0) f->_off += f->_len;
        f->_len = got; f->_pos = 0;
    }
    return (unsigned char)f->_base[f->_pos++];
}

int fputc(int c, FILE *stream) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    if (f == 0 || !f->_writable) { if (f) f->_err = 1; return -1; }
    if (f->_size > 0) {
        if (f->_pos >= f->_size) { if (__goclib_file_flush(f) != 0) return -1; }
        f->_base[f->_pos++] = (char)c;
    } else {
        char b = (char)c;
        long w = __goclib_file_write(f, &b, 1);
        if (w <= 0) { f->_err = 1; return -1; }
    }
    return (unsigned char)c;
}

char *fgets(char *s, long n, FILE *stream) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    long i = 0;
    if (f == 0 || !f->_readable || n <= 0 || s == 0) return 0;
    while (i < n - 1) {
        int c = fgetc(f);
        if (c < 0) break;
        s[i++] = (char)c;
        if (c == '\n') break;
    }
    if (i == 0 && f->_eof) return 0;   /* nothing read at end of input */
    s[i] = 0;
    return s;
}

int fputs(const char *s, FILE *stream) {
    if (s == 0) return -1;
    fwrite(s, 1, (long)strlen(s), stream);
    return 0;
}

/* --------------------------- position / state ----------------------------- */

int fflush(FILE *stream) {
    if (stream == 0) return 0;
    __goclib_file_flush((__goclib_FILE *)stream);
    return 0;
}

long ftell(FILE *stream) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    if (f == 0) return -1;
    if (f->_off < 0) return -1;   /* sequential stream: no meaningful offset */
    return f->_off + f->_pos;
}

int fseek(FILE *stream, long offset, int whence) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    long newoff;
    if (f == 0) return -1;
    if (f->_off < 0) { f->_err = 1; return -1; }  /* cannot seek a std stream */
    if (f->_writable) __goclib_file_flush(f);
    f->_pos = 0; f->_len = 0; f->_unget = -1;   /* discard buffered bytes */
    if (whence == 0) newoff = offset;
    else if (whence == 1) newoff = f->_off + f->_pos + offset;
    else newoff = __goclib_os_size(f->_fd) + offset;
    f->_off = newoff;
    f->_eof = 0;
    return 0;
}

void rewind(FILE *stream) {
    fseek(stream, 0, SEEK_SET);
    clearerr(stream);
}

int feof(FILE *stream) {
    return (stream != 0) ? ((__goclib_FILE *)stream)->_eof : 0;
}

int ferror(FILE *stream) {
    return (stream != 0) ? ((__goclib_FILE *)stream)->_err : 0;
}

void clearerr(FILE *stream) {
    if (stream != 0) {
        __goclib_FILE *f = (__goclib_FILE *)stream;
        f->_eof = 0; f->_err = 0;
    }
}

int ungetc(int c, FILE *stream) {
    __goclib_FILE *f = (__goclib_FILE *)stream;
    if (f == 0 || f->_unget >= 0) return -1;
    f->_unget = (unsigned char)c;
    f->_eof = 0;
    return (unsigned char)c;
}

/* ----------------------------- filesystem ---------------------------------- */

int remove(const char *path) {
#if defined(_WIN32)
    return DeleteFileA(path) ? 0 : -1;
#else
    return (unlink(path) == 0) ? 0 : -1;
#endif
}

int rename(const char *oldp, const char *newp) {
#if defined(_WIN32)
    return MoveFileA(oldp, newp) ? 0 : -1;
#else
    return (__goclib_rename(oldp, newp) == 0) ? 0 : -1;
#endif
}

FILE *tmpfile(void) {
    /* Minimal: a uniquely-named temp file in the current directory. Unlike
     * the C standard it is not auto-deleted on close, but it is valid for
     * read+write scratch use. */
    static long seq = 0;
    char name[40];
    long i = 0, v;
    const char *pre = "goc_tmp_";
    while (pre[i]) { name[i] = pre[i]; i++; }
    v = ++seq;
    if (v == 0) { name[i++] = '0'; }
    else {
        char t[20]; int k = 0, t2;
        while (v > 0) { t[k++] = (char)('0' + (v % 10)); v /= 10; }
        while (k > 0) { name[i++] = t[--k]; }
    }
    name[i++] = '.'; name[i++] = 't'; name[i++] = 'm'; name[i++] = 'p'; name[i] = 0;
    return fopen(name, "wb+");
}

int setvbuf(FILE *stream, char *buf, int mode, long size) {
    (void)stream; (void)buf; (void)mode; (void)size;
    return 0;
}

/* ------------------------ standard streams --------------------------------
 * The three standard streams are file-scope FILE structs whose buffers are
 * static arrays (never freed). _own stays 0 so fclose() only flushes them.
 * stdout/stderr/stdin are exposed as macros (see stdio.h) that call the
 * accessors below -- a macro keeps them usable as expressions everywhere
 * while letting the handles be resolved at runtime.
 * ------------------------------------------------------------------------- */
char __goclib_in_buf[4096];
char __goclib_out_buf[4096];
char __goclib_err_buf[256];
__goclib_FILE __goclib_stdin_file;
__goclib_FILE __goclib_stdout_file;
__goclib_FILE __goclib_stderr_file;
static int __goclib_streams_inited = 0;

static void __goclib_init_streams(void) {
    if (__goclib_streams_inited) return;
    __goclib_streams_inited = 1;
#if defined(_WIN32)
    __goclib_stdin_file._fd  = (long)GetStdHandle(-10);
    __goclib_stdout_file._fd = (long)GetStdHandle(-11);
    __goclib_stderr_file._fd = (long)GetStdHandle(-12);
#else
    __goclib_stdin_file._fd  = 0;
    __goclib_stdout_file._fd = 1;
    __goclib_stderr_file._fd = 2;
#endif
    __goclib_stdin_file._readable = 1;  __goclib_stdin_file._writable = 0;
    __goclib_stdout_file._readable = 0; __goclib_stdout_file._writable = 1;
    __goclib_stderr_file._readable = 0; __goclib_stderr_file._writable = 1;
    __goclib_stdin_file._base  = __goclib_in_buf;  __goclib_stdin_file._size  = 4096;
    /* stdout/stderr are unbuffered: the entry stub exits via exit_group /
     * ExitProcess directly, so a buffered stream that is never fflush'd or
     * fclose'd at the end of main would lose its last writes. Console output
     * going straight to the OS also keeps printf/fprintf(stdout) interleaving
     * correct. stdin stays buffered for efficient line reads. */
    __goclib_stdout_file._base = __goclib_out_buf; __goclib_stdout_file._size = 0;
    __goclib_stderr_file._base = __goclib_err_buf; __goclib_stderr_file._size = 0;
    __goclib_stdin_file._own = 0;  __goclib_stdout_file._own = 0; __goclib_stderr_file._own = 0;
    /* _off = -1 marks the std streams as SEQUENTIAL: I/O happens at the
     * OS handle's own position with no seek, so output from the os.c
     * direct-write path (print builtin, __goclib_write) and from this
     * FILE layer interleaves correctly on the same redirected handle. */
    __goclib_stdin_file._pos = 0;  __goclib_stdin_file._len = 0;  __goclib_stdin_file._off = -1;
    __goclib_stdout_file._pos = 0; __goclib_stdout_file._len = 0; __goclib_stdout_file._off = -1;
    __goclib_stderr_file._pos = 0; __goclib_stderr_file._len = 0; __goclib_stderr_file._off = -1;
    __goclib_stdin_file._unget = -1;  __goclib_stdout_file._unget = -1; __goclib_stderr_file._unget = -1;
    __goclib_stdin_file._eof = 0; __goclib_stdout_file._eof = 0; __goclib_stderr_file._eof = 0;
    __goclib_stdin_file._err = 0; __goclib_stdout_file._err = 0; __goclib_stderr_file._err = 0;
}

FILE *__goclib_stdin(void)  { __goclib_init_streams(); return (FILE *)&__goclib_stdin_file; }
FILE *__goclib_stdout(void) { __goclib_init_streams(); return (FILE *)&__goclib_stdout_file; }
FILE *__goclib_stderr(void) { __goclib_init_streams(); return (FILE *)&__goclib_stderr_file; }
