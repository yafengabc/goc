/* goc signal.c -- see signal.h.
 *
 * Handlers are recorded in a table indexed by signal number, and SIGINT is
 * additionally wired to the one Windows mechanism that can deliver it: the
 * console control handler, which the OS calls on a thread of its own when the
 * user presses Ctrl-C. The remaining signals have no delivery path here --
 * Windows reports those conditions as structured exceptions -- so installing
 * one is recorded and honoured by raise() but never fires on its own.
 */

#include "goclib.h"
#include <signal.h>

#if defined(_WIN32)
extern int SetConsoleCtrlHandler(int (*handler)(int), int add);
#endif

static __goc_sigfn handlers[NSIG];

#if defined(_WIN32)
/* CTRL_C_EVENT (0) and CTRL_BREAK_EVENT (1) are the two the console reports
 * for a keyboard interrupt. Returning non-zero tells Windows the event was
 * handled, which is what keeps the default "terminate" from also running. */
static int goc_console_ctrl(int type) {
    if ((type == 0 || type == 1) && handlers[SIGINT]) {
        handlers[SIGINT](SIGINT);
        return 1;
    }
    return 0;
}
#endif

__goc_sigfn signal(int sig, __goc_sigfn handler) {
    __goc_sigfn old;
    if (sig <= 0 || sig >= NSIG) return SIG_ERR;
    old = handlers[sig];
    /* SIG_ERR as an *input* is not meaningful; treat it as SIG_DFL rather
     * than storing a value that raise() would then have to special-case. */
    handlers[sig] = (handler == SIG_ERR) ? SIG_DFL : handler;
#if defined(_WIN32)
    if (sig == SIGINT) {
        int want = (handlers[sig] != SIG_DFL && handlers[sig] != SIG_IGN);
        SetConsoleCtrlHandler(goc_console_ctrl, want);
    }
#endif
    return old;
}

int raise(int sig) {
    if (sig <= 0 || sig >= NSIG) return -1;
    if (handlers[sig] && handlers[sig] != SIG_IGN) {
        handlers[sig](sig);
        return 0;
    }
    /* An unhandled abort still has to end the process the way the C standard
     * says an uncaught SIGABRT does. */
    if (sig == SIGABRT) abort();
    return 0;
}
