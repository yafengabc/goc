/* wintest.c -- deterministic runtime check of the windows.h surface.
 *
 * Every result line is 0/1 so the golden file is stable across machines.
 * Compiles only for the Windows target (externDLL imports).
 *
 *   stdout handle ok=1        GetStdHandle returns a live handle
 *   writefile ok=1 wrote=22   WriteFile round-trips through kernel32
 *   kernel32 module=1         GetModuleHandleA finds a loaded module
 *   bad module=1              ... and returns NULL for an unknown one
 *   lasterror=1               SetLastError/GetLastError round-trip
 *   ticks nondecreasing=1     GetTickCount never goes backwards
 *   sleep delta ok=1          Sleep(50) really takes ~50ms
 *   screensize positive=1     GetSystemMetrics reports a real display
 *   fails=0                   all of the above
 */

#include <windows.h>
#include <stdio.h>
#include <string.h>

int main(void) {
    int fails = 0;

    HANDLE h = GetStdHandle(STD_OUTPUT_HANDLE);
    int hok = (h != 0 && h != INVALID_HANDLE_VALUE);
    printf("stdout handle ok=%d\n", hok);
    if (!hok) fails++;

    const char *msg = "wintest via WriteFile\n";
    DWORD wrote = 0;
    BOOL wok = WriteFile(h, msg, (DWORD)strlen(msg), &wrote, 0);
    printf("writefile ok=%d wrote=%d\n", wok, wrote);
    if (!wok || wrote != 22) fails++;

    HMODULE m = GetModuleHandleA("kernel32.dll");
    printf("kernel32 module=%d\n", m != 0);
    if (m == 0) fails++;

    HMODULE bad = GetModuleHandleA("no_such_module_xyz.dll");
    printf("bad module=%d\n", bad == 0);
    if (bad != 0) fails++;

    SetLastError(0x7777);
    DWORD le = GetLastError();
    printf("lasterror=%d\n", le == 0x7777);
    if (le != 0x7777) fails++;

    DWORD t0 = GetTickCount();
    DWORD t1 = GetTickCount();
    printf("ticks nondecreasing=%d\n", t1 >= t0);
    if (t1 < t0) fails++;

    Sleep(50);
    DWORD dt = GetTickCount() - t1;
    printf("sleep delta ok=%d\n", dt >= 30 && dt < 5000);
    if (dt < 30 || dt >= 5000) fails++;

    int sw = GetSystemMetrics(SM_CXSCREEN);
    int sh = GetSystemMetrics(SM_CYSCREEN);
    printf("screensize positive=%d\n", sw > 0 && sh > 0);
    if (sw <= 0 || sh <= 0) fails++;

    printf("fails=%d\n", fails);
    ExitProcess(fails == 0 ? 0 : 1);
    return 0;
}
