/*
 * ucrun.c -- run a Linux x86-64 static ELF under the Unicorn engine.
 *
 * This is a straight port of tools/ucrun.py to a standalone C program. It must
 * be a SEPARATE C process (not called from inside the Go runtime): Unicorn is a
 * C++ library that installs its own SEH / exception handling, which conflicts
 * with Go's Vectored Exception Handler on Windows and crashes the emulation
 * (uc_mem_map faults) when hosted inside a Go process. A plain C process
 * (like the original Python/ctypes wrapper) works fine.
 *
 * Usage:
 *     ucrun.exe <linux-elf> [args...]
 * Exit code: the guest program's exit code (masked to 0xFF).
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdbool.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <direct.h>
#include <windows.h>
#include <unicorn/unicorn.h>

#define STACK_TOP   0x7ffc00000000ULL
#define STACK_SIZE  0x100000ULL
#define PAGE        0x1000ULL

/* ---- global emulation state (single-threaded emulation) ---- */

typedef struct {
    uc_engine *uc;
    int        exit_code;     /* default 0; set by exit syscall / faults */
    uint64_t   brk_cur;       /* current break (may be unaligned) */
    uint64_t   heap_top;      /* page-aligned top of mapped heap */
    int        fds[256];      /* guest_fd -> host fd; -1 = free */
    int        next_fd;
    char       tmpdir[MAX_PATH];
    char       origdir[MAX_PATH]; /* cwd before we chdir'd into tmpdir */
} Runner;

static Runner G;

static void rmtree(const char *dir);

/* Close every guest fd still open at shutdown. A guest that exits without
 * fclose leaves a host handle open; on Windows an open file cannot be deleted
 * and the temp dir would leak. */
static void close_guest_fds(void) {
    for (int i = 0; i < 256; i++) {
        if (G.fds[i] >= 0) {
            close(G.fds[i]);
            G.fds[i] = -1;
        }
    }
}

/* chdir back out of the temp dir: Windows refuses to delete a directory that
 * is any process's current working directory, so this must happen before the
 * rmtree -- otherwise every single run leaks one ucrun_* directory. */
static void leave_tmpdir(void) {
    SetCurrentDirectoryA(G.origdir[0] ? G.origdir : "C:\\");
}

/* Best-effort end-of-run cleanup, also used from the crash handler. */
static void cleanup_tmpdir(void) {
    if (G.tmpdir[0]) {
        leave_tmpdir();
        close_guest_fds();
        rmtree(G.tmpdir);
        G.tmpdir[0] = '\0';
    }
}

/* Crash handler: if the emulation dies hard (a fault inside Unicorn that our
 * hooks could not intercept), the normal end-of-main cleanup never runs and
 * the per-run temp directory would leak into the temp root. */
static LONG WINAPI crash_cleanup(EXCEPTION_POINTERS *ep) {
    (void)ep;
    cleanup_tmpdir();
    return EXCEPTION_CONTINUE_SEARCH;
}

static Runner G;

static uint64_t align_up(uint64_t x, uint64_t a) {
    return (x + a - 1) & ~(a - 1);
}

/* ---- register / memory helpers ---- */

static uint64_t reg_read(int reg) {
    uint64_t v = 0;
    uc_reg_read(G.uc, reg, &v);
    return v;
}
static void reg_write(int reg, uint64_t v) {
    uc_reg_write(G.uc, reg, &v);
}

/* Read a NUL-terminated C string from guest memory into buf (maxlen bytes). */
static void read_cstr(uint64_t addr, char *buf, int maxlen) {
    int i = 0;
    unsigned char b = 0;
    while (i < maxlen - 1) {
        uc_mem_read(G.uc, addr + (uint64_t)i, &b, 1);
        if (b == 0) break;
        buf[i++] = (char)b;
    }
    buf[i] = 0;
}

/* ---- file-descriptor table ---- */

static int fd_alloc(int hostfd) {
    int gfd = G.next_fd;
    if (gfd >= 256) return -1;
    G.fds[gfd] = hostfd;
    G.next_fd++;
    return gfd;
}
static int fd_lookup(int gfd) {
    if (gfd < 0 || gfd >= 256) return -1;
    return G.fds[gfd];
}

/* ---- heap (brk) growth, mirrors the kernel rule ---- */

static uint64_t grow_heap(uint64_t addr) {
    if (addr <= G.brk_cur) return G.brk_cur;
    uint64_t end = align_up(addr, PAGE);
    if (end > G.heap_top) {
        uint64_t need = end - G.heap_top;
        uc_mem_map(G.uc, G.heap_top, need, UC_PROT_READ | UC_PROT_WRITE);
        G.heap_top = end;
    }
    G.brk_cur = addr;
    return G.brk_cur;
}

/* ---- syscall dispatch (called from the code hook on 0F 05) ---- */

static void do_syscall(void) {
    uint64_t n   = reg_read(UC_X86_REG_RAX);
    uint64_t rdi = reg_read(UC_X86_REG_RDI);
    uint64_t rsi = reg_read(UC_X86_REG_RSI);
    uint64_t rdx = reg_read(UC_X86_REG_RDX);

    if (n == 60 || n == 231) {            /* exit / exit_group */
        G.exit_code = (int)(rdi & 0xFF);
        uc_emu_stop(G.uc);
        return;
    } else if (n == 1) {                  /* write */
        uint64_t buf = rsi;
        int64_t  cnt = (int64_t)rdx;
        if (cnt < 0) cnt = 0;
        unsigned char *data = malloc((size_t)cnt ? (size_t)cnt : 1);
        if (!data) { reg_write(UC_X86_REG_RAX, (uint64_t)-1); return; }
        if (cnt) uc_mem_read(G.uc, buf, data, (size_t)cnt);
        if (rdi == 1 || rdi == 2) {      /* stdout / stderr -> console */
            fwrite(data, 1, (size_t)cnt, rdi == 1 ? stdout : stderr);
            fflush(rdi == 1 ? stdout : stderr);
            reg_write(UC_X86_REG_RAX, (uint64_t)cnt);
        } else {
            int h = fd_lookup((int)rdi);
            if (h < 0) {
                reg_write(UC_X86_REG_RAX, (uint64_t)-1);
            } else {
                ssize_t w = write(h, data, (size_t)cnt);
                reg_write(UC_X86_REG_RAX, (uint64_t)(w < 0 ? -1 : w));
            }
        }
        free(data);
    } else if (n == 0) {                  /* read */
        uint64_t buf = rsi;
        int64_t  cnt = (int64_t)rdx;
        if (cnt < 0) cnt = 0;
        if (rdi == 0) {                   /* stdin: not wired up -> EOF */
            reg_write(UC_X86_REG_RAX, 0);
        } else {
            int h = fd_lookup((int)rdi);
            if (h < 0) {
                reg_write(UC_X86_REG_RAX, (uint64_t)-1);
            } else {
                unsigned char *data = malloc((size_t)cnt ? (size_t)cnt : 1);
                if (!data) { reg_write(UC_X86_REG_RAX, (uint64_t)-1); return; }
                ssize_t r = read(h, data, (size_t)cnt);
                if (r < 0) r = 0;
                if (r > 0) uc_mem_write(G.uc, buf, data, (size_t)r);
                reg_write(UC_X86_REG_RAX, (uint64_t)r);
                free(data);
            }
        }
    } else if (n == 2) {                  /* open */
        char path[4096];
        read_cstr(rdi, path, sizeof(path));
        int flags = (int)rsi;
        int mode  = (int)rdx;
        int oflags = (flags & 3) == 0 ? O_RDONLY :
                     ((flags & 3) == 1 ? O_WRONLY : O_RDWR);
        if (flags & 0x40)  oflags |= O_CREAT;   /* O_CREAT  = 0o100  */
        if (flags & 0x200) oflags |= O_TRUNC;   /* O_TRUNC  = 0o1000 */
        if (flags & 0x400) oflags |= O_APPEND;  /* O_APPEND = 0o2000 */
        int h = open(path, oflags, mode);
        if (h < 0) {
            reg_write(UC_X86_REG_RAX, (uint64_t)-1);
        } else {
            int gfd = fd_alloc(h);
            reg_write(UC_X86_REG_RAX, gfd < 0 ? (uint64_t)-1 : (uint64_t)gfd);
        }
    } else if (n == 3) {                  /* close */
        int h = fd_lookup((int)rdi);
        if (h < 0) {
            reg_write(UC_X86_REG_RAX, (uint64_t)-1);
        } else {
            close(h);
            G.fds[(int)rdi] = -1;
            reg_write(UC_X86_REG_RAX, 0);
        }
    } else if (n == 8) {                  /* lseek */
        int h = fd_lookup((int)rdi);
        if (h < 0) {
            reg_write(UC_X86_REG_RAX, (uint64_t)-1);
        } else {
            off_t r = lseek(h, (off_t)rsi, (int)rdx);
            reg_write(UC_X86_REG_RAX, (uint64_t)(r < 0 ? -1 : r));
        }
    } else if (n == 87) {                 /* unlink */
        char path[4096];
        read_cstr(rdi, path, sizeof(path));
        reg_write(UC_X86_REG_RAX, unlink(path) == 0 ? 0 : (uint64_t)-1);
    } else if (n == 82) {                 /* rename */
        char oldp[4096], newp[4096];
        read_cstr(rdi, oldp, sizeof(oldp));
        read_cstr(rsi, newp, sizeof(newp));
        reg_write(UC_X86_REG_RAX, rename(oldp, newp) == 0 ? 0 : (uint64_t)-1);
    } else if (n == 12) {                 /* brk */
        reg_write(UC_X86_REG_RAX, grow_heap(rdi));
    } else if (n == 228 || n == 96) {    /* clock_gettime / gettimeofday */
        uint64_t clk = rdi;
        uint64_t tp  = rsi;
        uint64_t sec = 0;
        uint64_t frac = 0;                /* nsec for 228, usec for 96 */
        FILETIME creation, exitt, kernel, user;
        ULONGLONG t = 0;
        if (n == 96 || clk == 0) {        /* wall clock: FILETIME -> epoch */
            FILETIME ft;
            GetSystemTimeAsFileTime(&ft);
            t = ((ULONGLONG)ft.dwHighDateTime << 32) | ft.dwLowDateTime;
            t -= 116444736000000000ULL;   /* 1601-01-01 -> 1970-01-01 */
            sec = t / 10000000ULL;
            frac = n == 96 ? (t / 10ULL) % 1000000ULL
                           : (t % 10000000ULL) * 100ULL;
        } else if (clk == 1) {            /* MONOTONIC: ms since boot */
            sec = GetTickCount64() / 1000ULL;
            frac = (GetTickCount64() % 1000ULL) *
                   (n == 96 ? 1000ULL : 1000000ULL);
        } else {                          /* per-process CPU time */
            if (GetProcessTimes(GetCurrentProcess(), &creation, &exitt,
                                &kernel, &user)) {
                t = ((ULONGLONG)user.dwHighDateTime << 32) | user.dwLowDateTime;
                t += ((ULONGLONG)kernel.dwHighDateTime << 32) | kernel.dwLowDateTime;
                sec = t / 10000000ULL;
                frac = n == 96 ? (t / 10ULL) % 1000000ULL
                               : (t % 10000000ULL) * 100ULL;
            }
        }
        /* Guest `struct timespec`/`timeval` is two 8-byte longs. */
        uc_mem_write(G.uc, tp, &sec, 8);
        uc_mem_write(G.uc, tp + 8, &frac, 8);
        reg_write(UC_X86_REG_RAX, 0);
    } else if (n == 4) {                 /* stat(path, buf) */
        /* Fill goclib's 144-byte Linux struct stat (st_mode @24, st_size @48,
         * st_mtime @88). The host is Windows, so the path is passed through
         * unchanged (. / ./x are valid here) and only the portable fields are
         * carried over; the POSIX mode bits line up with MSVCRT's _stat. */
        char path[4096];
        read_cstr(rdi, path, sizeof(path));
        struct _stat hs;
        if (_stat(path, &hs) != 0) {
            reg_write(UC_X86_REG_RAX, (uint64_t)-1);
        } else {
            unsigned char z[144];
            memset(z, 0, sizeof(z));
            uc_mem_write(G.uc, rsi, z, 144);
            uint32_t mode = (uint32_t)(hs.st_mode & 0xFFFF);
            int64_t  size = (int64_t)hs.st_size;
            int64_t  mtim = (int64_t)hs.st_mtime;
            uc_mem_write(G.uc, rsi + 24, &mode, 4);
            uc_mem_write(G.uc, rsi + 48, &size, 8);
            uc_mem_write(G.uc, rsi + 88, &mtim, 8);
            reg_write(UC_X86_REG_RAX, 0);
        }
    } else if (n == 5) {                 /* fstat(fd, buf) */
        int h = fd_lookup((int)rdi);
        if (h < 0) {
            reg_write(UC_X86_REG_RAX, (uint64_t)-1);
        } else {
            struct _stat hs;
            if (_fstat(h, &hs) != 0) {
                reg_write(UC_X86_REG_RAX, (uint64_t)-1);
            } else {
                unsigned char z[144];
                memset(z, 0, sizeof(z));
                uc_mem_write(G.uc, rsi, z, 144);
                uint32_t mode = (uint32_t)(hs.st_mode & 0xFFFF);
                int64_t  size = (int64_t)hs.st_size;
                int64_t  mtim = (int64_t)hs.st_mtime;
                uc_mem_write(G.uc, rsi + 24, &mode, 4);
                uc_mem_write(G.uc, rsi + 48, &size, 8);
                uc_mem_write(G.uc, rsi + 88, &mtim, 8);
                reg_write(UC_X86_REG_RAX, 0);
            }
        }
    } else if (n == 21) {                /* access(path, mode) */
        char path[4096];
        read_cstr(rdi, path, sizeof(path));
        reg_write(UC_X86_REG_RAX, access(path, (int)rsi) == 0 ? 0 : (uint64_t)-1);
    } else if (n == 79) {                /* getcwd(buf, size) */
        char host[4096];
        if (getcwd(host, sizeof(host)) && (strlen(host) + 1) <= (size_t)rsi) {
            uc_mem_write(G.uc, rdi, host, strlen(host) + 1);
            reg_write(UC_X86_REG_RAX, rdi);
        } else {
            reg_write(UC_X86_REG_RAX, 0);
        }
    } else if (n == 83) {                /* mkdir(path, mode) */
        char path[4096];
        read_cstr(rdi, path, sizeof(path));
        reg_write(UC_X86_REG_RAX, _mkdir(path) == 0 ? 0 : (uint64_t)-1);
    } else if (n == 84) {                /* rmdir(path) */
        char path[4096];
        read_cstr(rdi, path, sizeof(path));
        reg_write(UC_X86_REG_RAX, rmdir(path) == 0 ? 0 : (uint64_t)-1);
    } else if (n == 90) {                /* chmod(path, mode) */
        char path[4096];
        read_cstr(rdi, path, sizeof(path));
        reg_write(UC_X86_REG_RAX, chmod(path, (mode_t)rsi) == 0 ? 0 : (uint64_t)-1);
    } else if (n == 158) {               /* arch_prctl */
        /* goc sets the fs base to the .tls block (ARCH_SET_FS=0x1002) so the
         * main thread's _Thread_local variables are reachable through the fs
         * segment. Unicorn exposes the base as UC_X86_REG_FS_BASE / _GS_BASE. */
        uint64_t code = rdi;
        uint64_t addr = rsi;
        if (code == 0x1002) {            /* ARCH_SET_FS */
            reg_write(UC_X86_REG_FS_BASE, addr);
            reg_write(UC_X86_REG_RAX, 0);
        } else if (code == 0x1003) {     /* ARCH_GET_FS */
            uint64_t v = reg_read(UC_X86_REG_FS_BASE);
            uc_mem_write(G.uc, addr, &v, 8);
            reg_write(UC_X86_REG_RAX, 0);
        } else if (code == 0x1001) {     /* ARCH_SET_GS */
            reg_write(UC_X86_REG_GS_BASE, addr);
            reg_write(UC_X86_REG_RAX, 0);
        } else if (code == 0x1004) {     /* ARCH_GET_GS */
            uint64_t v = reg_read(UC_X86_REG_GS_BASE);
            uc_mem_write(G.uc, addr, &v, 8);
            reg_write(UC_X86_REG_RAX, 0);
        } else {
            reg_write(UC_X86_REG_RAX, (uint64_t)-1);
        }
    } else {                              /* unhandled syscall -> fatal */
        fprintf(stderr, "ucrun: unhandled syscall %llu (rip=0x%llx)\n",
                (unsigned long long)n,
                (unsigned long long)reg_read(UC_X86_REG_RIP));
        G.exit_code = 1;
        uc_emu_stop(G.uc);
    }
}

/* ---- hooks ---- */

static void hook_code(uc_engine *uc, uint64_t address, uint32_t size, void *ud) {
    (void)uc; (void)ud;
    if (size < 2) return;
    unsigned char code[2];
    uc_mem_read(G.uc, address, code, 2);
    if (code[0] == 0x0f && code[1] == 0x05)   /* syscall */
        do_syscall();
}

static bool hook_invalid(uc_engine *uc, uc_mem_type type, uint64_t address,
                         int size, int64_t value, void *ud) {
    (void)uc; (void)type; (void)value; (void)ud;
    uint64_t rip = reg_read(UC_X86_REG_RIP);
    fprintf(stderr,
            "ucrun: unmapped access at 0x%llx (size %d) rip=0x%llx\n"
            "       mapped: code/heap below 0x%llx, stack [0x%llx,0x%llx)\n",
            (unsigned long long)address, size, (unsigned long long)rip,
            (unsigned long long)G.heap_top,
            (unsigned long long)(STACK_TOP - STACK_SIZE),
            (unsigned long long)STACK_TOP);
    G.exit_code = 1;
    uc_emu_stop(G.uc);
    return false;   /* do not continue */
}

/* ---- ELF loading ---- */

typedef struct {
    uint8_t  *data;
    size_t    len;
    uint64_t  entry;
    /* PT_LOAD segments: offset, vaddr, filesz, memsz, flags */
    uint64_t  segs[64][5];
    int       nseg;
} Elf;

static int elf_load(const char *path, Elf *e) {
    FILE *f = fopen(path, "rb");
    if (!f) return -1;
    fseek(f, 0, SEEK_END);
    long sz = ftell(f);
    fseek(f, 0, SEEK_SET);
    if (sz < 0) { fclose(f); return -1; }
    e->data = malloc((size_t)sz);
    if (!e->data) { fclose(f); return -1; }
    if (fread(e->data, 1, (size_t)sz, f) != (size_t)sz) {
        free(e->data); fclose(f); return -1;
    }
    fclose(f);
    e->len = (size_t)sz;
    e->nseg = 0;
    if (sz < 0x40 || e->data[0] != 0x7f || e->data[1] != 'E' ||
        e->data[2] != 'L' || e->data[3] != 'F' || e->data[4] != 2 ||
        e->data[5] != 1) {
        free(e->data); e->data = NULL; return -1;
    }
    uint64_t phoff;    memcpy(&phoff,    e->data + 0x20, 8);
    uint16_t phentsz;  memcpy(&phentsz,  e->data + 0x36, 2);
    uint16_t phnum;    memcpy(&phnum,    e->data + 0x38, 2);
    memcpy(&e->entry,  e->data + 0x18, 8);
    for (uint16_t i = 0; i < phnum && e->nseg < 64; i++) {
        size_t off = (size_t)(phoff + (uint64_t)i * phentsz);
        uint32_t p_type, p_flags;
        uint64_t p_offset, p_vaddr, p_filesz, p_memsz;
        memcpy(&p_type,   e->data + off,        4);
        memcpy(&p_flags,  e->data + off + 4,    4);
        memcpy(&p_offset, e->data + off + 8,    8);
        memcpy(&p_vaddr,  e->data + off + 16,   8);
        memcpy(&p_filesz, e->data + off + 32,   8);
        memcpy(&p_memsz,  e->data + off + 40,   8);
        if (p_type == 1) { /* PT_LOAD */
            e->segs[e->nseg][0] = p_offset;
            e->segs[e->nseg][1] = p_vaddr;
            e->segs[e->nseg][2] = p_filesz;
            e->segs[e->nseg][3] = p_memsz;
            e->segs[e->nseg][4] = p_flags;
            e->nseg++;
        }
    }
    return 0;
}

static uint64_t brk_start(Elf *e) {
    uint64_t end = 0;
    for (int i = 0; i < e->nseg; i++)
        end = end > (e->segs[i][1] + e->segs[i][3]) ? end : (e->segs[i][1] + e->segs[i][3]);
    return align_up(end, PAGE);
}

/* ---- temporary working directory ---- */

/* Sweep stale per-run temp directories left by previous ucrun processes that
 * died hard before their crash handler could run (e.g. killed, power loss).
 * Anything named ucrun_* older than one hour in the same temp root is ours to
 * reap; live runs never keep a directory that long. */
static void sweep_stale(const char *tmppath) {
    char pat[MAX_PATH], path[MAX_PATH];
    WIN32_FIND_DATAA fd;
    FILETIME now_ft;
    GetSystemTimeAsFileTime(&now_ft);
    ULONGLONG now = ((ULONGLONG)now_ft.dwHighDateTime << 32) | now_ft.dwLowDateTime;
    snprintf(pat, sizeof(pat), "%sucrun_*", tmppath);
    HANDLE h = FindFirstFileA(pat, &fd);
    if (h == INVALID_HANDLE_VALUE) return;
    do {
        if (!(fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY)) continue;
        ULONGLONG wt = ((ULONGLONG)fd.ftLastWriteTime.dwHighDateTime << 32)
                     | fd.ftLastWriteTime.dwLowDateTime;
        if (now - wt < (ULONGLONG)3600 * 10000000ULL)   /* 1h in 100ns units */
            continue;
        snprintf(path, sizeof(path), "%s%s", tmppath, fd.cFileName);
        rmtree(path);
    } while (FindNextFileA(h, &fd));
    FindClose(h);
}

static void make_tmpdir(void) {
    char tmppath[MAX_PATH];
    DWORD n = GetTempPathA(sizeof(tmppath), tmppath);
    if (n == 0 || n >= sizeof(tmppath)) {
        strcpy(tmppath, ".\\");
    }
    sweep_stale(tmppath);
    snprintf(G.tmpdir, sizeof(G.tmpdir), "%sucrun_%u", tmppath, GetCurrentProcessId());
    /* ensure unique */
    for (int attempt = 0; attempt < 1000; attempt++) {
        if (CreateDirectoryA(G.tmpdir, NULL)) return;
        snprintf(G.tmpdir, sizeof(G.tmpdir), "%sucrun_%u_%d",
                 tmppath, GetCurrentProcessId(), attempt);
    }
}

static void rmtree(const char *dir) {
    char path[MAX_PATH];
    WIN32_FIND_DATAA fd;
    snprintf(path, sizeof(path), "%s\\*", dir);
    HANDLE h = FindFirstFileA(path, &fd);
    if (h == INVALID_HANDLE_VALUE) { RemoveDirectoryA(dir); return; }
    do {
        if (strcmp(fd.cFileName, ".") == 0 || strcmp(fd.cFileName, "..") == 0)
            continue;
        snprintf(path, sizeof(path), "%s\\%s", dir, fd.cFileName);
        if (fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY)
            rmtree(path);
        else
            DeleteFileA(path);
    } while (FindNextFileA(h, &fd));
    FindClose(h);
    RemoveDirectoryA(dir);
}

/* ---- main ---- */

int main(int argc, char **argv) {
    if (argc < 2) {
        fprintf(stderr, "usage: ucrun.exe <linux-elf> [args...]\n");
        return 2;
    }

    memset(&G, 0, sizeof(G));
    SetUnhandledExceptionFilter(crash_cleanup);
    for (int i = 0; i < 256; i++) G.fds[i] = -1;
    G.next_fd = 3;

    Elf elf;
    if (elf_load(argv[1], &elf) != 0) {
        fprintf(stderr, "ucrun: cannot load ELF %s\n", argv[1]);
        return 1;
    }

    uc_err rc = uc_open(UC_ARCH_X86, UC_MODE_64, &G.uc);
    if (rc != UC_ERR_OK) {
        fprintf(stderr, "ucrun: uc_open failed (%d)\n", rc);
        free(elf.data);
        return 1;
    }

    /* Map load segments (align-DOWN base, align-UP end). */
    for (int i = 0; i < elf.nseg; i++) {
        uint64_t off = elf.segs[i][0];
        uint64_t vaddr = elf.segs[i][1];
        uint64_t filesz = elf.segs[i][2];
        uint64_t memsz = elf.segs[i][3];
        uint32_t flags = (uint32_t)elf.segs[i][4];
        uint64_t base = vaddr & ~(PAGE - 1);
        uint64_t seg_end = align_up(vaddr + (memsz > filesz ? memsz : filesz), PAGE);
        if (seg_end <= base) continue;
        int prot = 0;
        if (flags & 4) prot |= UC_PROT_READ;
        if (flags & 2) prot |= UC_PROT_WRITE;
        if (flags & 1) prot |= UC_PROT_EXEC;
        rc = uc_mem_map(G.uc, base, seg_end - base, prot);
        if (rc != UC_ERR_OK) {
            fprintf(stderr, "ucrun: mem_map failed (%d) base=0x%llx\n",
                    rc, (unsigned long long)base);
            uc_close(G.uc); free(elf.data); return 1;
        }
        if (filesz) {
            uc_mem_write(G.uc, vaddr, elf.data + off, (size_t)filesz);
        }
    }

    /* Stack. */
    uint64_t stack_base = STACK_TOP - STACK_SIZE;
    uc_mem_map(G.uc, stack_base, STACK_SIZE, UC_PROT_READ | UC_PROT_WRITE);

    /* Build argv/envp block on the stack (SysV style). */
    int nargs = argc - 2;
    char **toks = malloc(sizeof(char *) * (nargs + 1));
    toks[0] = "ucrun";
    for (int i = 0; i < nargs; i++) toks[i + 1] = argv[i + 2];
    int ntok = nargs + 1;

    /* record string addresses, write strings high-to-low */
    uint64_t words[256];
    int nwords = 0;
    uint64_t rsp = STACK_TOP;
    for (int i = ntok - 1; i >= 0 && nwords < 256; i--) {
        size_t len = strlen(toks[i]);
        rsp -= (len + 1);
        uc_mem_write(G.uc, rsp, toks[i], len + 1);
        words[nwords++] = rsp;
    }
    /* reverse so words[0] is argv[0] */
    for (int i = 0; i < nwords / 2; i++) {
        uint64_t t = words[i]; words[i] = words[nwords - 1 - i]; words[nwords - 1 - i] = t;
    }
    rsp -= 8 * (uint64_t)(nwords + 2);
    rsp &= ~0xFULL;
    uint64_t p = rsp;
    uint64_t argc_val = (uint64_t)nwords;
    uc_mem_write(G.uc, p, &argc_val, 8); p += 8;
    for (int i = 0; i < nwords; i++) { uc_mem_write(G.uc, p, &words[i], 8); p += 8; }
    uint64_t zero = 0;
    uc_mem_write(G.uc, p, &zero, 8); p += 8;  /* NULL argv terminator */
    uc_mem_write(G.uc, p, &zero, 8);          /* empty envp */

    reg_write(UC_X86_REG_RSP, rsp);
    reg_write(UC_X86_REG_RIP, elf.entry);

    G.brk_cur = brk_start(&elf);
    G.heap_top = G.brk_cur;

    free(elf.data);
    free(toks);

    make_tmpdir();
    GetCurrentDirectoryA(sizeof(G.origdir), G.origdir);
    SetCurrentDirectoryA(G.tmpdir);

    uc_hook hh1, hh2;
    uc_hook_add(G.uc, &hh1, UC_HOOK_CODE, (void *)hook_code, &G, 0, (uint64_t)-1);
    uc_hook_add(G.uc, &hh2, UC_HOOK_MEM_INVALID, (void *)hook_invalid, &G, 0, (uint64_t)-1);

    rc = uc_emu_start(G.uc, elf.entry, 0, 0, 0);
    if (rc != UC_ERR_OK)
        G.exit_code = 1;   /* genuine failure (not a clean stop via uc_emu_stop) */

    uc_close(G.uc);
    cleanup_tmpdir();
    return G.exit_code & 0xFF;
}
