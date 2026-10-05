# CSTD_STATUS.md — goc C89/C99/C11/C17 单元测试就绪度矩阵

- 套件位置：`tests\cstd\cases\`（69 个用例 .c + 1 个配套 .h）
- 对拍基准：gcc 16.2.0（MSYS2 UCRT64，`D:\msys\ucrt64\bin\gcc.exe`），逐版本 `-std=c89/c99/c11/c17`（等号形式）
- 被测对象：`D:\projects\goc\bin\goc.exe`（单模式：接受但忽略 `-std`，所有用例同一语义）
- 验证日期：2026-10-02；重跑：`powershell -ExecutionPolicy Bypass -File tests\cstd\run_cstd_tests.ps1`（当前结果 0 MISMATCH，退出码 0）
- 判定四类：**PASS** = goc 与 gcc 输出+退出码一致；**FAIL** = gcc 过而 goc 编译/运行错误（真实缺口，报错原文照录）；**UNSUPPORTED** = goc 明确设计取舍/后置；**PARTIAL** = 部分子用例通过
- 汇总：**PASS=67 PARTIAL=2 FAIL=0 UNSUPPORTED=7 MISMATCH=0**（2026-10-06：原 60 PASS（c89/c99/c11/c17 共 69 用例）不变；新增 `cstd_lib_*` 详尽用例 7 个全部 PASS 并接入独立 `cstd` 组——此前这些文件因不匹配版本前缀被 harness 漏跑，现已注册 per-file std 覆盖）
- 覆盖缺口跟踪：已实现但未写详尽测试的导出函数（34 个）及已知实现缺陷，详见 **`docs/goclib-coverage-gaps.md`**

---

## C89（36 文件：34 PASS / 1 PARTIAL / 0 FAIL / 1 UNSUPPORTED）

### 核心语言（16 文件，全 PASS）

| 文件 | 特性 | 状态 | 子用例 | goc 证据 / gcc 对拍 | 写标准库建议 |
|---|---|---|---|---|---|
| c89_arith.c | 整型提升、通常算术转换、无符号回绕、混合符号、负数整除截断 | PASS | 10 | 输出与 gcc 逐字节一致 | 放心用 |
| c89_types.c | char/short/int/long/float/double/指针、typedef、sizeof、char 符号性 | PASS | 6 | 一致（`sizeof(long)` 有 ABI 差异，用例已避开，见交叉发现） | 放心用；勿硬编码 long=4 |
| c89_storage.c | auto/register/static/extern/作用域遮蔽 | PASS | 5 | 一致 | 注意：变量 extern 重声明有限制（见交叉发现） |
| c89_enum.c | 枚举常量/typedef/算术/switch | PASS | 5 | 一致（含尾随逗号扩展） | 放心用 |
| c89_struct.c | 嵌套/自引用指针/按值传返 | PASS | 6 | 一致 | 放心用 |
| c89_union.c | 重叠/首成员初始化/struct 内 union | PASS | 5 | 一致（成员避开 long 防 ABI diff） | 放心用 |
| c89_bitfield.c | 符号性/宽度/无名/零宽位域 | PASS | 6 | 一致（`int a:3` 存 4→-4，有符号） | 注意：plain int 位域按有符号处理 |
| c89_init.c | 标量/数组/嵌套/字符串/部分初始化补零 | PASS | 6 | 一致（多余初始化器 goc 静默丢弃，gcc 警告但运行） | 放心用 |
| c89_logic.c | 关系/相等/`&&` `\|\|` `!` 短路 | PASS | 5 | 一致（短路不求值已证） | 放心用 |
| c89_bitops.c | `&` `\|` `^` `~`/移位 | PASS | 5 | 一致（`-8>>1=-4` 算术右移） | 注意：有符号右移为算术移位 |
| c89_assign.c | 全部复合赋值/cast/逗号 | PASS | 6 | 一致 | 放心用 |
| c89_cond.c | `?:` 结合性/sizeof 不求值 | PASS | 5 | 一致 | 放心用 |
| c89_ptr.c | `&` `*` `[]` 等价/指针算术/差/比较/退化/void* | PASS | 7 | 一致 | 放心用 |
| c89_incdec.c | 前后缀 int/指针/序列点 | PASS | 5 | 一致 | 放心用 |
| c89_control.c | if/switch 穿透/循环/break-continue/goto | PASS | 7 | 一致（fallthrough 正常） | 放心用 |
| c89_func.c | 原型/递归/main(argc,argv)/函数指针 | PASS | 7 | 一致 | 注意：K&R 旧式定义不支持（见交叉发现） |

### 预处理 / 字面量 / 标准库（20 文件）

| 文件 | 特性 | 状态 | 子用例 | goc 证据 / gcc 对拍 | 写标准库建议 |
|---|---|---|---|---|---|
| c89_pp_obj.c | 对象宏/undef/空体/重定义/自引用/预定义宏 | PASS | 7 | 一致（case7 `__STDC__/__DATE__/__TIME__` 2026-10-02 P1.7 修复后全 defined） | 对象宏放心用；`__STDC__/__DATE__/__TIME__` 可用 |
| c89_pp_func.c | 函数宏/`#`/`##`/预扫描/嵌套 | PASS | 8 | 一致（含空实参 `GLUE(,tail)`、`STR(A)` 不预扫描） | 放心用；勿在函数宏体内再嵌函数宏调用（见交叉发现） |
| c89_pp_cond.c | `#if`/`#ifdef`/defined/常量表达式/嵌套 | PASS | 6 | 一致 | 放心用；多 `#elif` 链已修复（见 c89_pp_elif） |
| c89_pp_include.c | `<>`/`""`/guard/嵌套（+c89_pp_include_inc.h） | PASS | 4 | 一致 | 放心用；勿 `#include 宏路径` |
| c89_pp_misc.c | 块内宏/undef 后作标识符等 | PASS | 3 | 一致 | 避开 `#line`（行号差 1）与孤立 `#` |
| c89_lit_int.c | 十/十六进制、U/L/UL、`%d%u%o%x%ld%lu` | PASS | 8 | 一致 | 八进制字面量放心用（已修复 2026-10-02） |
| c89_lit_char.c | 字符常量/基础转义/0xFF 符号性 | PASS | 4 | 一致（`(char)0xFF`→-1，char 有符号） | `\ooo`/`\xhh` 已修复放心用（2026-10-02）；多字符 'AB' 仍避开 |
| c89_lit_float.c | f/L 后缀/指数/`5.` 尾点/`%f%e%g` | PASS | 8 | 一致（含 %e/%g 格式） | 前导点 `.5` 已修复放心用（2026-10-02） |
| c89_lit_str.c | 拼接/转义/空串/sizeof/下标/数组 vs 指针 | PASS | 6 | 一致 | 放心用 |
| c89_lib_stdio.c | printf 全家/sprintf/sscanf/文件 I/O 全链路 | PASS | 7 | 一致（临时文件自删） | 放心用 |
| c89_lib_string.c | 19 个字符串函数（含 memmove 自重叠、strncpy 补零） | PASS | 10 | 一致 | 放心用 |
| c89_lib_stdlib.c | atoi/strtol/div/qsort/bsearch/atexit/rand | PASS | 8 | 批次H：stdlib.h 补 RAND_MAX=32767，case5 与 gcc 一致；atexit LIFO 一致 | 放心用（rand/srand/exit/atexit/RAND_MAX） |
| c89_lib_ctype.c | 12 个 is* + tolower/toupper/EOF | PASS | 3 | 一致 | 放心用 |
| c89_lib_limits.c | limits.h + float.h + sizeof 汇总 | PARTIAL | 7 | LONG_MIN/MAX、ULONG_MAX、sizeof(long) 不同：goc long=8（LP64）vs gcc long=4（Windows LLP64）；float.h 全一致 | 勿按 long=4 假设；跨编译比对 long 值前先确认 ABI |
| c89_lib_varargs.c | va_list int/double/混合、ptrdiff_t、offsetof | PASS | 4 | 一致（批次H：标准 offsetof 宏落地 stddef.h，与指针算术路径同结果） | 放心用 |
| c89_trigraph.c | trigraph 探测 | UNSUPPORTED | — | goc：`parse error: line 13: expected ";", got "?"`；gcc 需 `-trigraphs` 才能编译 | 不要写 trigraph |
| c89_lit_octal.c | 八进制字面量 `010`/`0777` 等 | PASS | 4 | 一致（`010=8 0777=511 010U=8 010L=8 010+010=16`；原静默按十进制解析已修 2026-10-02） | 八进制放心用；`0` 后接 8/9 会干净报错 |
| c89_pp_elif.c | 多 `#elif` 链 | **PASS** | 4 | 一致（2026-10-02 P0.6 修复：`#if/#elif` 条件求值期间宏展开不再受分支活性影响，case2 多链真分支正确命中） | 多 `#elif` 链放心用 |
| c89_lit_esc.c | `\ooo` 八进制与 `\xhh` 十六进制转义 | PASS | 4 | 一致（`'\101'=65 '\x41'=65 '\377'=-1 '\x7f'=127`；原 preprocess 拒绝已修 2026-10-02） | `\ooo`/`\xhh` 放心用（char 有符号，`'\377'`=-1） |
| c89_lit_dotfloat.c | 前导点浮点 `.5`/`.5f`/`.5e2`/`.5L` | PASS | 4 | 一致（`.5=0.5 .5f=0.5 .5e2=50.0 .5L=0.5`；原 parse error 已修 2026-10-02） | 前导点 `.5` 放心用 |

---

## C99（21 文件：17 PASS / 0 PARTIAL / 0 FAIL / 4 UNSUPPORTED）

| 文件 | 特性 | 状态 | 子用例 | goc 证据 / gcc 对拍 | 写标准库建议 |
|---|---|---|---|---|---|
| c99_comment.c | `//` 行注释 | PASS | 5 | 一致（含字符串内 `//` 不生效） | 放心用 |
| c99_longlong.c | `long long`/LL/ULL/`%lld%llu%llx`/回绕 | PASS | 5 | 一致（`LLONG_MAX+1` 回绕到 `LLONG_MIN`） | 放心用 |
| c99_bool.c | `_Bool`/stdbool.h | PASS | 4 | 一致（批次H：stdbool.h 落地，include 不再 skip；bool/true/false/_Bool 内建） | 放心用 |
| c99_variadic_macro.c | `__VA_ARGS__`/宏转发 | PASS | 4 | 一致（标准变参宏，至少 1 个变参） | 放心用；勿写空尾变参 `##__VA_ARGS__`（GNU 扩展 goc 拒） |
| c99_stdint.c | stdint.h/inttypes.h | PASS | 3 | 原头缺失+`expected ";", got "i8"` 已修（P1.3, 2026-10-02）：goclib 现提供 LP64 版 stdint.h/inttypes.h（int64_t=long、PRId64="ld"）；intptr_t 对拍用固定常量——裸地址运行时相关（goc 低地址加载、LLP64 gcc 高地址），无法逐字节比 | 放心用（intN_t/INT64_C/UINT64_C/PRId64/PRIu64/PRIdPTR） |
| c99_restrict.c | restrict 指针 | PASS | 2 | 一致（行为等价） | 放心用 |
| c99_inline.c | inline 函数（C99 语义） | PASS | 2 | 输出一致；goc 把 inline 一律降级为普通外部函数（gcc 需 `extern` 重声明才链接） | 注意：goc 总是外提符号，别依赖 C99 inline 仅本 TU 内联 |
| c99_compound.c | 块作用域复合字面量 | PASS | 8 | case1-8 全部与 gcc 一致；**case8 `sizeof((int[]){1,2,3})`=12（P0.5 已修复 2026-10-02，原恒 0）** | 放心用（含 sizeof 复合字面量） |
| c99_compound_file.c | 文件作用域复合字面量 | UNSUPPORTED | 3 | `type error(s): line 9: compound literal requires block scope (file-scope static literals are not supported)`；gcc 全过 | 避开文件作用域复合字面量 |
| c99_designated.c | 指定初始化器 `[i]=`/`.field=` | PASS | 4 | 一致（乱序/嵌套/重复指示符后者生效）；位置+指示符混用 goc 报 `cannot mix positional and designated ("[i] =") initialisers` | 放心用纯指示符；勿混用 |
| c99_vla.c | 变长数组 | UNSUPPORTED | 3 | `parse error: line 11: expected ";", got "n"`；gcc 全过 | 避开 VLA |
| c99_mixdecl.c | 声明与语句混排/`for(int i=)` | PASS | 4 | 一致 | 放心用 |
| c99_hexfloat.c | 十六进制浮点常量 `0x1.8p3` + `%a` 打印 | PASS | 6 | 常量解析正确；`%a/%A` 已修（P0.7, 2026-10-02）：精确位型、显式精度舍入、`%#a`、inf/极端值逐字节同 gcc（-0.0/NaN 常量因 goc 常量折叠丢符号位而避开） | 放心用常量与 `%a` 打印 |
| c99_complex.c | `<complex.h>`/`_Complex` | UNSUPPORTED | 1 | `note: skipping unavailable system header <complex.h>`；`parse error: line 12: expected ";", got "z"`；gcc(+lm) 通过 | 避开复数 |
| c99_funcname.c | `__func__` 预定义标识符 | PASS | 3 | 原三处 undeclared 已修（P1.4, 2026-10-02）：每函数体首注入 `static const char __func__[]`，三函数均打印自身名，与 gcc 一致 | 放心用 `__func__` |
| c99_pragma.c | `_Pragma()` 运算符 | UNSUPPORTED | 1 | `parse error: line 9: expected type specifier, got "_Pragma"`；gcc 编译期 message 正常 | 避开 `_Pragma` |
| c99_ucn.c | 通用字符名 `\uXXXX` | PASS | 1 | 字符串内一致：`e-acute é 5`（UTF-8 解码；原吞反斜杠静默错译已修 2026-10-02）；标识符内 UCN 仍未支持（另一独立缺口） | 字符串/字符字面量 UCN 放心用；标识符内 UCN 仍避开 |
| c99_trailing.c | 枚举/初始化列表尾随逗号 | PASS | 3 | 一致 | 放心用 |
| c99_vacopy.c | `va_copy`（stdarg.h） | PASS | 1 | 原 codegen unknown function 已修（P1.5, 2026-10-02）：stdarg.h 补 `#define va_copy(d,s) ((d)=(s))`（va_list=char*），两路独立推进=60 60 与 gcc 一致 | 放心用 va_copy |
| c99_math.c | C99 math 宏/函数 | PASS | 7 | 一致（round/trunc/floor/ceil/fabs/pow/sqrt/hypot/fmod、isnan/isinf/isfinite/signbit/fpclassify、nan()） | 放心用；HUGE_VAL/HUGE_VALF/HUGE_VALL 已提供（批次H，1.0/0.0 运行时 IEEE 除零得 inf） |
| c99_implicit.c | C99 应拒绝的构造（隐式 int、隐式函数声明） | PASS（拒绝类） | 3 | goc 硬拒：`parse error: line 9: expected type specifier, got "x"`；gcc 16.2 在纯 `-std=c99` 下把这两类升级为**硬错误**，对拍需 `-Wno-error=implicit-int -Wno-error=implicit-function-declaration -fcommon` | goc 行为正确（拒绝），放心 |

---

## C11（10 文件：8 PASS / 2 UNSUPPORTED）

| 文件 | 特性 | 状态 | 子用例 | goc 证据 / gcc 对拍 | 写标准库建议 |
|---|---|---|---|---|---|
| c11_generic.c | `_Generic` 类型泛选 | PASS | 6 | 一致（控制表达式不求值、default、char 左值 vs `'a'` 整型提升陷阱、嵌套） | 放心用；**同一关联列表勿同时列 `int:` 与 `const int:`**（goc 报 appears twice） |
| c11_static_assert.c | `_Static_assert`/static_assert | PASS | 4 | 一致；goc 是关键字（失败用例诊断：`parse error: static_assert failed: this must fail`）；gcc 需 `#include <assert.h>` 取宏 | 放心用（goc 解析期即拒，诊断干净） |
| c11_align.c | `_Alignas`/`_Alignof`/对齐宏 | PASS | 5 | 一致（内置 alignas/alignof 可用，实测 16）；`_Alignof(struct Tag)` 仍为缺口（用例改用 sizeof/指针减法）；标准 offsetof 宏批次H 已提供 | 用内置 alignas/alignof；勿用 `_Alignof(struct Tag)` |
| c11_thread_local.c | `_Thread_local`/thread_local | PASS | 5 | 一致（goc 原生 TLS：文件作用域、static 函数内、取地址、跨函数读均正常）；块作用域非 static TLS 现被干净拒绝（P0.2），不再 panic | 放心用；块作用域须 `static _Thread_local`；gcc 本机无 threads.h，用例用 `#define thread_local _Thread_local` 兜底 |
| c11_thread_local_bad.c | 块作用域非 static `_Thread_local`（负向） | PASS（拒绝类） | 3 | goc 干净拒：`type error(s): line N: _Thread_local variable "x" at block scope must be static or extern`（P0.2，原 codegen.go:4785 IsArray nil panic 已消除）；gcc -std=c11 同样拒（`function-scope 'x' implicitly auto and declared '_Thread_local'`） | 双方一致拒绝；块作用域写 TLS 必须加 static |
| c11_noreturn.c | `_Noreturn`/noreturn 宏 | PASS | 3 | 一致 | 放心用 |
| c11_anon.c | 匿名 struct/union 成员 | PASS | 7 | 一致（扁平访问/初始化透明/嵌套/union 重叠） | 放心用 |
| c11_uchar.c | `<uchar.h>`/char16_t/`u""`/`U""` | UNSUPPORTED | 4 | 批次H：<uchar.h> 已提供，char16_t/char32_t 与转换函数可用；仍拒 `u""`/`U""`/u 前缀宽字面量（`parse error: line 11: expected ';' after global declaration`）；gcc 编译通过 | 宽字面量避开；类型/转换函数放心用 |
| c11_atomic.c | `<stdatomic.h>`/atomic_int | UNSUPPORTED | 1 | `note: skipping unavailable system header <stdatomic.h>`；`parse error: line 13: expected ";", got "a"`；裸 `_Atomic int x;`（不 include）`parse error: line 1: expected ";", got "int"` —— **`_Atomic` 不是 goc 关键字** | 避开（roadmap 后置项，语法也未接受） |
| c11_threads.c | `<threads.h>`/thrd_t 等 | PASS（兜底） | 1 | 批次H：goc 提供 threads.h（同构 opaque 类型，sizeof 与兜底一致）；调用 thrd_*/mtx_* 函数仍为 clean unknown-function（goc 单线程）；本机 gcc 无此头 | 类型声明可用；勿调用 threads 函数 |

---

## C17（2 文件：1 PASS / 1 PARTIAL-记录）

| 文件 | 特性 | 状态 | 子用例 | goc 证据 / gcc 对拍 | 写标准库建议 |
|---|---|---|---|---|---|
| c17_stdver.c | 版本宏记录 | PARTIAL（设计性 OUTPUT_DIFF） | 5 | goc：`STDC_VERSION=202311 / STDC=1 / HOSTED=1 / NO_ATOMICS=not-defined / NO_THREADS=not-defined`；gcc `-std=c17`：`201710 / 1 / 1 / not-defined / not-defined`（P1.7 后 STDC 宏已定义；版本值差异为设计性记录，goc 单模式报 C23） | 条件编译依赖 `__STDC_VERSION__` 的代码现可工作（goc 报 202311） |
| c17_smoke.c | C17 冒烟回归（_Generic/_Static_assert/_Alignas/匿名成员/thread_local 抽查） | PASS | 5 | 与 gcc 一致（C11 特性集在 goc 行为稳定） | 放心用 |

---

## C-stdlib 详尽用例（cstd_lib_*，7 文件全 PASS）

> 这些文件不走版本前缀，由 harness 的 `cstd` 组（默认 `-std=c2x`，per-file `std` 覆盖）单独跑。
> 目的是对 goclib 的「高阶/跨版本」函数做逐字节对拍，覆盖标准版本用例未触及的导出面。

| 文件 | 特性 | 状态 | 子用例 | gcc 对拍 / 备注 |
|---|---|---|---|---|
| cstd_lib_stdio2.c | printf 标志 `+/-/#/.*s/%ls`、sscanf 返回值语义、tmpfile 自删 | PASS | 多 | gcc `-std=c99` 逐字节一致 |
| cstd_lib_wchar.c | wcsspn/wcstok/wcscmp/wmem*、<wchar.h> wint_t/WEOF | PASS | 多 | gcc `-std=c99` 一致 |
| cstd_lib_stdlib2.c | strtod 十六进制浮点、strtol/strtoul ERANGE、qsort/bsearch | PASS | 多 | gcc `-std=c99` 一致 |
| cstd_lib_string2.c | strnlen（C23 上限 n）、stpcpy（POSIX 返回 NUL 指针） | PASS | 3 | gcc `-std=c2x -DSTUB_STPCPY`：mingw 不导 stpcpy 符号，gcc 侧用测试内参考实现对拍 goc 真 stpcpy |
| cstd_lib_math2.c | scalbn(x,int)/scalbln(x,long)（C99 7.12.6.13/14） | PASS | 3 | gcc `-std=c2x` 一致（避开大指数浮点打印精度差） |
| cstd_lib_stdbit.c | stdc_bit_floor_*/stdc_has_single_bit_* 各 5 宽度（C23 <stdbit.h>） | PASS | 多 | gcc 无 <stdbit.h>，用测试内参考实现 cross-check；goclib 的 bit_floor 本就正确 |
| cstd_lib_time2.c | timespec_get（返回 TIME_UTC）、tzset（不崩、tzname[] 非空） | PASS | 3 | gcc `-std=c2x` 一致；tzname[0] 拼写 OS 相关、timespec_get 返墙钟，仅断言确定性不变量 |

---

## 交叉发现（跨版本，写标准库前必读）

### ABI：LP64 vs Windows LLP64（最重要）
goc 是 **LP64**（`sizeof(long)=8`、指针 8、size_t 8），本机 Windows gcc 是 **LLP64**（`sizeof(long)=4`、指针 8）。凡依赖 long 宽度（`sizeof(long)`、`LONG_MIN/MAX`、`ULONG_MAX`、含 long 的 struct/union 布局）的对拍必然 DIFF，用例已主动避开或标 PARTIAL。**写可移植代码不要把 long 当 4 字节假设；跨编译器比对 long 相关 sizeof 不做断言。**

### 静默错译清单（最危险：无报错、退出码 0、输出错值）
| 缺口 | 现象 | 出处 |
|---|---|---|
| ~~八进制字面量~~ **已修复(2026-10-02)** | 原 `010` 按十进制解析得 10；现正确为 8 | c89_lit_octal.c |
| ~~字符串内 UCN~~ **已修复(2026-10-02)** | 原 `"\u00e9"` 吞反斜杠输出字面 `u00e9`；现正确解码 UTF-8 | c99_ucn.c |
| ~~`sizeof(复合字面量)`~~ **已修复(2026-10-02, P0.5)** | 原 `sizeof((int[]){1,2,3})` 恒为 0；现正确为 12 | c99_compound.c case8 |
| ~~`%a` 十六进制浮点打印~~ **已修复(2026-10-02, P0.7)** | 原 `printf("%a %a",...)` 退化成字面打印 `a a`；现精确位型十六进制浮点，与 gcc 逐字节一致 | c99_hexfloat.c 对拍记录 |

### 硬错误缺口（gcc 过、goc 编译失败，报错原文）
- ~~`\ooo`/`\xhh` 转义~~ **已修复(2026-10-02)**：原 `preprocess error: ... unterminated character literal`，现正确解码
- ~~`.5` 前导点浮点~~ **已修复(2026-10-02)**：原 `parse error: unexpected token "."`，现正确解析
- ~~多 `#elif` 链~~ **已修复(2026-10-02, P0.6)**：`#if/#elif` 条件求值期间宏展开不再受分支活性影响，真分支正确命中
- ~~stdint.h/inttypes.h 缺失~~ **已修复(2026-10-02, P1.3)**：原 `note: skipping unavailable system header` + `expected ";", got "i8"`，现 goclib 提供两头
- ~~`__func__`~~ **已修复(2026-10-02, P1.4)**：原 `line 9: undeclared identifier "__func__"`，现函数体内注入预定义标识符
- ~~`va_copy`~~ **已修复(2026-10-02, P1.5)**：原 `codegen error: unknown function "va_copy": not in goclib`，现 stdarg.h 宏展开
- K&R 旧式定义：`parse error: line 1: expected type specifier, got "a"`
- 顶层变量 extern 重声明：`type error(s): line 2: redefinition of "x" in the same scope`（extern 只对函数声明工作）
- 标准 `offsetof(type,member)`：批次H 已修复（stddef.h 落地 `((size_t)&(((type*)0)->member))`，对拍与 gcc 一致）
- `_Alignof(struct Tag)`：parse error（`_Alignof(标量/数组)` 正常）
- gets：`codegen error: unknown function "gets": not in goclib (...)`（符合 C11 移除，goc 同样无）
- 块作用域非 static `_Thread_local`：**已修复（P0.2，2026-10-02）**——现被语义层干净拒绝（`_Thread_local variable "x" at block scope must be static or extern`，exit 非 0），原 codegen.go:4785 IsArray nil panic 路径不可达；gcc 同样拒绝

### UNSUPPORTED（设计取舍/后置，报错原文）
- trigraph：`parse error: line 13: expected ";", got "?"`（gcc 需 `-trigraphs`）
- 文件作用域复合字面量：`compound literal requires block scope (file-scope static literals are not supported)`
- VLA：`parse error: line 11: expected ";", got "n"`
- `_Complex`：`expected ";", got "z"`（complex.h 被跳过）
- `_Pragma`：`expected type specifier, got "_Pragma"`（整个 token 不接受）
- `<uchar.h>`：`expected type specifier, got "char16_t"`；`u""`/`U""`/`L""` 前缀被 lexer 吞空：`expected ";", got ""`
- `<stdatomic.h>`/`_Atomic`：`expected ";", got "a"`；裸 `_Atomic` 非关键字
- `<threads.h>`：被跳过；本机 gcc 亦无（用例走 `__has_include` 兜底）
- `#include 宏路径`：`malformed #include`；孤立 `#`：`unknown preprocessing directive`；`#line` 行号差 1

### 与 gcc 行为一致的高价值点（可放心依赖）
atexit LIFO、qsort/bsearch、memmove 自重叠、strncpy 补零、`%e/%g/%a` 浮点格式、char 有符号（0xFF→-1）、算术右移、枚举尾随逗号（双方都当扩展接受）、零宽位域必须无名（`int:0;`）、复合赋值全家、函数宏 `#`/`##` 与空实参、嵌套 `#if/#else`、`__FILE__/__LINE__`、stdio/string/ctype 全函数、C99 math 宏函数、`_Generic`/`_Static_assert`/`_Alignas`/匿名成员/thread_local 行为。

### 记录类
- 多字符常量 `'AB'`：goc `preprocess error: unterminated character literal`（gcc 得 0x4142=16706）
- `#error`：goc `preprocess error: <file>:2: #error`（丢弃引号内消息文本），gcc 保留消息；两边都正确拒绝
- `#pragma` 未知指令：goc 静默忽略（与 gcc 一致）
- register 取地址：goc 接受并运行（标准要求拒绝，gcc 硬报错）——goc 比标准宽松
- 多余初始化器：goc 静默丢弃（gcc 警告）
- `##__VA_ARGS__`（GNU 空变参扩展）：gcc 接受、goc 拒绝 `unexpected token ")"`；标准变参宏（≥1 实参）正常
- 不同定义的宏重定义：goc 静默接受（gcc 仅 warning）
- 函数宏体内再嵌函数宏调用不被重扫描（直接转发链 `OUTER(x)→INNER(x)→((x)+1)` 正常）

---

## 附：`__STDC_VERSION__` 实测与库头可用性一览（2026-10-02 实测）

### 预定义宏/标识符
| 宏 | goc | gcc -std=c17 |
|---|---|---|
| `__STDC_VERSION__` | **202311**（2026-10-02 P1.7 修复；单模式报 C23） | 201710L（c89/c99/c11 分别为未定义/199901L/201112L） |
| `__STDC__` | 1（2026-10-02 P1.7 修复） | 1 |
| `__STDC_HOSTED__` | 1（2026-10-02 P1.7 修复） | 1 |
| `__STDC_NO_ATOMICS__` | 未定义 | 未定义（gcc 有原子） |
| `__STDC_NO_THREADS__` | 未定义 | 未定义 |
| `__DATE__` / `__TIME__` | defined（2026-10-02 P1.7 修复：`"Mmm dd yyyy"`/`"hh:mm:ss"`） | defined |
| `__FILE__` / `__LINE__` | **可用** | defined |
| `__func__` | 未实现（undeclared identifier） | defined |

### 库头可用性（goc 自带）
| 类别 | 头文件 | 说明 |
|---|---|---|
| ✅ 可用（无 note） | assert ctype errno float limits math stdarg stddef stdio stdlib string tgmath time **stdckdint stdbool stdnoreturn stdint inttypes uchar threads** | 头内标识符/函数可正常使用（stdbool/stdnoreturn/uchar/threads 为批次H 新提供） |
| ⚠️ 被跳过但关键字内建 | stdalign（alignas/alignof 内建） | include 打 `note: skipping unavailable system header` 但不影响使用 |
| ❌ 缺失（note 后所有标识符未定义） | complex fenv iso646 locale setjmp signal **stdatomic** wchar wctype | include 不报错但头内一切未定义，等同没有（stdint/inttypes 于批次F、threads/uchar 于批次H 提供） |
| 📌 特例 | （批次H 已清空：stddef.h 提供 offsetof/max_align_t，stdlib.h 提供 RAND_MAX，math.h 提供 HUGE_VAL 系） | 原三项缺口均已落地，直接使用 |

### 类型模型快照
int=4、long=8（LP64）、long long=8、指针=8、float=4、double=8、long double=8（降级 double，标 `__goc_long_double_is_double`）、wchar_t=8、size_t=8；char 有符号；sizeof(enum)=4（int 宽）；plain int 位域有符号。

### 给标准库作者的最终建议（放心用 / 避开速查）
- **放心用**：对象/函数宏（含 `#` `##`）、嵌套 `#if/#else`、`//` 注释、long long、`_Bool`、restrict、声明混排、十六进制浮点常量、尾随逗号、全套 stdio/string/ctype/stdlib/math（含 C99 math 宏/RAND_MAX/HUGE_VAL）、stdint/inttypes、stdbool/stdnoreturn/uchar/threads 类型、offsetof、max_align_t、`_Generic`、`_Static_assert`、alignas/alignof、thread_local（静态/文件作用域）、`_Noreturn`、匿名 struct/union、stdckdint.h、`__FILE__/__LINE__`
- **避开**：VLA、`_Complex`、`_Pragma`、trigraph、K&R 定义、gets、`_Alignof(struct Tag)`、`_Atomic`/`<stdatomic.h>`、`u""`/`U""` 宽字面量、threads 函数调用（thrd_*/mtx_*）、`#include 宏路径`、`#line`、块作用域非 static TLS（编译期拒绝，须加 static）（多 `#elif` 链、`__STDC_VERSION__/__STDC__/__DATE__/__TIME__`、`sizeof(复合字面量)`（P0.5）、字符串字面量直接下标（P0.8）、`__func__`（P1.4）、`%a` 打印（P0.7）、va_copy（P1.5）、stdint.h/inttypes.h（P1.3）、`offsetof(type,m)`、RAND_MAX、HUGE_VAL 均已于 2026-10-02 修复，移出避开清单）
