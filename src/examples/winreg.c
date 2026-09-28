/* winreg.c -- full message-loop window demo, exercising the struct-based
 * user32 surface that the old scalar-only windows.h could not express.
 *
 * Registers a window class, creates a window with the 12-argument
 * CreateWindowExA, runs a GetMessage/TranslateMessage/DispatchMessage loop,
 * paints text into a WM_PAINT via BeginPaint/EndPaint/DrawTextA, and exits
 * after 5 timer ticks by posting WM_CLOSE -> WM_DESTROY -> PostQuitMessage.
 *
 * The auto-exit is driven by SetTimer/WM_TIMER rather than paint counting:
 * a window that ends up behind other windows never receives WM_PAINT, so a
 * paint counter would hang an unattended run. WM_TIMER is dispatched
 * regardless of visibility, which makes the demo verifiable headlessly.
 *
 * There is no golden file on purpose: the output depends on the machine and
 * the window must be visible to be painted. CI compiles it to prove the
 * kernel32/user32/gdi32 imports and the long-argument call link; run it
 * manually to watch the window.
 *
 * This example is the reason maxArgs was raised from 8 to 16: CreateWindowExA
 * takes 12 arguments, which exceeds the old limit.
 */

#include <windows.h>
#include <stdio.h>

#define PAINT_TOTAL 5

/* Window procedure: receives all messages for our window. */
LRESULT CALLBACK WndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
    static int tick_count = 0;
    if (msg == WM_CREATE) {
        SetTimer(hwnd, 1, 500, NULL);
        return 0;
    }
    if (msg == WM_TIMER) {
        tick_count = tick_count + 1;
        InvalidateRect(hwnd, NULL, TRUE);
        if (tick_count >= PAINT_TOTAL) {
            KillTimer(hwnd, 1);
            PostMessageA(hwnd, WM_CLOSE, 0, 0);
        }
        return 0;
    }
    if (msg == WM_PAINT) {
        PAINTSTRUCT ps;
        HDC dc = BeginPaint(hwnd, &ps);
        RECT rc;
        char buf[64];
        sprintf(buf, "goc window - paint #%d", tick_count + 1);
        SetBkMode(dc, TRANSPARENT);
        SetTextColor(dc, 0x0000FF); /* red text */
        SetRect(&rc, 10, 10, 400, 100);
        DrawTextA(dc, buf, -1, &rc, DT_LEFT | DT_NOCLIP);
        EndPaint(hwnd, &ps);
        return 0;
    }
    if (msg == WM_DESTROY) {
        PostQuitMessage(0);
        return 0;
    }
    return DefWindowProcA(hwnd, msg, wp, lp);
}

int main(void) {
    HINSTANCE inst = GetModuleHandleA(NULL);

    WNDCLASSEX wc;
    wc.cbSize = sizeof(WNDCLASSEX);
    wc.style = CS_HREDRAW | CS_VREDRAW;
    wc.lpfnWndProc = WndProc;
    wc.cbClsExtra = 0;
    wc.cbWndExtra = 0;
    wc.hInstance = inst;
    wc.hIcon = LoadIconA(NULL, IDI_APPLICATION);
    wc.hCursor = LoadCursorA(NULL, IDC_ARROW);
    wc.hbrBackground = (HBRUSH)(COLOR_WINDOW + 1);
    wc.lpszMenuName = NULL;
    wc.lpszClassName = "gocWinClass";
    wc.hIconSm = NULL;

    if (!RegisterClassExA(&wc)) {
        printf("RegisterClassExA failed, err=%lu\n", GetLastError());
        return 1;
    }

    HWND hwnd = CreateWindowExA(
        0,                    /* ex style */
        "gocWinClass",        /* class */
        "goc winreg",         /* title */
        WS_OVERLAPPEDWINDOW | WS_VISIBLE,
        100, 100,             /* x, y */
        420, 160,             /* width, height */
        NULL,                 /* parent */
        NULL,                 /* menu */
        inst,                 /* instance */
        NULL);                /* param */

    if (hwnd == NULL) {
        printf("CreateWindowExA failed, err=%lu\n", GetLastError());
        return 1;
    }

    ShowWindow(hwnd, SW_SHOW);
    UpdateWindow(hwnd);

    MSG msg;
    while (GetMessageA(&msg, NULL, 0, 0)) {
        TranslateMessage(&msg);
        DispatchMessageA(&msg);
    }

    printf("winreg done, exit=%d\n", (int)msg.wParam);
    return (int)msg.wParam;
}
