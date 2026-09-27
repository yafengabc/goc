/* winbox.c -- GUI demo of the windows.h surface.
 *
 * Pops a MessageBox with the screen size and process uptime. There is no
 * golden file on purpose: the box must be dismissed by hand, and the output
 * depends on the machine. CI compiles it to prove the user32 imports link;
 * run it manually to see the box.
 */

#include <windows.h>
#include <stdio.h>

int main(void) {
    int w = GetSystemMetrics(SM_CXSCREEN);
    int h = GetSystemMetrics(SM_CYSCREEN);
    DWORD ms = GetTickCount();

    char buf[128];
    sprintf(buf, "screen: %dx%d\nuptime: %d ms", w, h, ms);

    /* Real bitwise-or: goc supports | since 2026-09-28. */
    MessageBoxA(NULL, buf, "goc winbox", MB_OK | MB_ICONINFORMATION);
    return 0;
}
