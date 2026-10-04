# gomd × goc 真机测试清单

日期：2026-10-04　　构建：`goc -mwindows`（11 文件）　　结果：**438 通过 / 0 失败**（与 gcc 基准一致）

产物：`D:/Projects/goc/tmp/gomd.exe`（466,432 字节）

---

## 一、本轮修掉的 5 个 goc bug

全部是「编译通过、运行出错」类，无编译告警。已加回归测试：
`src/gomd_regress_test.go`（4 个用例）+ 更新 `src/winmain_test.go`（3 个）。

### 1. `unsigned char` / `unsigned short` 收窄转换错误符号扩展
- **位置**：`src/codegen.go` `CastExpr`
- **现象**：`(unsigned char)0xE2` 得 `0xFFFFFFE2`（应 `0xE2`）；UTF-8 字面量逐字节打印全错
- **根因**：`canonInt` 只做 32 位截断（`shl rax,32; sar rax,32`），对 8/16 位无掩码
- **修法**：按目标宽度分派——宽 1 用 `and eax,0xFF`、宽 2 用 `and eax,0xFFFF`；有符号侧用 `shl/sar 56`（char）、`shl/sar 48`（short）
- **回归**：`TestCastUnsignedCharZeroExtend`、`TestCastUnsignedShortZeroExtend`

### 2. goclib `_vsnprintf_s` / `_snprintf_s` 参数顺序与 MSVC 相反 ★主因
- **位置**：`src/goclib/stdio.c`、`stdio.h`
- **现象**：所有 `_vsnprintf_s/_snprintf_s(...,_TRUNCATE,...)` **静默返回空串**；直接调用段错误
- **根因**：原型写成 `(buf, count, size, ...)` 并拿**第 3 参**当边界；而真实调用惯例是
  `_vsnprintf_s(buf, sizeof buf, _TRUNCATE, fmt, ap)`，`_TRUNCATE`=`(size_t)-1` 落进边界槽 →
  `vsnprintf` 算出 `cap = (long)SIZE_MAX-1` → 溢出为负 → 夹成 **0**
- **修法**：改为 MSVC 顺序 `(buf, size_of_buffer, count, ...)`，边界取 `size_of_buffer`，`count` 仅作二次收紧，`_TRUNCATE` = 填满缓冲区
- **影响**：gomd 8 处调用点。markdown 列表标记 `"%d."` 变空 → 4 个失败且**消息全空**（因 `fail()` 自己也用它）
- **回归**：`TestGoclibSnprintfSArgOrder`

### 3. goclib `OBJ_*` 常量表与真实 SDK 不符 ★双缓冲读回失败
- **位置**：`src/goclib/wingdi.h`
- **现象**：`GetCurrentObject(dc, OBJ_BITMAP)` 恒返回 NULL → `ink_ratio_dc` 返回 -1 → 渲染层 3 个失败
- **根因**：goclib 自造了一套 0-based 值（`OBJ_BITMAP 0`）；真实 `wingdi.h` 从 `OBJ_PEN=1` 起编，`OBJ_BITMAP=7`。向 GDI 要「类型 0」**不报错、只返回 NULL**
- **修法**：整套换成 SDK 权威值，并补 `GDI_MIN/MAX_OBJ_TYPE`
- **回归**：`TestGoclibObjConstantsMatchSDK`

### 4. goclib 缺 4 个 Win32 入口（硬编译错误）
- `GetProcAddress`（winbase.h）、`FARPROC`（windef.h）、`GetObjectType`、`HGDI_ERROR`（wingdi.h）
- **回归**：`TestGoclibWin32SurfacePresent`

### 5. GUI 入口桩命令行未剥离程序名
- **位置**：`src/goclib/args.c`（`__goclib_get_cmdline_w/a`）
- **现象**：`wWinMain` 收到 `"gomd.exe --test"` 而非 `"--test"`，参数比较全不匹配
- **状态**：本轮只更新了过时的测试断言（`winmain_test.go` 仍固化旧的「直调 GetCommandLineW」）

---

## 二、 RegisterClassExW


---

## 三、Win10 真机测试步骤

```bat
REM 1) 自测（三种模式分开跑；日志在 %TEMP%\MDQuickViewer-test.log）
D:\Projects\goc\tmp\gomd.exe --test-md
D:\Projects\goc\tmp\gomd.exe --test-hl
D:\Projects\goc\tmp\gomd.exe --test-render
REM 期望：96/0、62/0、280/0

REM 2) 正常 GUI（会弹窗口；关掉即可）
D:\Projects\goc\tmp\gomd.exe
```

### 建议人工确认的渲染项（自测覆盖不到）
- [ ] 窗口正常显示，中文/emoji 不乱码
- [ ] **语法着色**：代码块有颜色（此前 `CreateCompatibleBitmap` 1 位单色会全黑）
- [ ] 列表圆点 `•` / `◦` / `▪` 分层缩进正确
- [ ] 表格对齐、链接可点击
- [ ] 缩放窗口 → 内容重排不撕裂（双缓冲）
- [ ] 选中文字、反选高亮
- [ ] `Ctrl+滚轮` 缩放、滚动流畅

---

## 四、重建命令（备查）

```bash
cd /d/Projects/goc && bash build.sh          # 改 goclib/*.h 后须先 touch src/headers.go
cd /d/Projects/gomd/MDQuickViewer/src
/d/Projects/goc/bin/goc.exe -mwindows \
  dlg.c highlight.c highlight_test.c main.c markdown.c markdown_test.c \
  render.c render_test.c settings.c ui.c win32.c \
  -o /d/Projects/goc/tmp/gomd.exe
```

**必须排除 `layout_stress.c`**：它自带 `int main(void)`，而 goc 选入口时 `main` 优先于 `wWinMain`，
glob 全部 `*.c` 会跑错入口、不写日志。

goc 自身回归：`cd /d/Projects/goc/src && go test ./...`（当前全绿）
