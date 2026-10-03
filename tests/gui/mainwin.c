/* mainwin.c -- goc 的 Win32 主窗口程序测试。
 *
 * 这是一个不依赖项目代码的、自校验的 GUI 程序，用来回答四个问题：
 *   1. goc 能不能从 wWinMain 入口建出一个真正的主窗口（非对话框、非 MessageBox）；
 *   2. RegisterClassExW / CreateWindowExW / 消息循环 / WM_PAINT 是否端到端可用；
 *   3. GDI 绘制是否真的落到像素上（截图可证，而不是只看返回值）；
 *   4. 窗口能不能自己干净退出（定时器 -> WM_DESTROY -> PostQuitMessage）。
 *
 * 自校验：成功绘制至少一帧返回 0，否则返回非 0。因此它既能当回归测试跑，
 * 也能当截图脚本的驱动。
 *
 * 窗口 3 秒后自动关闭（SetTimer），也可以按 Esc 立刻关闭，所以无人值守也能跑完。
 *
 * 构建：goc mainwin.c -o mainwin.exe -mwindows
 * 运行：./mainwin.exe ; echo $?        （Windows 上是 %ERRORLEVEL%）
 */
#include <windows.h>

/* 注册用的窗口类名。程序只跑一次，不存在重复注册。 */
#define TEST_CLASS_NAME  L"GocMainWinTest"
#define TIMER_ID         1
#define CLOSE_AFTER_MS   3000

static int g_frames   = 0;   /* WM_PAINT 计数：证明确实画了 */
static int g_paints_ok = 1;  /* 绘图过程里是否有 GDI 失败 */

/* ------------------------------------------------------------------ */
/* 绘制                                                                */
/* ------------------------------------------------------------------ */
static void put_int(HDC dc, int x, int y, int v) {
    char buf[24];
    char tmp[24];
    int k = 0, j = 0;
    if (v == 0) { tmp[j++] = '0'; }
    else {
        int neg = 0;
        if (v < 0) { neg = 1; v = -v; }
        while (v) { tmp[j++] = (char)('0' + v % 10); v /= 10; }
        if (neg) tmp[j++] = '-';
    }
    while (j) buf[k++] = tmp[--j];
    buf[k] = 0;
    TextOutA(dc, x, y, buf, k);
}

/* UTF-8 -> UTF-16 into a fresh LocalAlloc block. The test inlines this rather
 * than borrowing the MDQuickViewer helper, so it depends only on goclib -- and
 * it exercises the same MultiByteToWideChar + LocalAlloc path real Win32 code
 * uses to show non-ASCII text. Caller frees with LocalFree. */
static wchar_t *utf8_to_wide(const char *s) {
    int n = MultiByteToWideChar(CP_UTF8, 0, s, -1, NULL, 0);
    if (n <= 0) return NULL;
    wchar_t *w = (wchar_t *)LocalAlloc(LMEM_FIXED, (SIZE_T)n * sizeof(wchar_t));
    if (!w) return NULL;
    if (MultiByteToWideChar(CP_UTF8, 0, s, -1, w, n) <= 0) {
        LocalFree(w);
        return NULL;
    }
    return w;
}

static void paint(HDC dc, int w, int h) {
    RECT all;
    all.left = 0; all.top = 0; all.right = w; all.bottom = h;

    /* 背景 */
    HBRUSH bg = CreateSolidBrush(RGB(0xFA, 0xFA, 0xF5));
    if (!bg) { g_paints_ok = 0; return; }
    FillRect(dc, &all, bg);
    DeleteObject(bg);

    /* 标题栏色带：证明 GDI 形状绘制可用 */
    RECT band;
    band.left = 0; band.top = 0; band.right = w; band.bottom = 44;
    HBRUSH bandBr = CreateSolidBrush(RGB(0x1F, 0x4E, 0x79));
    if (!bandBr) { g_paints_ok = 0; return; }
    FillRect(dc, &band, bandBr);
    DeleteObject(bandBr);

    SetBkMode(dc, TRANSPARENT);
    SetTextColor(dc, RGB(0xFF, 0xFF, 0xFF));
    TextOutA(dc, 16, 14, "goc Win32 main-window test", 26);
    SetTextColor(dc, RGB(0x20, 0x20, 0x20));

    /* 边框 + 填充矩形 + 椭圆 + 直线：四种不同的 GDI 原语 */
    HPEN pen = CreatePen(PS_SOLID, 2, RGB(0x1F, 0x4E, 0x79));
    HBRUSH fill = CreateSolidBrush(RGB(0xCF, 0xE2, 0xF3));
    if (!pen || !fill) { g_paints_ok = 0; if (pen) DeleteObject(pen); if (fill) DeleteObject(fill); return; }
    HGDIOBJ oldPen = SelectObject(dc, pen);
    HGDIOBJ oldBr  = SelectObject(dc, fill);

    RECT box;
    box.left = 24; box.top = 72; box.right = 24 + (w - 72) / 2; box.bottom = 72 + 120;
    Rectangle(dc, box.left, box.top, box.right, box.bottom);

    int ex = box.right + 24;
    int ew = w - ex - 24;
    if (ew > 20) Ellipse(dc, ex, 72, ex + ew, 192);

    SelectObject(dc, oldPen);
    SelectObject(dc, oldBr);

    MoveToEx(dc, 24, 216, NULL);
    LineTo(dc, w - 24, 216);
    DeleteObject(pen);
    DeleteObject(fill);

    /* 状态文字：用 goclib 的宽字符转换把 UTF-8 转成宽串，
     * 这样中文也能画出来（顺带验证 MultiByteToWideChar + TextOutW）。 */
    HBRUSH info = CreateSolidBrush(RGB(0xE8, 0xF0, 0xE8));
    if (info) {
        RECT box2;
        box2.left = 24; box2.top = 236; box2.right = w - 24; box2.bottom = h - 20;
        FillRect(dc, &box2, info);
        DeleteObject(info);
    }
    SetBkMode(dc, TRANSPARENT);
    SetTextColor(dc, RGB(0x18, 0x50, 0x18));

    wchar_t *msg = utf8_to_wide("goc 主窗口测试：W 字消息循环正常，Esc 或 3 秒后自动退出。");
    if (msg) {
        TextOutW(dc, 36, 246, msg, (int)wcslen(msg));
        LocalFree(msg);
    } else {
        TextOutA(dc, 36, 246, "goc main window test: message loop OK", 36);
    }

    SetTextColor(dc, RGB(0x60, 0x60, 0x60));
    /* g_frames is bumped before paint() runs, so the number on screen is the
     * 1-based index of the frame being drawn -- "1" on the first pass reads
     * correctly, whereas incrementing afterwards would always show 0 here. */
    put_int(dc, 36, h - 52, g_frames);
    TextOutA(dc, 60, h - 52, "frames painted", 14);
}

/* ------------------------------------------------------------------ */
/* 窗口过程                                                            */
/* ------------------------------------------------------------------ */
static LRESULT CALLBACK test_wnd_proc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
    switch (msg) {
        case WM_CREATE:
            /* 窗口刚建好：立刻要一帧 WM_PAINT */
            InvalidateRect(hwnd, NULL, FALSE);
            return 0;

        case WM_ERASEBKGND:
            /* 背景由 WM_PAINT 统一画，这里必须返回 1，否则会闪 */
            return 1;

        case WM_PAINT: {
            PAINTSTRUCT ps;
            HDC dc = BeginPaint(hwnd, &ps);
            RECT rc;
            GetClientRect(hwnd, &rc);
            g_frames++;                 /* count first: paint() displays it */
            paint(dc, rc.right - rc.left, rc.bottom - rc.top);
            EndPaint(hwnd, &ps);
            return 0;
        }

        case WM_TIMER:
            if (wp == TIMER_ID) { DestroyWindow(hwnd); return 0; }
            return 0;

        case WM_KEYDOWN:
            if (wp == VK_ESCAPE) { DestroyWindow(hwnd); return 0; }
            return 0;

        case WM_DESTROY:
            PostQuitMessage(0);
            return 0;

        default:
            break;
    }
    return DefWindowProcW(hwnd, msg, wp, lp);
}

/* ------------------------------------------------------------------ */
/* 入口：goc 的 wWinMain 路径                                            */
/* ------------------------------------------------------------------ */
int WINAPI wWinMain(HINSTANCE inst, HINSTANCE prev, PWSTR cmdline, int show) {
    (void)prev;
    (void)cmdline;

    WNDCLASSEXW wc;
    ZeroMemory(&wc, sizeof(wc));
    wc.cbSize    = sizeof(wc);              /* 自引用 sizeof：Win32 标准写法 */
    wc.style     = CS_HREDRAW | CS_VREDRAW;
    wc.lpfnWndProc = test_wnd_proc;         /* 函数地址进结构体字段 */
    wc.hInstance = inst;                    /* 入口桩传进来的那个 */
    wc.hCursor   = LoadCursorW(NULL, IDC_ARROW);
    wc.hbrBackground = (HBRUSH)(COLOR_WINDOW + 1);
    wc.lpszClassName = TEST_CLASS_NAME;
    /* 不用图标：MAKEINTRESOURCEW(1) 在无资源的二进制上返回 NULL，
     * 上一轮排查 err=998 时先排除了它，这里同样不依赖。 */

    if (!RegisterClassExW(&wc)) return 10;   /* 类注册失败 */

    HWND hwnd = CreateWindowExW(
        0, TEST_CLASS_NAME, L"goc main-window test",
        WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN,
        CW_USEDEFAULT, CW_USEDEFAULT,
        680, 460,
        NULL, NULL, inst, NULL);
    if (!hwnd) return 11;                   /* 窗口创建失败 */

    SetTimer(hwnd, TIMER_ID, CLOSE_AFTER_MS, NULL);
    ShowWindow(hwnd, show);
    UpdateWindow(hwnd);

    MSG msg;
    while (GetMessageW(&msg, NULL, 0, 0) > 0) {
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }

    /* 自校验结果编码进退出码，便于脚本断言。 */
    if (!g_paints_ok)   return 20;           /* GDI 中途失败 */
    if (g_frames < 1)   return 21;           /* 一帧都没画出来 */
    return 0;                               /* 全部通过 */
}
