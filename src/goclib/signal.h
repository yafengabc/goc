#ifndef GOC_SIGNAL_H
#define GOC_SIGNAL_H

/* goc signal.h -- a minimal C89 <signal.h>.
 *
 * Unix signal delivery is an OS facility with no direct counterpart in the
 * Win32 calls goc talks to: Windows routes Ctrl-C through a console control
 * handler that runs on a thread of its own, and it delivers the conditions
 * Unix calls SIGSEGV/SIGFPE/SIGILL as structured exceptions through a
 * completely separate mechanism. goc implements the one mapping that is
 * clean -- SIGINT, via SetConsoleCtrlHandler -- and records handlers for the
 * rest, so a program that installs them links and runs. They are simply never
 * delivered, which is the honest limit of this implementation rather than a
 * silent no-op: raise() still calls the handler a program installed.
 *
 * Real code needs this. Nim's system.nim installs handlers for SIGINT,
 * SIGSEGV, SIGABRT, SIGFPE and SIGILL before main runs, and its generated C
 * does not link without signal().
 *
 * The numbers are the Microsoft CRT's, which is what a Windows program's
 * generated C assumes when it writes them literally.
 */

typedef void (*__goc_sigfn)(int);

#define SIGINT   2
#define SIGILL   4
#define SIGFPE   8
#define SIGSEGV  11
#define SIGTERM  15
#define SIGBREAK 21
#define SIGABRT  22
#define NSIG     23

#define SIG_DFL ((__goc_sigfn)0)
#define SIG_IGN ((__goc_sigfn)1)
#define SIG_ERR ((__goc_sigfn)-1)

extern __goc_sigfn signal(int sig, __goc_sigfn handler);
extern int raise(int sig);

#endif /* GOC_SIGNAL_H */
