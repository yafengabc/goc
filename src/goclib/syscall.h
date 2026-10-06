/* =============================================================================
 * syscall.h -- the raw Linux system calls goclib makes, and how each one is
 * reached on whichever host is compiling.
 *
 * goclib talks to the kernel through two different routes, and which one is
 * available depends on who is compiling.
 *
 * Under goc, goa turns a name in its syscall table (goa/asm.go: linuxSyscalls)
 * into a three-instruction stub -- `mov rax,N; syscall; ret` -- so the call is a
 * direct trip into the kernel with no library in between. Most of those names
 * carry a __goclib_ prefix (__goclib_rename, __goclib_stat, __goclib_futex)
 * rather than the plain syscall name, and the reason is in goa's own comment:
 * goclib also defines a C function with that name. `rename' cannot extern
 * `rename', because the extern would resolve to its own definition -- infinite
 * recursion. So the wrapper calls an alias that only the stub table provides.
 *
 * Under a host compiler there is no such table, and the alias names mean
 * nothing: nothing defines them, so the link fails. But every one of them names
 * a Linux system call that the C library already reaches -- rename(2), stat(2),
 * futex(2) through syscall(SYS_futex, ...), and so on. So this header maps each
 * alias, and each raw call, to the route the host actually has:
 *
 *   __goclib_*        goc: goa emits a syscall stub under this name.
 *                     host: the libc function, or syscall(2) for the two that
 *                           libc does not wrap in that shape (futex, clone).
 *
 * Nothing here is part of goclib's public API: the wrappers a program calls
 * (rename, stat, mkdir) are declared in goclib's own <stat.h> with the standard
 * signatures. This header exists so the .c files can call one spelling and
 * reach the kernel on either host.
 *
 * The header is Linux-only, and the guard at the bottom keeps it that way:
 * every name below is a Linux system call and on Windows none of it exists.
 * goclib includes this header from ten translation units at file scope rather
 * than from inside their `#if defined(_WIN32) / #elif defined(__linux__)' arms,
 * because arming each include is a way to eventually forget one. Declaring
 * write(2) in a Windows TU is not merely useless -- goc's code generator
 * resolves a call it cannot find against goclib's function table and then
 * against the DLL imports, finds neither, and fails the whole build with
 * `unknown function "write": not in goclib'.
 * ========================================================================== */

#if defined(__linux__) || defined(__linux)

#ifdef __goc__

/* ---- under goc: goa's syscall stubs ---------------------------------------- */

extern long __goclib_rename(const char *oldp, const char *newp);
extern long __goclib_stat(const char *path, void *buf);
extern long __goclib_mkdir(const char *path, long mode);
extern long __goclib_rmdir(const char *path);
extern long __goclib_getdents64(long fd, void *buf, long count);
extern long __goclib_getcwd(char *buf, long size);
extern long __goclib_chmod(const char *path, long mode);
extern long __goclib_access(const char *path, long amode);
extern long __goclib_fstat(long fd, void *buf);
extern long __goclib_clone(long flags, void *stack, void *ptid, void *ctid, long tls);
extern long __goclib_futex(int *uaddr, long op, long val, void *timeout,
                           void *uaddr2, long val3);
extern long __goclib_gettid(void);
extern long __goclib_sched_yield(void);
extern void __goclib_exit_thread(long code);
extern long __goc_clock_gettime(int clk, void *ts);
/* system()'s three raw calls, aliased for the same reason as the rest: goa's
 * table carries the __goclib_ spellings (58 vfork, 59 execve, 61 wait4). */
extern long __goclib_vfork(void);
extern long __goclib_execve(const char *path, char **argv, char **envp);
extern long __goclib_wait4(long pid, long *status, long options, void *rusage);

/* The ones whose plain names goclib does NOT define, so no alias was needed
 * and the stub table carries the plain spelling. */
extern long read(long fd, void *buf, long n);
extern long write(long fd, const void *buf, long n);
extern long open(const char *path, long flags, long mode);
extern long close(long fd);
extern long lseek(long fd, long off, long whence);
extern void *brk(void *addr);
extern void *mmap(void *addr, long len, long prot, long flags, long fd, long off);
extern long munmap(void *addr, long len);
extern long nanosleep(const void *req, void *rem);
extern long unlink(const char *path);
extern void exit_group(long code);

/* goa's brk stub takes and returns a pointer, which is what is declared above,
 * so on this host the mapping is the identity. */
#define __goclib_brk(addr)                   ((void *)(addr))

/* ---- under a host compiler: libc, or syscall(2) ---------------------------- */

#else /* !__goc__ */

/* Only the names goclib does not already declare for itself. The public POSIX
 * surface -- stat, mkdir, rmdir, chmod, access, getcwd, rename -- is declared in
 * goclib's own <stat.h> with the standard signatures, and this header is included
 * alongside it, so redeclaring them here with a different spelling would be a
 * conflicting-types error rather than a convenience. What is left is the raw
 * file-descriptor layer, which goclib uses internally and never publishes: those
 * have no declaration in any goclib header. */
extern long  read(long fd, void *buf, long n);
extern long  write(long fd, const void *buf, long n);
extern long  open(const char *path, long flags, long mode);
extern long  close(long fd);
extern long  lseek(long fd, long off, long whence);
extern void *mmap(void *addr, long len, long prot, long flags, long fd, long off);
extern long  munmap(void *addr, long len);
extern long  nanosleep(const void *req, void *rem);
extern int   unlink(const char *path);
extern int   getpid(void);
extern int   gettid(void);
extern int   sched_yield(void);
/* vfork returns the child's pid as an int, not a long -- every compiler knows
 * it as a builtin with exactly that type, so a long here is a mismatch. */
extern int   vfork(void);
extern long  execve(const char *path, char **argv, char **envp);
extern long  wait4(long pid, long *status, long options, void *rusage);

/* brk, clone, futex and the stat/mkdir family all go through syscall(2) rather
 * than through libc, and each for its own reason:
 *
 *  - brk does not merely differ in signature, it does not work. Measured against
 *    musl 1.2.6: brk(0) through the compiler's builtin returns (void*)-1 -- the
 *    bump allocator reads that as "the kernel refused" and fails its very first
 *    malloc -- while syscall(SYS_brk, 0) in the same process returns the real
 *    break. So the syscall is not an alternative route here, it is the only one
 *    that answers.
 *
 *  - clone and futex have no libc function of the same shape: musl and glibc
 *    expose clone as a variadic wrapper whose stack argument sits elsewhere,
 *    and futex only as syscall(SYS_futex, ...).
 *
 *  - stat, mkdir, rmdir, getcwd, chmod, access, fstat and rename have libc
 *    functions, but calling them by name from inside goclib would call goclib's
 *    own wrapper: dir.c defines `int stat(const char *, struct stat *)` and that
 *    definition is the one the name resolves to, so `#define __goclib_stat ...
 *    stat(...)' turns the wrapper into infinite recursion -- which gcc reports
 *    as -Winfinite-recursion. This is the very reason goa's table carries the
 *    __goclib_ spellings in the first place, and the reason the aliases cannot
 *    be mapped to the libc names on this host.
 *
 *    The system call is also the more faithful route here, not merely the
 *    available one: stat.h's struct stat is the kernel's x86-64 layout, the one
 *    stat(2) fills in directly, so asking the kernel keeps goclib reading the
 *    bytes it was written against.
 *
 *  - getdents64 has no libc function at all. musl and glibc both keep it
 *    private, so declaring it and calling it by name compiles and then fails at
 *    link time with an undefined reference. SYS_getdents64 is 217.
 *
 * The numbers are x86-64 syscall numbers -- the same ones goa's table spells --
 * because <sys/syscall.h> is out of reach under -nostdinc. */
extern long  syscall(long number, ...);

#define __goclib_brk(addr)                   ((void *)syscall(12, (long)(addr)))
#define __goclib_clone(flags, stack, ptid, ctid, tls) \
    syscall(56, (flags), (stack), (ptid), (ctid), (tls))
#define __goclib_futex(uaddr, op, val, timeout, uaddr2, val3) \
    syscall(202, (uaddr), (op), (val), (timeout), (uaddr2), (val3))
#define __goclib_exit_thread(code)           syscall(60, (code))
#define __goc_clock_gettime(clk, ts)         syscall(228, (clk), (ts))

/* exit_group has no libc function of that name, and _exit is the closest
 * equivalent on the way out of a process -- it leaves immediately, without
 * running atexit handlers or flushing stdio, which is what a program that has
 * already torn down its own I/O wants. SYS_exit_group is 231.
 *
 * _Noreturn, because __goclib_exit is: without the annotation here gcc reports
 * "'noreturn' function does return" for os.c's __goclib_exit, having no way to
 * see that a _exit call at the end of the body stops there. */
extern _Noreturn void _exit(int code);
#define exit_group(code)                     _exit((int)(code))

/* The stat / directory family. None of these can use the libc name, for the two
 * reasons above: goclib defines stat/mkdir/rmdir/getcwd/chmod/access/fstat itself,
 * and getdents64 is not exported by libc at all. */
#define __goclib_stat(path, buf)             syscall(4,  (path), (buf))
#define __goclib_fstat(fd, buf)              syscall(5,  (long)(fd), (buf))
#define __goclib_access(path, mode)          syscall(21, (path), (mode))
#define __goclib_rename(oldp, newp)          syscall(82, (oldp), (newp))
#define __goclib_mkdir(path, mode)           syscall(83, (path), (mode))
#define __goclib_rmdir(path)                 syscall(84, (path))
#define __goclib_getcwd(buf, size)           syscall(79, (buf), (size))
#define __goclib_chmod(path, mode)           syscall(90, (path), (mode))
#define __goclib_getdents64(fd, buf, n)      syscall(217, (long)(fd), (buf), (long)(n))
#define __goclib_gettid()                    gettid()
#define __goclib_sched_yield()               sched_yield()
#define __goclib_vfork()                     vfork()
#define __goclib_execve(p, a, e)             execve((p), (a), (e))
#define __goclib_wait4(p, s, o, r)           wait4((p), (s), (o), (r))

#endif /* __goc__ */

#endif /* __linux__ */