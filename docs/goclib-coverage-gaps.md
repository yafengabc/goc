# goclib 测试覆盖缺口文档（Coverage Gaps）

- 维护目标：跟踪 goclib 已实现但**尚未写详尽测试**的导出函数，按模块列出签名、对拍可行性与测试策略。
- 最后更新：2026-10-06
- 关联文档：`tests/cstd/CSTD_STATUS.md`（逐字节对拍就绪度矩阵）、`.workbuddy/memory/MEMORY.md`（铁律与架构）

---

## 1. 覆盖是如何衡量的

goclib 的详尽测试走 `tests/cstd/` 交叉验证套件：

1. 每个用例 `.c` 同时用 `bin/goc.exe` 和 MSYS2 UCRT64 `gcc`（16.2.0）编译运行；
2. 两边输出做 CRLF 归一化（`tr -d '\r'`）后逐字节 `diff`；
3. 完全一致 = **PASS**，否则定位是 goclib 真缺陷还是测试侧/对拍侧问题。

套件入口：`powershell -ExecutionPolicy Bypass -File tests/cstd/run_cstd_tests.ps1`。
按文件名前缀分版本组（`c89/c99/c11/c17`），另设独立的 `cstd` 组专门收 `cstd_lib_*` 详尽用例（per-file `-std` 覆盖，已注册 7 个全部 PASS）。

---

## 2. 对拍陷阱（写用例前必读，避免假红）

| 陷阱 | 说明 |
|---|---|
| **ABI：LP64 vs LLP64** | goc 是 LP64（`long`=8）；mingw gcc 是 LLP64（`long`=4）。**不要直接比较 `long` 值或 `sizeof(long)`**。整型用 `int`/`long long`、浮点用 `double` 比对。 |
| **mingw 缺头文件** | mingw 16.x **没有** `<stdbit.h>` / `<threads.h>` / `<stdckdint.h>`。对拍这些特性时用 `#if __has_include(...)` 包真实头，else 自带纯 C 参考实现。 |
| **`stpcpy` 不导出** | mingw 的 `<string.h>` 不声明 `stpcpy`，C 运行库也**不导出该符号** → gcc 链接失败。对拍办法：用例里 `#ifdef STUB_STPCPY` 自带参考实现，gcc 侧传 `-DSTUB_STPCPY` 去对拍 goc 真 `stpcpy`。 |
| **`tzname`/`TZ` OS 相关** | `tzname[0]` 拼写取决于操作系统（无 `TZ`→中文区名；`TZ=UTC0`→`"UTC"`），`timespec_get` 返回墙钟 → 都**不可逐字节对拍**。只断言确定性不变量（返回码、指针非空）。 |
| **`errno` 是宏** | goclib 里 `errno` 展开为 `(*__goclib_errno_loc())`，不是全局变量；用例里直接 `#include <errno.h>` 用即可。 |
| **printf 大指数精度** | 极大 `double`（如 `scalbn(1.0,1023)`）位模式正确，但十进制字符串打印有精度差（goc 比 gcc 末几位偏）。对拍浮点用可精确打印的值（如 `scalbn(1.5,-1)`）。 |
| **goc 忽略 `-std`** | goc 接受但忽略 `-std=*`，所有用例同一语义；gcc 侧需按特性选 `-std=c99/c2x`。C23 特性（`<stdbit.h>`、`timespec_get` 的 `TIME_UTC`、`nullptr` 等）对拍 gcc 用 `-std=c2x`。 |

---

## 3. 缺口汇总

| 模块 | 已实现但未测函数数 | 备注 |
|---|---|---|
| threads（`<threads.h>`） | 10 | `cnd_*`(6) + `mtx_timedlock` + `thrd_detach/thrd_exit/thrd_sleep` |
| stdio（`<stdio.h>`） | 5 | `vscanf/vfscanf/vsscanf/tmpnam/fopen_s` |
| file/dir（`<stdio.h>/<stat.h>/<dirent.h>`） | 7 | `readdir/stat/fstat/getcwd/mkdir/rmdir/unlink` |
| math（`<math.h>`） | 3 | `nextafter/nexttoward/remquo` |
| time（`<time.h>`） | 8 | `gmtime/localtime/gmtime_r/localtime_r/ctime/ctime_r/asctime/asctime_r` |
| wchar（`<wchar.h>`） | 1 | `wcscat_s` |
| **合计** | **34** | 全部已实现，仅需补详尽测试 |

**已排除（非覆盖缺口）：** `open` / `close` 是 POSIX 底层 fd I/O，goclib 走 `FILE*`（fopen/fclose），不在范围内，不计入。

---

## 4. 详细缺口清单

### 4.1 threads（`<threads.h>`）— 10 个

| 函数 | 签名 | 对拍可行性 / 测试策略 | 备注 |
|---|---|---|---|
| `cnd_init` | `int cnd_init(cnd_t *cond)` | 测返回 0；与 gcc 比返回码 | 初始化屏障 |
| `cnd_destroy` | `void cnd_destroy(cnd_t *cond)` | 销毁后复用一个新 cond 仍正常 | 配套 `cnd_init` |
| `cnd_signal` | `int cnd_signal(cnd_t *cond)` | 对未等待的 cond 发信号返回 0（不崩） | 单线程可验 |
| `cnd_broadcast` | `int cnd_broadcast(cnd_t *cond)` | 同上 | — |
| `cnd_wait` | `int cnd_wait(cnd_t *cond, mtx_t *mtx)` | 需配 `thrd_create` 双线程；验证等待/唤醒逻辑确定性部分 | 跨平台实现（Win `SleepConditionVariable` / Linux `futex`） |
| `cnd_timedwait` | `int cnd_timedwait(cnd_t *cond, mtx_t *mtx, const struct timespec *ts)` | 超时返回 `thrd_timedout`（与 gcc 比返回码） | 用短超时验证超时分支 |
| `mtx_timedlock` | `int mtx_timedlock(mtx_t *mtx, const struct timespec *ts)` | 已锁情况下再 timedlock 应返回 `thrd_timedout` | 比 `mtx_lock` 多超时 |
| `thrd_detach` | `int thrd_detach(thrd_t thr)` | 创建后 detach 返回 0 | — |
| `thrd_exit` | `void thrd_exit(int res)` | 子线程内 `thrd_exit` 后 join 取到该值 | 配 `thrd_create`/`thrd_join` |
| `thrd_sleep` | `int thrd_sleep(const struct timespec *duration, struct timespec *remaining)` | 短睡眠（如 10ms）返回 0；与 gcc 比返回码 | 不可比时长，只比对返回码 |

> 线程类用例对拍要点：优先比对**返回码**和**确定性逻辑结果**（如超时返回、join 取到的退出值），不要比对执行时序/睡眠时长。

### 4.2 stdio（`<stdio.h>`）— 5 个

| 函数 | 签名 | 对拍可行性 / 测试策略 | 备注 |
|---|---|---|---|
| `vscanf` | `int vscanf(const char *fmt, va_list ap)` | 用已知输入串驱动，比对读取值与返回计数 | 底层复用 `vfscanf` |
| `vfscanf` | `int vfscanf(FILE *stream, const char *fmt, va_list ap)` | 构造 `FILE*` 输入，比对解析结果与 gcc | 可逐字节对拍（输入确定） |
| `vsscanf` | `int vsscanf(const char *s, const char *fmt, va_list ap)` | 固定输入串，比对解析结果 | 最易对拍，优先做 |
| `tmpnam` | `char *tmpnam(char *s)` | 比对"返回非空"且各次调用字符串不同；**不比对具体文件名**（路径 OS 相关） | 非确定性，只断言不变量 |
| `fopen_s` | `int fopen_s(FILE **fp, const char *path, const char *mode)` | 打开存在的/不存在的文件，比对 `fp` 结果与返回码 | MSVC 安全函数 |

### 4.3 file/dir（`<stdio.h>/<stat.h>/<dirent.h>`）— 7 个

| 函数 | 签名 | 对拍可行性 / 测试策略 | 备注 |
|---|---|---|---|
| `readdir` | `struct dirent *readdir(DIR *d)` | 在固定临时目录下列出条目，比对条目名集合 | 用 `opendir`（已测）配合 |
| `stat` | `int stat(const char *path, struct stat *buf)` | 对已知文件比对 `st_size`/`st_mode` 等可移植字段 | 部分字段 OS 相关，避开 |
| `fstat` | `int fstat(int fd, struct stat *buf)` | 对 `fileno` 后的 `FILE*` 比对 `st_size` | Windows 无 POSIX fd-open，fstat 受限 |
| `getcwd` | `char *getcwd(char *buf, size_t size)` | 比对"返回非空"且路径含仓库名片段；**不比对完整绝对路径** | OS 绝对路径不同 |
| `mkdir` | `int mkdir(const char *path, unsigned int mode)` | 建目录后 `stat` 验证存在、返回 0 | `mode` 仅作源码兼容，goc 忽略 |
| `rmdir` | `int rmdir(const char *path)` | 删除空目录，比对返回码 | 需先 `mkdir` |
| `unlink` | `long unlink(const char *path)` | 删除文件后验证消失，比对返回码 | 声明在 `file.c`，syscall 层实现 |

> file/dir 用例对拍要点：用临时目录 + 固定文件名，比对**返回码与可移植字段**（大小、存在性），避开绝对路径/权限位等 OS 相关字段。

### 4.4 math（`<math.h>`）— 3 个

| 函数 | 签名 | 对拍可行性 / 测试策略 | 备注 |
|---|---|---|---|
| `nextafter` | `double nextafter(double x, double y)` | **完全确定，逐字节对拍** | 取 `nextafter(0.0,1.0)`、`nextafter(1.0,2.0)` 等 |
| `nexttoward` | `double nexttoward(double x, double y)` | 基本确定，逐字节对拍 | **签名偏差**：标准 `long double nexttoward(double, long double)`，goc 无 long double 故两参均为 `double` |
| `remquo` | `double remquo(double x, double y, int *quo)` | 比对余数与 `*quo` 的符号/低位 | 余数可比对；`*quo` 仅低位可靠 |

> math 这 3 个是**最干净的对拍目标**，建议优先做（与 gcc 逐字节一致概率高）。

### 4.5 time（`<time.h>`）— 8 个

| 函数 | 签名 | 对拍可行性 / 测试策略 | 备注 |
|---|---|---|---|
| `gmtime` | `struct tm *gmtime(const time_t *tp)` | 比对 `asctime(gmtime(tp))` 字符串（UTC，时区无关）→ 可比对 | 指针指向共享静态区，**比格式化串不比指针** |
| `localtime` | `struct tm *localtime(const time_t *tp)` | 比对 `tm_*` 字段中时区无关部分（`tm_year/tm_mday` 等），或固定 `TZ=UTC0` 后比对字符串 | 受 `TZ` 影响；设 `TZ=UTC0` 可比对 |
| `gmtime_r` | `struct tm *gmtime_r(const time_t *tp, struct tm *result)` | 同 `gmtime` 但写入调用方 buffer | 线程安全变体 |
| `localtime_r` | `struct tm *localtime_r(const time_t *tp, struct tm *result)` | 同 `localtime`，固定 `TZ` 后比对 | — |
| `ctime` | `char *ctime(const time_t *tp)` | `ctime(tp) == asctime(localtime(tp))`，固定 `TZ=UTC0` 比对字符串 | 共享静态串 |
| `ctime_r` | `char *ctime_r(const time_t *tp, char *buf)` | 同上，写入调用方 buffer | — |
| `asctime` | `char *asctime(const struct tm *tp)` | 构造固定 `struct tm`，比对 26 字节串（如 `"Sun Jan 00 00:00:00 1900\n"`） | 完全确定，优先做 |
| `asctime_r` | `char *asctime_r(const struct tm *tp, char *buf)` | 同 `asctime`，写入调用方 buffer | — |

> time 用例对拍要点：**永远比对格式化后的字符串或可移植字段，绝不直接比对返回的 `struct tm*`/`char*` 指针地址**；`localtime`/`ctime` 系列设 `TZ=UTC0` 后可与 gcc 一致。

### 4.6 wchar（`<wchar.h>`）— 1 个

| 函数 | 签名 | 对拍可行性 / 测试策略 | 备注 |
|---|---|---|---|
| `wcscat_s` | `int wcscat_s(wchar_t *dest, size_t destsz, const wchar_t *src)` | 构造已知宽串拼接，比对结果串与返回码；含 `destsz` 不足时的约束处理错误码 | 安全函数，边界用例重要 |

---

## 5. 已排除（非覆盖缺口，非本任务范围）

| 符号 | 原因 |
|---|---|
| `open` | POSIX 底层 fd 打开；goclib 用 `FILE*`（`fopen`/`fclose`），不提供 fd 版 `open` |
| `close` | 同上，对应 `fclose` |

> 这两个不在 goclib 的 C 标准库子集内，不是"未测试"而是"未实现/设计取舍"，不计入覆盖缺口。

---

## 6. 已知实现缺陷（独立于覆盖，需修实现而非补测试）

以下不是"未测函数"，而是 goc 编译/运行层面的真实缺陷，来自 `.workbuddy/memory/MEMORY.md`：

1. **造不出 `-0.0`**：一元负号发 `0.0 - x`，应为 `xorpd` 清零符号位。
2. **`main(int argc, char **argv)` 完全没传参**：argv 未填充、argc 可能为 0。
3. **printf 大指数 `double` 十进制打印精度差**：位模式正确，十进制字符串末几位偏（见 §2）。

---

## 7. 本阶段已关闭（2026-10-06）

通过 4 个新详尽用例关闭了 16 个未覆盖导出函数，全部与 gcc 逐字节一致：

- `cstd_lib_string2.c`：`strnlen`(C23) + `stpcpy`(POSIX)
- `cstd_lib_math2.c`：`scalbn` / `scalbln`(C99)
- `cstd_lib_stdbit.c`：`stdc_bit_floor_*` / `stdc_has_single_bit_*` 各 5 宽度(C23)
- `cstd_lib_time2.c`：`timespec_get` + `tzset`

并修复根因：`src/frontend/check.go` 的 `put()` 对全局作用域 `extern` 声明 + 定义并存过度报错（tentative-definition），已放宽（仅全局作用域且类型相容时静默合并）。

---

## 8. 建议下一批顺序

1. **math（3 个）**：`nextafter/nexttoward/remquo` 确定性最强，逐字节对拍概率高，先吃。
2. **time（8 个）**：`asctime`/`asctime_r` 完全确定优先；其余固定 `TZ=UTC0` 比对字符串。
3. **stdio（5 个）**：`vsscanf` 最易对拍；`tmpnam` 只断言不变量。
4. **file/dir（7 个）**：临时目录 + 返回码/可移植字段比对。
5. **threads（10 个）**：比对返回码与确定性逻辑（超时/退出值），最后做。
6. **wchar（1 个）**：`wcscat_s` 边界用例。

每批新增用例后跑 `run_cstd_tests.ps1` 并对 `nextafter` 等确定性项直接 `diff` goc/gcc 输出；改动编译器后重跑 `bin/gocregress.exe`（基线 pass=490 fail=3，3 个失败是 Linux `c11_threads_basic` 因 ucrun 缺 clone/futex，与覆盖无关）。
