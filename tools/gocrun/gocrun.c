/*
 * gocrun.c -- run a goc-produced Linux x86-64 static ELF natively on Windows.
 *
 * Like ucrun it loads the ELF and translates the Linux syscalls the goclib
 * runtime issues, but it does NOT emulate the CPU. goc (goa) routes every
 * syscall through a single __goc_syscall symbol (emitted as `syscall; ret` in
 * the ELF). gocrun maps the ELF's segments into its own address space at their
 * link address (0x400000) and rewrites __goc_syscall's body from `syscall`
 * into `ud2` (0F 0B). A vectored exception handler (registered first, so it
 * runs before the CRT's own translation) catches the #UD, reads the SysV
 * syscall registers out of the CONTEXT (rax/rdi/rsi/rdx/r10/r8/r9), calls
 * translate(), a plain C function that performs each syscall against the
 * Win32/CRT API, then writes the return value back into CONTEXT.Rax and
 * advances RIP past the ud2 before resuming.
 *
 * Because the guest runs as native code in this process, guest pointers are
 * host pointers: translate() reads and writes guest memory by direct cast, with
 * no copy through an emulator buffer. This removes the 25 MB Unicorn dependency
 * and runs the Linux legs at full native speed.
 *
 * Why UD2+VEH instead of a `call translate` trampoline? goc's codegen relies on
 * the Linux fact that a syscall preserves every guest register except rax/rcx/
 * r11. A normal call into a C function clobbers rdx/r8/r9/r10/r11, and
 * manually spilling/restoring six registers around the call is fragile (shadow
 * space overlap, ABI alignment, arg mapping -- each caused a gocrun crash in
 * earlier iterations). A VEH dispatch, by contrast, has the OS save the full
 * register file at the fault; the handler only touches Rax and Rip, so every
 * other guest register is preserved by construction.
 *
 * Usage:
 *     gocrun.exe <linux-elf> [args...]
 * Exit code: the guest program's exit code (masked to 0xFF).
 */

#include <windows.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdbool.h>
#include <io.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <direct.h>
#include <processthreadsapi.h>

#define PAGE        0x1000ULL   /* guest page size (and commit granularity) */
#define ALLOC_GRAN  0x10000ULL  /* VirtualAlloc MEM_RESERVE granularity (64K) */
#define STACK_SIZE  0x200000ULL   /* 2 MB guest stack */
#define MAX_FD      256

/* ---- global loader / runtime state (single-threaded guest) ---- */

static int      g_fds[MAX_FD];          /* guest_fd -> host CRT fd; -1 = free */
static int      g_next_fd;
static uint64_t g_brk_cur;              /* current program break (bytes) */
static uint64_t g_heap_top;             /* highest mapped heap address */
static char     g_tmpdir[MAX_PATH];
static char     g_origdir[MAX_PATH];

/* directory-enumeration fds (Windows has no directory fd) */
static HANDLE           g_dirh[MAX_FD];
static WIN32_FIND_DATAA g_dirfind[MAX_FD];
static int              g_dirfirst[MAX_FD];  /* 1 = g_dirfind holds FindFirst result */
static int              g_dirstate[MAX_FD];  /* 0=".", 1="..", 2+=real entries */
static uint64_t         g_diroff[MAX_FD];

static uint64_t align_up(uint64_t x, uint64_t a) { return (x + a - 1) & ~(a - 1); }

/* Address of the ud2 we wrote into __goc_syscall. The VEH handler only
 * dispatches a #UD if RIP sits exactly here (and the bytes still say 0F 0B),
 * so we never hijack unrelated ud2 instructions. */
static uint64_t g_patched_ud2;

/* Syscall backend (defined below, after the directory/fd helpers). */
uint64_t translate(uint64_t num, uint64_t a1, uint64_t a2,
                   uint64_t a3, uint64_t a4, uint64_t a5, uint64_t a6);

/* Defined in trampoline.s: switch back to the main stack and clean-exit. */
extern void goc_exit_stub(void);

/* Exit handling: the exit syscall cannot call ExitProcess directly, because the
 * guest's return frames (in the raw ELF, which has no PE unwind tables) would
 * still be on the stack and ntdll's process-teardown stack unwind would fault.
 * So the trampoline, on seeing g_exit_requested, switches rsp back to the saved
 * main stack (no RtlUnwind, which would also trip over the guest frames) and
 * calls goc_exit_now(), which exits from a clean PE-aware stack. */
int      g_exit_requested;
int      g_exit_code;
uint64_t g_main_rsp;

/* Read a NUL-terminated C string from guest memory (guest ptr == host ptr). */
static void read_cstr(uint64_t addr, char *buf, int maxlen) {
    const char *p = (const char *)(uintptr_t)addr;
    int i = 0;
    if (!p) { buf[0] = 0; return; }
    while (i < maxlen - 1) {
        char c = p[i];
        if (!c) break;
        buf[i++] = c;
    }
    buf[i] = 0;
}

static int fd_alloc(int hostfd) {
    int g = g_next_fd;
    if (g >= MAX_FD) return -1;
    g_fds[g] = hostfd;
    g_next_fd++;
    return g;
}
static int fd_lookup(int g) {
    if (g < 0 || g >= MAX_FD) return -1;
    return g_fds[g];
}

/* brk: addr==0 queries, addr>break grows, returns the new break (old on fail).
 * Note: MEM_COMMIT alone on an unreserved range fails with ERROR_INVALID_ADDRESS
 * (487) -- Windows requires MEM_RESERVE on the first touch, exactly like the
 * PT_LOAD / unwind / stub allocations. Without it sbrk's grow fails, every
 * heap_alloc returns NULL, and fopen silently returns 0. */
static uint64_t grow_heap(uint64_t addr) {
    if (addr == 0) return g_brk_cur;
    if (addr <= g_brk_cur) return g_brk_cur;
    uint64_t end = align_up(addr, PAGE);
    if (end > g_heap_top) {
        void *p = VirtualAlloc((void *)g_heap_top, end - g_heap_top,
                               MEM_COMMIT | MEM_RESERVE, PAGE_READWRITE);
        if (!p) {
            MEMORY_BASIC_INFORMATION mbi;
            SIZE_T mb = VirtualQuery((const void *)g_heap_top, &mbi, sizeof(mbi));
            fprintf(stderr, "[gocrun] grow_heap VirtualAlloc(%llx,%llx) failed err=%lu "
                    "vq=%lu base=%p size=%llx state=%lx type=%lx\n",
                    (unsigned long long)g_heap_top,
                    (unsigned long long)(end - g_heap_top), GetLastError(),
                    (unsigned long)mb, mbi.BaseAddress,
                    (unsigned long long)mbi.RegionSize,
                    (unsigned long)mbi.State, (unsigned long)mbi.Type);
            fflush(stderr);
            return g_brk_cur;        /* grow failed: return old break */
        }
        g_heap_top = end;
    }
    g_brk_cur = addr;
    return g_brk_cur;
}

/* ---- temporary working directory (mirrors ucrun) ---- */

static void rmtree(const char *dir);

static void close_guest_fds(void) {
    for (int i = 0; i < MAX_FD; i++) {
        if (g_fds[i] >= 0) { _close(g_fds[i]); g_fds[i] = -1; }
        if (g_dirh[i] != NULL && g_dirh[i] != INVALID_HANDLE_VALUE) {
            FindClose(g_dirh[i]); g_dirh[i] = INVALID_HANDLE_VALUE;
        }
    }
}

static void leave_tmpdir(void) {
    SetCurrentDirectoryA(g_origdir[0] ? g_origdir : "C:\\");
}

static void cleanup_tmpdir(void) {
    fprintf(stderr, "[gocrun] cleanup: g_tmpdir=%s\n", g_tmpdir[0] ? g_tmpdir : "(empty)");
    fflush(stderr);
    if (getenv("GOCRUN_KEEP_TMPDIR")) {
        fprintf(stderr, "[gocrun] GOCRUN_KEEP_TMPDIR set, leaving %s\n", g_tmpdir);
        fflush(stderr);
        leave_tmpdir();
        return;
    }
    if (g_tmpdir[0]) {
        fprintf(stderr, "[gocrun] cleanup: leave_tmpdir\n"); fflush(stderr);
        leave_tmpdir();
        fprintf(stderr, "[gocrun] cleanup: close_guest_fds\n"); fflush(stderr);
        close_guest_fds();
        fprintf(stderr, "[gocrun] cleanup: rmtree\n"); fflush(stderr);
        rmtree(g_tmpdir);
        g_tmpdir[0] = '\0';
    }
}

static LONG WINAPI crash_cleanup(EXCEPTION_POINTERS *ep) {
    EXCEPTION_RECORD *er = ep->ExceptionRecord;
    fprintf(stderr, "[gocrun] *** EXCEPTION 0x%lx at %p (fault=%p) rsp=%p\n",
            (unsigned long)er->ExceptionCode,
            (void *)er->ExceptionAddress,
            (void *)er->ExceptionInformation[1],
            (void *)ep->ContextRecord->Rsp);
    fflush(stderr);
    cleanup_tmpdir();
    return EXCEPTION_CONTINUE_SEARCH;
}

/* Identify the module containing addr, as "name+0xoff". Returns NULL if not
 * in any loaded module (e.g. the raw ELF guest image or an unmapped page). */
static const char *mod_of(uint64_t addr, char *buf, size_t n) {
    HMODULE h = NULL;
    if (GetModuleHandleExA(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS |
                           GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
                           (LPCSTR)(uintptr_t)addr, &h) && h) {
        DWORD r = GetModuleFileNameA(h, buf, (DWORD)n);
        if (r && r < n) {
            const char *s = strrchr(buf, '\\');
            if (s) {
                uintptr_t base = (uintptr_t)h;
                snprintf(buf, n, "%s+0x%llx", s + 1,
                         (unsigned long long)(addr - base));
                return buf;
            }
        }
    }
    return NULL;
}

/* Is the qword at a readable committed page? Safe probe, no SEH needed. */
static int page_readable(uint64_t a) {
    MEMORY_BASIC_INFORMATION mbi;
    if (!a || a >= (1ULL << 48)) return 0;
    if (!VirtualQuery((LPCVOID)(uintptr_t)a, &mbi, sizeof(mbi))) return 0;
    if (mbi.State != MEM_COMMIT) return 0;
    DWORD p = mbi.Protect;
    if (p == PAGE_NOACCESS || p == PAGE_EXECUTE) return 0;
    return 1;
}

/* Vectored handler: runs BEFORE the CRT's own exception/signal translation,
 * so we can capture the real crash site even on a foreign/guest stack. */
static LONG CALLBACK veh_handler(EXCEPTION_POINTERS *ep) {
    EXCEPTION_RECORD *er = ep->ExceptionRecord;
    CONTEXT *ctx = ep->ContextRecord;

    /* ---- syscall dispatch (UD2 trap) ----
     * goc routes every Linux syscall through __goc_syscall, whose body we
     * rewrote to `ud2` (0F 0B). On a #UD (0xC000001D) with RIP sitting exactly
     * on that ud2, translate the syscall against the Win32/CRT API. The OS
     * saved the whole register file in ctx, so we only touch Rax (result) and
     * Rip (skip the ud2). Every other guest register is preserved by the
     * exception machinery -- this is what makes UD2+VEH robust where a
     * register-marshaling trampoline was not. */
    if (er->ExceptionCode == 0xC000001D && ctx->Rip == g_patched_ud2) {
        const uint8_t *b = (const uint8_t *)(uintptr_t)ctx->Rip;
        if (b[0] == 0x0F && b[1] == 0x0B) {
            uint64_t num = ctx->Rax;
            uint64_t ret = translate(num, ctx->Rdi, ctx->Rsi, ctx->Rdx,
                                     ctx->R10, ctx->R8, ctx->R9);
            if (g_exit_requested) {
                /* exit/exit_group: cannot ExitProcess from inside the VEH
                 * callback (its frame would be left dangling). Land RIP on the
                 * exit stub instead; it switches back to the main stack and
                 * calls goc_exit_now(), which exits cleanly. Rax carries the
                 * exit code (translate stored it in g_exit_code). */
                ctx->Rip = (uint64_t)(uintptr_t)goc_exit_stub;
                ctx->Rax = (uint64_t)(uint32_t)g_exit_code;
                return EXCEPTION_CONTINUE_EXECUTION;
            }
            ctx->Rax = ret;
            ctx->Rip += 2;              /* skip the ud2 (fault, not trap) */
            return EXCEPTION_CONTINUE_EXECUTION;
        }
    }

    ULONG_PTR slo, shi;
    GetCurrentThreadStackLimits(&slo, &shi);
    char path[MAX_PATH];
    DWORD tn = GetTempPathA(sizeof(path), path);
    if (tn == 0 || tn >= sizeof(path)) strcpy(path, ".\\");
    strcat(path, "gocrun_crash.txt");
    char buf[4096];
    char m1[128], m2[128], m3[128];
    const char *cm1 = mod_of((uint64_t)(uintptr_t)er->ExceptionAddress, m1, sizeof(m1));
    const char *cm2 = mod_of((uint64_t)(uintptr_t)ctx->Rip, m2, sizeof(m2));
    const char *cm3 = mod_of(ctx->R9, m3, sizeof(m3));
    int n = snprintf(buf, sizeof(buf),
        "CRASH code=0x%lx addr=%p(%s) fault=%p\n"
        "rip=%p(%s) rsp=%p rbp=%p rax=%p rbx=%p rcx=%p rdx=%p rsi=%p rdi=%p\n"
        "r8=%p r9=%p(%s) r10=%p r11=%p r12=%p r13=%p r14=%p r15=%p\n"
        "thread_stack=[%p,%p]\n"
        "stack @rsp:\n",
        (unsigned long)er->ExceptionCode,
        (void *)er->ExceptionAddress, cm1 ? cm1 : "?",
        (void *)er->ExceptionInformation[1],
        (void *)ctx->Rip, cm2 ? cm2 : "?",
        (void *)ctx->Rsp, (void *)ctx->Rbp,
        (void *)ctx->Rax, (void *)ctx->Rbx, (void *)ctx->Rcx,
        (void *)ctx->Rdx, (void *)ctx->Rsi, (void *)ctx->Rdi,
        (void *)ctx->R8, (void *)ctx->R9, cm3 ? cm3 : "?",
        (void *)ctx->R10, (void *)ctx->R11, (void *)ctx->R12, (void *)ctx->R13,
        (void *)ctx->R14, (void *)ctx->R15,
        (void *)slo, (void *)shi);
    /* Dump 48 qwords from the fault RSP so we can see return addresses. */
    const uint64_t *sp = (const uint64_t *)(uintptr_t)ctx->Rsp;
    for (int i = 0; i < 48; i++) {
        uint64_t v = sp[i];
        n += snprintf(buf + n, sizeof(buf) - n, "  +%2d %016llx\n", i, (unsigned long long)v);
    }
    /* Second pass: annotate values that fall inside a loaded module. */
    n += snprintf(buf + n, sizeof(buf) - n, "module annotation:\n");
    for (int i = 0; i < 48; i++) {
        uint64_t v = sp[i];
        char m[128];
        const char *cm = mod_of(v, m, sizeof(m));
        if (cm) n += snprintf(buf + n, sizeof(buf) - n, "  +%2d %016llx -> %s\n",
                              i, (unsigned long long)v, cm);
    }
    /* Guest-frame diagnostics: follow the rbp chain and dump common arg slots.
     * goc's codegen (SysV-ish, but it uses rbp frames like the disassembly of
     * fwrite shows: args saved at [rbp-0x30/-0x38/-0x40/-0x48]). */
    n += snprintf(buf + n, sizeof(buf) - n, "rbp chain:\n");
    uint64_t rb = ctx->Rbp;
    for (int depth = 0; depth < 4 && page_readable(rb); depth++) {
        uint64_t *fp = (uint64_t *)(uintptr_t)rb;
        uint64_t saved_rbp = fp[0], retaddr = fp[1];
        char m[128];
        const char *cm = mod_of(retaddr, m, sizeof(m));
        n += snprintf(buf + n, sizeof(buf) - n,
                      "  depth=%d rbp=%016llx ret=%016llx%s%s\n",
                      depth, (unsigned long long)rb,
                      (unsigned long long)retaddr, cm ? " -> " : "",
                      cm ? m : "");
        /* dump guest arg slots if the frame is in the guest image (0x400000..) */
        if (rb >= 0x400000 && rb < 0x500000) {
            for (int slot = 0; slot < 4; slot++) {
                uint64_t a = rb - 0x30 - slot * 8;
                uint64_t v = 0;
                if (page_readable(a)) v = *(uint64_t *)(uintptr_t)a;
                n += snprintf(buf + n, sizeof(buf) - n,
                              "    [rbp-%02x] = %016llx\n", 0x30 + slot * 8,
                              (unsigned long long)v);
            }
        }
        if (saved_rbp == rb || !page_readable(saved_rbp)) break;
        rb = saved_rbp;
    }
    DWORD wr;
    HANDLE f = CreateFileA(path, GENERIC_WRITE, 0, NULL,
                           CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
    if (f != INVALID_HANDLE_VALUE) {
        WriteFile(f, buf, (DWORD)n, &wr, NULL);
        CloseHandle(f);
    }
    return EXCEPTION_CONTINUE_SEARCH;
}

static void sweep_stale(const char *tmppath) {
    char pat[MAX_PATH], path[MAX_PATH];
    WIN32_FIND_DATAA fd;
    FILETIME now_ft;
    GetSystemTimeAsFileTime(&now_ft);
    ULONGLONG now = ((ULONGLONG)now_ft.dwHighDateTime << 32) | now_ft.dwLowDateTime;
    snprintf(pat, sizeof(pat), "%sgocrun_*", tmppath);
    HANDLE h = FindFirstFileA(pat, &fd);
    if (h == INVALID_HANDLE_VALUE) return;
    do {
        if (!(fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY)) continue;
        ULONGLONG wt = ((ULONGLONG)fd.ftLastWriteTime.dwHighDateTime << 32)
                     | fd.ftLastWriteTime.dwLowDateTime;
        if (now - wt < (ULONGLONG)3600 * 10000000ULL) continue;
        snprintf(path, sizeof(path), "%s%s", tmppath, fd.cFileName);
        rmtree(path);
    } while (FindNextFileA(h, &fd));
    FindClose(h);
}

static void make_tmpdir(void) {
    char tmppath[MAX_PATH];
    DWORD n = GetTempPathA(sizeof(tmppath), tmppath);
    if (n == 0 || n >= sizeof(tmppath)) strcpy(tmppath, ".\\");
    sweep_stale(tmppath);
    snprintf(g_tmpdir, sizeof(g_tmpdir), "%sgocrun_%u", tmppath, GetCurrentProcessId());
    for (int attempt = 0; attempt < 1000; attempt++) {
        if (CreateDirectoryA(g_tmpdir, NULL)) return;
        snprintf(g_tmpdir, sizeof(g_tmpdir), "%sgocrun_%u_%d",
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

/* ---- getdents64 (Linux directory enumeration) ---- */

#define DT_DIR 4
#define DT_REG 8

static uint64_t getdents64(int gfd, void *buf, uint64_t count) {
    HANDLE h = g_dirh[gfd];
    if (h == NULL || h == INVALID_HANDLE_VALUE) return 0;
    uint8_t *out = (uint8_t *)buf;
    uint64_t total = 0;
    for (;;) {
        const char *name; int type; int synth = 0;
        if (g_dirstate[gfd] == 0)      { name = ".";  type = DT_DIR; synth = 1; }
        else if (g_dirstate[gfd] == 1) { name = ".."; type = DT_DIR; synth = 1; }
        else {
            if (g_dirfirst[gfd]) { g_dirfirst[gfd] = 0; }   /* use FindFirst result */
            else if (!FindNextFileA(h, &g_dirfind[gfd])) break;
            name = g_dirfind[gfd].cFileName;
            type = (g_dirfind[gfd].dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY)
                   ? DT_DIR : DT_REG;
        }
        int nlen = (int)strlen(name);
        int reclen = (19 + nlen + 1 + 7) & ~7;   /* d_name starts at 19, 8-aligned */
        if (total + (uint64_t)reclen > count) break;  /* no room: keep state */
        uint64_t ino = 0;
        uint64_t off = g_diroff[gfd] + (uint64_t)reclen;
        memcpy(out + total + 0,  &ino, 8);
        memcpy(out + total + 8,  &off, 8);
        uint16_t rl = (uint16_t)reclen;
        memcpy(out + total + 16, &rl, 2);
        out[total + 18] = (uint8_t)type;
        memcpy(out + total + 19, name, (size_t)nlen + 1);
        g_diroff[gfd] = off;
        total += (uint64_t)reclen;
        if (synth) g_dirstate[gfd]++;
    }
    return total;
}

/* ---- syscall translation (called from veh_handler on a UD2 trap) ----
 * num = rax; a1..a6 = rdi, rsi, rdx, r10, r8, r9 (in that order). */

uint64_t translate(uint64_t num, uint64_t a1, uint64_t a2,
                   uint64_t a3, uint64_t a4, uint64_t a5, uint64_t a6) {
    switch (num) {
    case 60:            /* exit */
    case 231: {         /* exit_group */
        g_exit_code = (int)(a1 & 0xFF);
        g_exit_requested = 1;     /* veh_handler lands RIP on the exit stub */
        return 0;
    }
    case 1: {           /* write */
        uint64_t fd = a1, buf = a2;
        int64_t cnt = (int64_t)a3;
        if (cnt < 0) cnt = 0;
        int h = fd_lookup((int)fd);
        fprintf(stderr, "[gocrun] write fd=%llu gfd->hfd=%d cnt=%lld buf=%llx\n",
                (unsigned long long)fd, h, (long long)cnt, (unsigned long long)buf);
        fflush(stderr);
        if (h < 0) return (uint64_t)-1;
        int w = _write(h, (const void *)(uintptr_t)buf, (unsigned)cnt);
        fprintf(stderr, "[gocrun] write -> %d\n", w);
        fflush(stderr);
        return w < 0 ? (uint64_t)-1 : (uint64_t)w;
    }
    case 0: {           /* read */
        if (a1 == 0) return 0;   /* stdin: not wired up -> EOF (matches ucrun) */
        uint64_t buf = a2;
        int64_t cnt = (int64_t)a3;
        if (cnt < 0) cnt = 0;
        int h = fd_lookup((int)a1);
        if (h < 0) return (uint64_t)-1;
        int r = _read(h, (void *)(uintptr_t)buf, (unsigned)cnt);
        return r < 0 ? 0 : (uint64_t)r;
    }
    case 2: {           /* open */
        char path[4096];
        read_cstr(a1, path, sizeof(path));
        fprintf(stderr, "[gocrun] open: path='%s'\n", path); fflush(stderr);
        struct _stat st;
        fprintf(stderr, "[gocrun] open: before _stat\n"); fflush(stderr);
        int sr = _stat(path, &st);
        fprintf(stderr, "[gocrun] open: after _stat sr=%d\n", sr); fflush(stderr);
        if (sr == 0 && (st.st_mode & _S_IFDIR)) {
            char pat[MAX_PATH];
            snprintf(pat, sizeof(pat), "%s\\*", path);
            WIN32_FIND_DATAA wfd;
            HANDLE h = FindFirstFileA(pat, &wfd);
            if (h == INVALID_HANDLE_VALUE) return (uint64_t)-1;
            int g = fd_alloc(-1);
            if (g < 0) { FindClose(h); return (uint64_t)-1; }
            g_dirh[g] = h; g_dirfirst[g] = 1; g_dirstate[g] = 0; g_diroff[g] = 0;
            memcpy(&g_dirfind[g], &wfd, sizeof(wfd));
            g_fds[g] = -1;
            return (uint64_t)g;
        }
        int flags = (int)a2, mode = (int)a3;
        fprintf(stderr, "[gocrun] open: flags=%x mode=%o\n", flags, mode); fflush(stderr);
        int oflags = (flags & 3) == 0 ? _O_RDONLY
                   : ((flags & 3) == 1 ? _O_WRONLY : _O_RDWR);
        if (flags & 0x40)  oflags |= _O_CREAT;
        if (flags & 0x200) oflags |= _O_TRUNC;
        if (flags & 0x400) oflags |= _O_APPEND;
        int h = _open(path, oflags, mode);
        fprintf(stderr, "[gocrun] open: after _open h=%d\n", h); fflush(stderr);
        if (h < 0) return (uint64_t)-1;
        int g = fd_alloc(h);
        return g < 0 ? (uint64_t)-1 : (uint64_t)g;
    }
    case 3: {           /* close */
        int g = (int)a1;
        fprintf(stderr, "[gocrun] close fd=%d\n", g); fflush(stderr);
        if (g >= 0 && g < MAX_FD && g_dirh[g] != NULL
            && g_dirh[g] != INVALID_HANDLE_VALUE) {
            FindClose(g_dirh[g]);
            g_dirh[g] = INVALID_HANDLE_VALUE;
            g_fds[g] = -1;
            return 0;
        }
        int h = fd_lookup(g);
        if (h < 0) return (uint64_t)-1;
        _close(h);
        g_fds[g] = -1;
        return 0;
    }
    case 8: {           /* lseek */
        int h = fd_lookup((int)a1);
        if (h < 0) return (uint64_t)-1;
        int64_t r = _lseeki64(h, (int64_t)a2, (int)a3);
        return r < 0 ? (uint64_t)-1 : (uint64_t)r;
    }
    case 87: {          /* unlink */
        char path[4096];
        read_cstr(a1, path, sizeof(path));
        return _unlink(path) == 0 ? 0 : (uint64_t)-1;
    }
    case 82: {          /* rename */
        char oldp[4096], newp[4096];
        read_cstr(a1, oldp, sizeof(oldp));
        read_cstr(a2, newp, sizeof(newp));
        return rename(oldp, newp) == 0 ? 0 : (uint64_t)-1;
    }
    case 12: {          /* brk */
        uint64_t r = grow_heap(a1);
        fprintf(stderr, "[gocrun] brk(0x%llx) -> 0x%llx (cur=0x%llx top=0x%llx)\n",
                (unsigned long long)a1, (unsigned long long)r,
                (unsigned long long)g_brk_cur, (unsigned long long)g_heap_top);
        fflush(stderr);
        return r;
    }
    case 4:             /* stat */
    case 5: {           /* fstat */
        struct _stat hs;
        int ok;
        if (num == 4) {
            char path[4096];
            read_cstr(a1, path, sizeof(path));
            ok = _stat(path, &hs);
        } else {
            int h = fd_lookup((int)a1);
            if (h < 0) return (uint64_t)-1;
            ok = _fstat(h, &hs);
        }
        if (ok != 0) return (uint64_t)-1;
        uint8_t z[144];
        memset(z, 0, sizeof(z));
        memcpy((void *)(uintptr_t)a2, z, 144);
        uint32_t mode = (uint32_t)(hs.st_mode & 0xFFFF);
        int64_t  size = (int64_t)hs.st_size;
        int64_t  mtim = (int64_t)hs.st_mtime;
        memcpy((void *)(uintptr_t)(a2 + 24), &mode, 4);
        memcpy((void *)(uintptr_t)(a2 + 48), &size, 8);
        memcpy((void *)(uintptr_t)(a2 + 88), &mtim, 8);
        return 0;
    }
    case 21: {          /* access */
        char path[4096];
        read_cstr(a1, path, sizeof(path));
        return _access(path, (int)a2) == 0 ? 0 : (uint64_t)-1;
    }
    case 79: {          /* getcwd */
        char host[4096];
        if (_getcwd(host, sizeof(host)) && (strlen(host) + 1) <= (size_t)a2) {
            memcpy((void *)(uintptr_t)a1, host, strlen(host) + 1);
            return a1;
        }
        return 0;
    }
    case 83: {          /* mkdir */
        char path[4096];
        read_cstr(a1, path, sizeof(path));
        return _mkdir(path) == 0 ? 0 : (uint64_t)-1;
    }
    case 84: {          /* rmdir */
        char path[4096];
        read_cstr(a1, path, sizeof(path));
        return _rmdir(path) == 0 ? 0 : (uint64_t)-1;
    }
    case 90: {          /* chmod */
        char path[4096];
        read_cstr(a1, path, sizeof(path));
        return _chmod(path, (int)a2) == 0 ? 0 : (uint64_t)-1;
    }
    case 96:            /* gettimeofday */
    case 228: {         /* clock_gettime */
        uint64_t clk = (num == 228) ? a1 : 0;
        uint64_t tp  = a2;
        uint64_t sec = 0, frac = 0;
        if (num == 96 || clk == 0) {            /* wall clock */
            FILETIME ft;
            GetSystemTimeAsFileTime(&ft);
            uint64_t t = ((uint64_t)ft.dwHighDateTime << 32) | ft.dwLowDateTime;
            t -= 116444736000000000ULL;          /* 1601-01-01 -> 1970-01-01 */
            sec  = t / 10000000ULL;
            frac = (num == 96) ? (t / 10ULL) % 1000000ULL
                               : (t % 10000000ULL) * 100ULL;
        } else if (clk == 1) {                   /* CLOCK_MONOTONIC */
            uint64_t ms = GetTickCount64();
            sec  = ms / 1000ULL;
            frac = (num == 96) ? (ms % 1000ULL) * 1000ULL
                               : (ms % 1000ULL) * 1000000ULL;
        } else {                                 /* CLOCK_PROCESS_CPUTIME */
            FILETIME c, e, k, u;
            if (GetProcessTimes(GetCurrentProcess(), &c, &e, &k, &u)) {
                uint64_t t = ((uint64_t)u.dwHighDateTime << 32) | u.dwLowDateTime;
                t += ((uint64_t)k.dwHighDateTime << 32) | k.dwLowDateTime;
                sec  = t / 10000000ULL;
                frac = (num == 96) ? (t / 10ULL) % 1000000ULL
                                   : (t % 10000000ULL) * 100ULL;
            }
        }
        memcpy((void *)(uintptr_t)tp, &sec, 8);
        memcpy((void *)(uintptr_t)(tp + 8), &frac, 8);
        return 0;
    }
    case 217:           /* getdents64 */
        return getdents64((int)a1, (void *)(uintptr_t)a2, a3);
    case 58:            /* vfork  (system(): not exercised by the regression) */
    case 59:            /* execve */
    case 61:            /* wait4 */
        return (uint64_t)-1;
    default:
        fprintf(stderr, "gocrun: unhandled syscall %llu\n",
                (unsigned long long)num);
        cleanup_tmpdir();
        ExitProcess(1);
    }
    return 0;
}

/* ---- ELF loading ---- */



static int rd_u16(const uint8_t *p) { return (int)p[0] | ((int)p[1] << 8); }
static int rd_u32(const uint8_t *p) {
    return (int)p[0] | ((int)p[1] << 8) | ((int)p[2] << 16) | ((int)p[3] << 24);
}
static uint64_t rd_u64(const uint8_t *p) {
    uint64_t v = 0;
    for (int i = 0; i < 8; i++) v |= (uint64_t)p[i] << (8 * i);
    return v;
}

/* RtlAddFunctionTable (declared in <winnt.h>) lets us publish x64 unwind info
 * for the guest image so the OS unwinder can walk guest frames during a Win32
 * syscall's internal exception dispatch. Without it, 0x400000 frames have no
 * unwind table and the unwinder segfaults (the original gocrun crash).
 * The x64 RUNTIME_FUNCTION layout is identical to our local copy below. */
typedef struct _GocRF {
    ULONG BeginAddress;
    ULONG EndAddress;
    ULONG UnwindData;
} GocRF;

/* Parse the .gocuw section (the only non-alloc PROGBITS section goa emits) and
 * build + register an x64 unwind table. Returns the first free address past the
 * reserved table so the sbrk heap can start above it. Failure is non-fatal: the
 * program still runs, just without unwind info (original behavior). */
static uint64_t register_guest_unwind(const uint8_t *data, uint64_t shoff,
                                      uint16_t shentsz, uint16_t shnum,
                                      uint64_t image_end) {
    uint64_t goc_off = 0, goc_size = 0;
    for (uint16_t i = 0; i < shnum; i++) {
        size_t so = (size_t)(shoff + (uint64_t)i * shentsz);
        uint32_t sh_type  = (uint32_t)rd_u32(data + so + 4);
        uint64_t sh_flags = rd_u64(data + so + 8);
        if (sh_type == 1 && (sh_flags & 0x2) == 0) {  /* PROGBITS, !SHF_ALLOC */
            goc_off  = rd_u64(data + so + 24);
            goc_size = rd_u64(data + so + 32);
            break;
        }
    }
    if (goc_off == 0 || goc_size == 0) {
        fprintf(stderr, "[gocrun] unwind: no .gocuw (off=%llx size=%llx)\n",
                (unsigned long long)goc_off, (unsigned long long)goc_size);
        fflush(stderr);
        return image_end;
    }

    const uint8_t *p = data + goc_off;
    uint64_t remain = goc_size;

    /* pass 1: count records (format: u32 start, u32 end, u8 nreg, nreg*u8 regs,
     * u32 alloc) */
    uint32_t n = 0;
    {
        const uint8_t *q = p; uint64_t r = remain;
        while (r >= 13) {
            uint32_t nreg = q[8];
            uint64_t need = 9 + nreg + 4;
            if (r < need) break;
            n++; q += need; r -= need;
        }
    }
    fprintf(stderr, "[gocrun] unwind: .gocuw off=%llx size=%llx records=%u\n",
            (unsigned long long)goc_off, (unsigned long long)goc_size, n);
    fflush(stderr);
    if (n == 0) return image_end;

    /* pass 2: total bytes for RF array + aligned UNWIND_INFO blocks */
    uint64_t need_bytes = (uint64_t)n * sizeof(GocRF);
    {
        const uint8_t *q = p; uint64_t r = remain;
        for (uint32_t k = 0; k < n; k++) {
            uint32_t nreg  = q[8];
            uint32_t alloc = rd_u32(q + 9 + nreg);
            uint32_t cnt   = nreg + (alloc > 0 ? 3 : 0);  /* pushes + alloc */
            uint32_t ui    = 4 + 2 * cnt;
            if (ui & 3) ui += 4 - (ui & 3);               /* 4-byte align */
            need_bytes += ui;
            q += 9 + nreg + 4; r -= 9 + nreg + 4;
        }
    }

    /* Allocate adjacent to the image so RVA = (addr - 0x400000) fits in 32 bits.
     * Try a few fixed low addresses (all within 2^32 of 0x400000) as fallbacks. */
    fprintf(stderr, "[gocrun] unwind: need_bytes=%llu image_end=%llx\n",
            (unsigned long long)need_bytes, (unsigned long long)image_end);
    fflush(stderr);
    uintptr_t blk = 0;
    uint64_t unwind_cands[] = { image_end, 0x41000000, 0x42000000, 0x43000000, 0x20000000 };
    for (size_t ci = 0; ci < sizeof(unwind_cands)/sizeof(unwind_cands[0]) && !blk; ci++) {
        blk = (uintptr_t)VirtualAlloc((void *)unwind_cands[ci], (SIZE_T)need_bytes,
                                      MEM_COMMIT | MEM_RESERVE, PAGE_EXECUTE_READWRITE);
        if (blk)
            fprintf(stderr, "[gocrun] unwind: alloc at %p (cand %llx) err=%lu\n",
                    (void *)blk, (unsigned long long)unwind_cands[ci], GetLastError());
    }
    if (!blk) {
        fprintf(stderr, "[gocrun] unwind: VirtualAlloc failed err=%lu\n", GetLastError());
        fflush(stderr);
        return image_end;
    }
    memset((void *)blk, 0, (SIZE_T)need_bytes);

    uint8_t *rf = (uint8_t *)blk;
    uint8_t *ui = (uint8_t *)blk + (uint64_t)n * sizeof(GocRF);
    uint8_t *cur = ui;
    const uint8_t *q = p; uint64_t r = remain;
    for (uint32_t k = 0; k < n; k++) {
        uint32_t start = rd_u32(q + 0);
        uint32_t end   = rd_u32(q + 4);
        uint32_t nreg  = q[8];
        int pushes[16]; uint32_t pr = 0;
        for (uint32_t i = 0; i < nreg && pr < 16; i++) pushes[pr++] = q[9 + i];
        uint32_t alloc = rd_u32(q + 9 + nreg);

        /* Prolog byte offsets (for descending-order unwind codes).
         * push reg: 1 byte for rbp/rbx/rax..rdi, 2 bytes for r8..r15.
         * The canonical prolog is `push rbp (1B); mov rbp, rsp (3B);
         * push <callee-saves>; sub rsp, N`, so the 3-byte `mov` sits BETWEEN
         * the rbp push and the callee-save pushes. We therefore add its size
         * right after the first (rbp) push so the subsequent pushes get their
         * true offsets.  sub rsp: 4 (imm8) or 7 (imm32). */
        int off = 0; int po[16];
        for (uint32_t i = 0; i < pr; i++) {
            po[i] = off;
            int rg = pushes[i];
            off += (rg >= 8 && rg <= 15) ? 2 : 1;
            if (i == 0) off += 3;                   /* `mov rbp, rsp` follows rbp */
        }
        int allocOff = off;
        int subSize  = (alloc > 0) ? (alloc <= 127 ? 4 : 7) : 0;
        int sizeOfProlog = off + subSize;

        /* Emit unwind codes in DESCENDING prolog-offset order:
         * alloc first (highest offset), then pushes in reverse execution order. */
        uint8_t *codes = cur + 4;
        int nslots = 0;
        if (alloc > 0) {
            *codes++ = (uint8_t)allocOff;
            /* UNWIND_CODE byte: low nibble = UnwindOp, high nibble = OpInfo.
             * UWOP_ALLOC_LARGE = 1, OpInfo 0 => unscaled 4-byte size. */
            *codes++ = 0x01;                        /* UWOP_ALLOC_LARGE, OpInfo=0 */
            *codes++ = (uint8_t)(alloc & 0xFF);
            *codes++ = (uint8_t)((alloc >> 8) & 0xFF);
            *codes++ = (uint8_t)((alloc >> 16) & 0xFF);
            *codes++ = (uint8_t)((alloc >> 24) & 0xFF);
            nslots += 3;
        }
        for (int i = (int)pr - 1; i >= 0; i--) {
            *codes++ = (uint8_t)po[i];
            /* UWOP_PUSH_NONVOL = 0; OpInfo (high nibble) = register number. */
            *codes++ = (uint8_t)(pushes[i] << 4);   /* UWOP_PUSH_NONVOL, OpInfo=reg */
            nslots += 1;
        }
        cur[0] = 0x01;                              /* version 1, no flags */
        cur[1] = (uint8_t)sizeOfProlog;
        cur[2] = (uint8_t)nslots;
        cur[3] = 0;                                /* no frame register */

        GocRF *rfent = (GocRF *)(rf + (uint64_t)k * sizeof(GocRF));
        rfent->BeginAddress = start;
        rfent->EndAddress   = end;
        rfent->UnwindData   = (ULONG)((uintptr_t)cur - 0x400000ULL);

        uint32_t ui_size = 4 + 2 * (uint32_t)nslots;
        if (ui_size & 3) ui_size += 4 - (ui_size & 3);
        cur += ui_size;
        q += 9 + nreg + 4; r -= 9 + nreg + 4;
    }

    RtlAddFunctionTable((PRUNTIME_FUNCTION)rf, n, 0x400000ULL);

    {
        BOOLEAN ok = TRUE;
        fprintf(stderr, "[gocrun] unwind: registered %u funcs at %p ret=%d\n",
                n, (void *)blk, (int)ok);
        for (uint32_t k = 0; k < n && k < 3; k++) {
            GocRF *e = (GocRF *)(rf + (uint64_t)k * sizeof(GocRF));
            fprintf(stderr, "[gocrun]   rf[%u] begin=%x end=%x uw=%x\n",
                    k, e->BeginAddress, e->EndAddress, e->UnwindData);
            const uint8_t *ui2 = (const uint8_t *)(uintptr_t)(0x400000ULL + e->UnwindData);
            fprintf(stderr, "[gocrun]   ui bytes:");
            for (int b = 0; b < 16 && b < (int)need_bytes; b++) fprintf(stderr, " %02x", ui2[b]);
            fprintf(stderr, "\n");
        }
    }

    uint64_t top = blk + need_bytes;
    /* VirtualAlloc(MEM_RESERVE) reserves at 64KB granularity, so the region
     * actually owned by `blk` extends to the next 64KB boundary. The heap must
     * start past that, or the first grow_heap commit fails with 487. */
    return (top + ALLOC_GRAN - 1) & ~(ALLOC_GRAN - 1);
}

static int elf_load_and_patch(const char *path, uint64_t *out_entry,
                              uint64_t *out_elfTop, uint64_t *out_stub) {
    FILE *f = fopen(path, "rb");
    if (!f) return -1;
    fseek(f, 0, SEEK_END);
    long sz = ftell(f);
    fseek(f, 0, SEEK_SET);
    if (sz < 0 || sz < 0x40) { fclose(f); return -1; }
    uint8_t *data = malloc((size_t)sz);
    if (!data) { fclose(f); return -1; }
    if (fread(data, 1, (size_t)sz, f) != (size_t)sz) {
        free(data); fclose(f); return -1;
    }
    fclose(f);

    fprintf(stderr, "[gocrun] loaded %lld bytes\n", (long long)sz); fflush(stderr);
    if (data[0] != 0x7f || data[1] != 'E' || data[2] != 'L' || data[3] != 'F'
        || data[4] != 2 || data[5] != 1) {
        free(data); return -1;
    }

    uint64_t phoff   = rd_u64(data + 0x20);
    uint16_t phentsz = (uint16_t)rd_u16(data + 0x36);
    uint16_t phnum   = (uint16_t)rd_u16(data + 0x38);
    uint64_t shoff   = rd_u64(data + 0x28);
    uint16_t shentsz = (uint16_t)rd_u16(data + 0x3a);
    uint16_t shnum   = (uint16_t)rd_u16(data + 0x3c);
    *out_entry = rd_u64(data + 0x18);

    /* Map every PT_LOAD segment at its link address. */
    uint64_t max_end = 0;
    for (uint16_t i = 0; i < phnum; i++) {
        size_t off = (size_t)(phoff + (uint64_t)i * phentsz);
        uint32_t p_type = (uint32_t)rd_u32(data + off);
        if (p_type != 1) continue;     /* PT_LOAD */
        uint64_t p_offset = rd_u64(data + off + 8);
        uint64_t p_vaddr  = rd_u64(data + off + 16);
        uint64_t p_filesz = rd_u64(data + off + 32);
        uint64_t p_memsz  = rd_u64(data + off + 40);
        uint64_t base = p_vaddr;
        uint64_t end  = align_up(p_vaddr + p_memsz, PAGE);
        void *m = VirtualAlloc((void *)base, end - base,
                               MEM_COMMIT | MEM_RESERVE, PAGE_EXECUTE_READWRITE);
        fprintf(stderr, "[gocrun] PT_LOAD base=%llx size=%llx -> %p\n",
                (unsigned long long)base, (unsigned long long)(end - base), m);
        fflush(stderr);
        if (!m) { free(data); return -1; }
        memcpy((void *)(uintptr_t)(p_vaddr), data + p_offset, (size_t)p_filesz);
        if (p_vaddr + p_memsz > max_end) max_end = p_vaddr + p_memsz;
    }
    fprintf(stderr, "[gocrun] segments mapped, max_end=%llx\n",
            (unsigned long long)max_end); fflush(stderr);

    /* Locate __goc_syscall via .symtab (single PT_LOAD, so vaddr == host ptr). */
    uint8_t *syscall_addr = NULL;
    for (uint16_t i = 0; i < shnum; i++) {
        size_t off = (size_t)(shoff + (uint64_t)i * shentsz);
        uint32_t sh_type = (uint32_t)rd_u32(data + off + 4);
        if (sh_type != 2) continue;            /* SHT_SYMTAB */
        uint64_t sym_off  = rd_u64(data + off + 24);
        uint64_t sym_size = rd_u64(data + off + 32);
        uint32_t sh_link  = (uint32_t)rd_u32(data + off + 40);
        /* string table section */
        size_t stroff = (size_t)(shoff + (uint64_t)sh_link * shentsz);
        uint64_t str_off = rd_u64(data + stroff + 24);
        uint64_t n = sym_size / 24;
        for (uint64_t s = 0; s < n; s++) {
            size_t so = (size_t)(sym_off + s * 24);
            uint32_t st_name = (uint32_t)rd_u32(data + so);
            uint64_t st_value = rd_u64(data + so + 8);
            const char *nm = (const char *)(data + str_off + st_name);
            if (strcmp(nm, "__goc_syscall") == 0) {
                syscall_addr = (uint8_t *)(uintptr_t)st_value;
                fprintf(stderr, "[gocrun] found __goc_syscall at %p\n",
                        (void *)syscall_addr); fflush(stderr);
                break;
            }
        }
        if (syscall_addr) break;
    }
    /* image_end (page-aligned end of the loaded image) is where we reserve the
     * guest unwind table; the heap starts just above it. */
    uint64_t image_end = align_up(max_end, PAGE);
    /* Register the guest unwind table so Windows x64 can walk guest frames
     * during a Win32 syscall's internal exception dispatch. Without it the
     * 0x400000 frames have no unwind info and the unwinder segfaults (the
     * original gocrun crash). The table is reserved right after the image, and
     * the returned address becomes the new heap base so sbrk never overlaps it. */
    uint64_t heap_top = register_guest_unwind(data, shoff, shentsz, shnum,
                                              image_end);
    free(data);
    if (!syscall_addr) return -1;
    image_end = heap_top;   /* heap starts above the unwind table */

    /* Stub page: a single RWX page holding the entry transfer stub (set rsp +
     * jmp to _start; written in main()). With the UD2+VEH scheme the syscall
     * dispatch no longer needs an absolute-jmp shim here -- the VEH handler
     * reads the guest registers straight out of the CONTEXT. The page right
     * after the image is not reliably free (adjacent-loader reservation), so
     * try a few fixed low addresses that are almost always available in a
     * 64-bit process. */
    uint8_t *stub = NULL;
    uint64_t cands[] = { image_end, 0x10000000, 0x20000000, 0x30000000,
                         0x40000000, 0x50000000, 0x60000000, 0x70000000 };
    for (size_t ci = 0; ci < sizeof(cands) / sizeof(cands[0]) && !stub; ci++) {
        stub = (uint8_t *)VirtualAlloc((void *)cands[ci], PAGE,
                                       MEM_COMMIT | MEM_RESERVE,
                                       PAGE_EXECUTE_READWRITE);
        if (stub)
            fprintf(stderr, "[gocrun] stub at %p (cand %llx)\n",
                    (void *)stub, (unsigned long long)cands[ci]);
    }
    if (!stub) {
        fprintf(stderr, "[gocrun] stub alloc failed, err=%lu\n", GetLastError());
        return -1;
    }

    /* Patch __goc_syscall: `syscall` (0F 05) -> `ud2` (0F 0B). The VEH handler
     * catches the #UD and dispatches the syscall; it then bumps RIP past the
     * ud2 (fault semantics) and resumes the guest. */
    syscall_addr[0] = 0x0F;
    syscall_addr[1] = 0x0B;
    g_patched_ud2 = (uint64_t)(uintptr_t)syscall_addr;

    *out_elfTop  = image_end;
    *out_stub    = (uintptr_t)stub;
    fprintf(stderr, "[gocrun] patch: __goc_syscall=%p -> ud2 (0F 0B)\n",
            (void *)syscall_addr);
    fprintf(stderr, "[gocrun] __goc_syscall bytes: %02x %02x\n",
            syscall_addr[0], syscall_addr[1]);
    return 0;
}

/* Called from the trampoline (via an asm stack switch to the saved main stack,
 * so no guest frames remain on the stack when ExitProcess runs). */
void goc_exit_now(void) {
    cleanup_tmpdir();
    ExitProcess((UINT)g_exit_code);
}

int main(int argc, char **argv) {
    if (argc < 2) {
        fprintf(stderr, "usage: gocrun.exe <linux-elf> [args...]\n");
        return 2;
    }

    SetUnhandledExceptionFilter(crash_cleanup);
    AddVectoredExceptionHandler(1, veh_handler);
    for (int i = 0; i < MAX_FD; i++) { g_fds[i] = -1; g_dirh[i] = INVALID_HANDLE_VALUE; }
    g_fds[0] = 0; g_fds[1] = 1; g_fds[2] = 2; g_next_fd = 3;
    g_brk_cur = 0; g_heap_top = 0;

    uint64_t entry, elfTop, stub;
    if (elf_load_and_patch(argv[1], &entry, &elfTop, &stub) != 0) {
        fprintf(stderr, "gocrun: cannot load ELF %s\n", argv[1]);
        return 1;
    }
    fprintf(stderr, "[gocrun] back in main, entry=%llx elfTop=%llx\n",
            (unsigned long long)entry, (unsigned long long)elfTop);
    fflush(stderr);

    /* Initial break = first page past the loaded image; heap grows upward. */
    g_brk_cur  = elfTop;        /* image_end (page-aligned) */
    g_heap_top = g_brk_cur;

    /* ---- build the initial stack (argc/argv, SysV style) ----
     * Built ON THE MAIN THREAD STACK (a few KB below the current rsp). Running
     * the guest -- and its syscall translation, which calls into Win32/CRT --
     * on a separate VirtualAlloc'd stack is unreliable: that memory is not
     * registered as the thread's stack in the TIB, so Windows stack probes
     * fault inside Win32 calls such as SetCurrentDirectoryA / _close. Using the
     * real main stack avoids the whole class of problems. ---- */
    int nargs = argc - 2;
    if (nargs < 0) nargs = 0;
    char **toks = malloc(sizeof(char *) * (nargs + 1));
    toks[0] = "gocrun";
    for (int i = 0; i < nargs; i++) toks[i + 1] = argv[i + 2];
    int ntok = nargs + 1;

    uint64_t words[256];
    int nwords = 0;
    uint64_t rsp;
    asm volatile ("movq %%rsp, %0" : "=r"(rsp));
    {
        ULONG_PTR slo, shi;
        GetCurrentThreadStackLimits(&slo, &shi);
        fprintf(stderr, "[gocrun] main rsp=%llx stack=[%llx,%llx]\n",
                (unsigned long long)rsp,
                (unsigned long long)slo, (unsigned long long)shi);
        fflush(stderr);
    }
    rsp -= 0x4000;                 /* safety margin below main's own frame */
    for (int i = ntok - 1; i >= 0 && nwords < 256; i--) {
        size_t len = strlen(toks[i]);
        rsp -= (len + 1);
        memcpy((void *)(uintptr_t)rsp, toks[i], len + 1);
        words[nwords++] = rsp;
    }
    for (int i = 0; i < nwords / 2; i++) {
        uint64_t t = words[i]; words[i] = words[nwords - 1 - i];
        words[nwords - 1 - i] = t;
    }
    rsp -= 8 * (uint64_t)(nwords + 2);
    rsp &= ~0xFULL;
    uint64_t p = rsp;
    uint64_t argc_val = (uint64_t)nwords;
    memcpy((void *)(uintptr_t)p, &argc_val, 8); p += 8;
    for (int i = 0; i < nwords; i++) {
        memcpy((void *)(uintptr_t)p, &words[i], 8); p += 8;
    }
    uint64_t zero = 0;
    memcpy((void *)(uintptr_t)p, &zero, 8);   /* NULL argv terminator */
    p += 8;
    memcpy((void *)(uintptr_t)p, &zero, 8);   /* empty envp */
    free(toks);

    /* Transfer stub inside the mapped stub page (RWX): set rsp, then jmp to _start. */
    uint8_t *transfer = (uint8_t *)stub;
    transfer[0] = 0x48; transfer[1] = 0xBC;   /* mov rsp, imm64 */
    memcpy(transfer + 2, &rsp, 8);
    int64_t tdisp = (int64_t)entry - ((int64_t)(uintptr_t)transfer + 15);
    transfer[10] = 0xE9;
    transfer[11] = (uint8_t)(tdisp);
    transfer[12] = (uint8_t)(tdisp >> 8);
    transfer[13] = (uint8_t)(tdisp >> 16);
    transfer[14] = (uint8_t)(tdisp >> 24);

    /* chdir into a private temp dir so file I/O is isolated. */
    make_tmpdir();
    GetCurrentDirectoryA(sizeof(g_origdir), g_origdir);
    SetCurrentDirectoryA(g_tmpdir);

    /* Hand control to the guest. The guest runs to completion and ends via the
     * exit syscall: translate() sets g_exit_requested, the VEH handler lands
     * RIP on goc_exit_stub, which switches rsp back to the saved main stack
     * and calls goc_exit_now(). */
    void (*guest_entry)(void) = (void (*)(void))(uintptr_t)transfer;
    asm volatile ("movq %%rsp, %0" : "=r"(g_main_rsp));
    g_main_rsp &= ~0xFULL;          /* align to 16 so the called exit fn is ok */
    guest_entry();
    return 0;                       /* unreachable */
}
