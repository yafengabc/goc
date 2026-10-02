# CSTD_STATUS.md — goc C89/C99/C11/C17 单元测试就绪度矩阵

- 套件位置：`tests\cstd\cases\`（68 个用例 .c + 1 个配套 .h）
- 对拍基准：gcc 16.2.0（MSYS2 UCRT64，`D:\msys\ucrt64\bin\gcc.exe`），逐版本 `-std=c89/c99/c11/c17`（等号形式）
- 被测对象：`D:\projects\goc\bin\goc.exe`（单模式：接受但忽略 `-std`，所有用例同一语义）
- 验证日期：2026-10-02；重跑：`powershell -ExecutionPolicy Bypass -File tests\cstd\run_cstd_tests.ps1`（当前结果 0 MISMATCH，退出码 0）
- 判定四类：**PASS** = goc 与 gcc 输出+退出码一致；**FAIL** = gcc 过而 goc 编译/运行错误（真实缺口，报错原文照录）；**UNSUPPORTED** = goc 明确设计取舍/后置；**PARTIAL** = 部分子用例通过
- 汇总：**PASS=48 PARTIAL=5 FAIL=8 UNSUPPORTED=7 MISMATCH=0**

---

## C89（36 文件：28 PASS / 3 PARTIAL / 4 FAIL / 1 UNSUPPORTED）

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
| c89_pp_obj.c | 对象宏/undef/空体/重定义/自引用/预定义宏 | PARTIAL | 7 | case1-6 一致；case7 `__STDC__/__DATE__/__TIME__` goc 全 NOT-defined（gcc 全 defined） | 避开 `__STDC__/__DATE__/__TIME__`；对象宏放心用 |
| c89_pp_func.c | 函数宏/`#`/`##`/预扫描/嵌套 | PASS | 8 | 一致（含空实参 `GLUE(,tail)`、`STR(A)` 不预扫描） | 放心用；勿在函数宏体内再嵌函数宏调用（见交叉发现） |
| c89_pp_cond.c | `#if`/`#ifdef`/defined/常量表达式/嵌套 | PASS | 6 | 一致 | 用嵌套 `#if/#else` 代替多 `#elif` 链（见 c89_pp_elif） |
| c89_pp_include.c | `<>`/`""`/guard/嵌套（+c89_pp_include_inc.h） | PASS | 4 | 一致 | 放心用；勿 `#include 宏路径` |
| c89_pp_misc.c | 块内宏/undef 后作标识符等 | PASS | 3 | 一致 | 避开 `#line`（行号差 1）与孤立 `#` |
| c89_lit_int.c | 十/十六进制、U/L/UL、`%d%u%o%x%ld%lu` | PASS | 8 | 一致 | 避开前导 0 八进制（见 c89_lit_octal） |
| c89_lit_char.c | 字符常量/基础转义/0xFF 符号性 | PASS | 4 | 一致（`(char)0xFF`→-1，char 有符号） | 避开 `\ooo`/`\xhh`/多字符 'AB' |
| c89_lit_float.c | f/L 后缀/指数/`5.` 尾点/`%f%e%g` | PASS | 8 | 一致（含 %e/%g 格式） | 避开前导点 `.5`（见 c89_lit_dotfloat），写 `0.5` |
| c89_lit_str.c | 拼接/转义/空串/sizeof/下标/数组 vs 指针 | PASS | 6 | 一致 | 放心用 |
| c89_lib_stdio.c | printf 全家/sprintf/sscanf/文件 I/O 全链路 | PASS | 7 | 一致（临时文件自删） | 放心用 |
| c89_lib_string.c | 19 个字符串函数（含 memmove 自重叠、strncpy 补零） | PASS | 10 | 一致 | 放心用 |
| c89_lib_stdlib.c | atoi/strtol/div/qsort/bsearch/atexit/rand | PARTIAL | 8 | case5 `RAND_MAX=not-defined`；其余一致（atexit LIFO 一致） | 不要引用 RAND_MAX；其余放心用 |
| c89_lib_ctype.c | 12 个 is* + tolower/toupper/EOF | PASS | 3 | 一致 | 放心用 |
| c89_lib_limits.c | limits.h + float.h + sizeof 汇总 | PARTIAL | 7 | LONG_MIN/MAX、ULONG_MAX、sizeof(long) 不同：goc long=8（LP64）vs gcc long=4（Windows LLP64）；float.h 全一致 | 勿按 long=4 假设；跨编译比对 long 值前先确认 ABI |
| c89_lib_varargs.c | va_list int/double/混合、ptrdiff_t、offsetof | PASS | 4 | 一致（offsetof 用指针算术实现） | 标准 `offsetof(type,member)` 宏别用（见交叉发现） |
| c89_trigraph.c | trigraph 探测 | UNSUPPORTED | — | goc：`parse error: line 13: expected ";", got "?"`；gcc 需 `-trigraphs` 才能编译 | 不要写 trigraph |
| c89_lit_octal.c | 八进制字面量 `010`/`0777` 等 | **FAIL**（静默错译） | 4 | **无任何报错**，输出 `010=10 0777=777`（按十进制解析）；gcc 全对（8/511） | **严禁依赖 0 前缀八进制**；用 `0x` 或显式十进制 |
| c89_pp_elif.c | 多 `#elif` 链 | **FAIL** | 4 | case2 两段 `#elif` 链真分支被跳过、落 `#else`（输出 `else`，gcc 输出 `branch-5`）；单 `#elif` 与 `#elif defined` 反而正确 | 用嵌套 `#if/#else` 代替多 `#elif` 链 |
| c89_lit_esc.c | `\ooo` 八进制与 `\xhh` 十六进制转义 | **FAIL**（硬错误） | 4 | `preprocess error: ... line 13: unterminated character literal`；gcc 编译运行、解码全对 | 避开 `\ooo`/`\xhh`；非打印字符用查表值 |
| c89_lit_dotfloat.c | 前导点浮点 `.5`/`.5f`/`.5e2`/`.5L` | **FAIL**（硬错误） | 4 | `parse error: line 12: unexpected token "." in expression`；gcc 输出 0.5/50.0/0.5 全对 | 永远写 `0.5` 不写 `.5` |

---

## C99（21 文件：12 PASS / 1 PARTIAL / 4 FAIL / 4 UNSUPPORTED）

| 文件 | 特性 | 状态 | 子用例 | goc 证据 / gcc 对拍 | 写标准库建议 |
|---|---|---|---|---|---|
| c99_comment.c | `//` 行注释 | PASS | 5 | 一致（含字符串内 `//` 不生效） | 放心用 |
| c99_longlong.c | `long long`/LL/ULL/`%lld%llu%llx`/回绕 | PASS | 5 | 一致（`LLONG_MAX+1` 回绕到 `LLONG_MIN`） | 放心用 |
| c99_bool.c | `_Bool`/stdbool.h | PASS | 4 | 一致；goc 打印 `note: skipping unavailable system header <stdbool.h>` 但 `bool/true/false/_Bool` 是内建，无需 include | 放心用，无需 include |
| c99_variadic_macro.c | `__VA_ARGS__`/宏转发 | PASS | 4 | 一致（标准变参宏，至少 1 个变参） | 放心用；勿写空尾变参 `##__VA_ARGS__`（GNU 扩展 goc 拒） |
| c99_stdint.c | stdint.h/inttypes.h | **FAIL** | 3 | `note: skipping unavailable system header <stdint.h>` + `<inttypes.h>`；`parse error: line 13: expected ";", got "i8"`；gcc 完全通过 | **避开**（goc 无 stdint.h/inttypes.h，intN_t/INT64_C/PRId64 全不可用） |
| c99_restrict.c | restrict 指针 | PASS | 2 | 一致（行为等价） | 放心用 |
| c99_inline.c | inline 函数（C99 语义） | PASS | 2 | 输出一致；goc 把 inline 一律降级为普通外部函数（gcc 需 `extern` 重声明才链接） | 注意：goc 总是外提符号，别依赖 C99 inline 仅本 TU 内联 |
| c99_compound.c | 块作用域复合字面量 | PARTIAL | 8 | case1-7 一致；**case8 `sizeof((int[]){1,2,3})` goc=0 vs gcc=12（真实 bug）** | 注意：别对复合字面量取 sizeof |
| c99_compound_file.c | 文件作用域复合字面量 | UNSUPPORTED | 3 | `type error(s): line 9: compound literal requires block scope (file-scope static literals are not supported)`；gcc 全过 | 避开文件作用域复合字面量 |
| c99_designated.c | 指定初始化器 `[i]=`/`.field=` | PASS | 4 | 一致（乱序/嵌套/重复指示符后者生效）；位置+指示符混用 goc 报 `cannot mix positional and designated ("[i] =") initialisers` | 放心用纯指示符；勿混用 |
| c99_vla.c | 变长数组 | UNSUPPORTED | 3 | `parse error: line 11: expected ";", got "n"`；gcc 全过 | 避开 VLA |
| c99_mixdecl.c | 声明与语句混排/`for(int i=)` | PASS | 4 | 一致 | 放心用 |
| c99_hexfloat.c | 十六进制浮点常量 `0x1.8p3` | PASS | 2 | 常量解析正确（`%.6f` 与 gcc 一致） | 放心用常量；**勿用 `%a` 打印**（goc 退化成字面 `a a`） |
| c99_complex.c | `<complex.h>`/`_Complex` | UNSUPPORTED | 1 | `note: skipping unavailable system header <complex.h>`；`parse error: line 12: expected ";", got "z"`；gcc(+lm) 通过 | 避开复数 |
| c99_funcname.c | `__func__` 预定义标识符 | **FAIL** | 3 | `line 9: undeclared identifier "__func__"`（三处）；gcc 打印函数名 | 避开 `__func__` |
| c99_pragma.c | `_Pragma()` 运算符 | UNSUPPORTED | 1 | `parse error: line 9: expected type specifier, got "_Pragma"`；gcc 编译期 message 正常 | 避开 `_Pragma` |
| c99_ucn.c | 通用字符名 `\uXXXX` | **FAIL**（静默错译） | 1 | 字符串内 goc 吞反斜杠：`e-acute u00e9 5` vs gcc UTF-8 `e-acute é 5`（**不报错，静默 miscompile**）；标识符内 `int \u00e9` 则 `preprocess error: unexpected character '\'` | 避开 UCN |
| c99_trailing.c | 枚举/初始化列表尾随逗号 | PASS | 3 | 一致 | 放心用 |
| c99_vacopy.c | `va_copy`（stdarg.h） | **FAIL** | 1 | `codegen error: unknown function "va_copy": not in goclib (...)`；gcc 两路独立推进=60 60 | 避开 va_copy |
| c99_math.c | C99 math 宏/函数 | PASS | 7 | 一致（round/trunc/floor/ceil/fabs/pow/sqrt/hypot/fmod、isnan/isinf/isfinite/signbit/fpclassify、nan()） | 放心用；**HUGE_VAL 未定义**（math.h 无），不要引用 |
| c99_implicit.c | C99 应拒绝的构造（隐式 int、隐式函数声明） | PASS（拒绝类） | 3 | goc 硬拒：`parse error: line 9: expected type specifier, got "x"`；gcc 16.2 在纯 `-std=c99` 下把这两类升级为**硬错误**，对拍需 `-Wno-error=implicit-int -Wno-error=implicit-function-declaration -fcommon` | goc 行为正确（拒绝），放心 |

---

## C11（9 文件：7 PASS / 2 UNSUPPORTED）

| 文件 | 特性 | 状态 | 子用例 | goc 证据 / gcc 对拍 | 写标准库建议 |
|---|---|---|---|---|---|
| c11_generic.c | `_Generic` 类型泛选 | PASS | 6 | 一致（控制表达式不求值、default、char 左值 vs `'a'` 整型提升陷阱、嵌套） | 放心用；**同一关联列表勿同时列 `int:` 与 `const int:`**（goc 报 appears twice） |
| c11_static_assert.c | `_Static_assert`/static_assert | PASS | 4 | 一致；goc 是关键字（失败用例诊断：`parse error: static_assert failed: this must fail`）；gcc 需 `#include <assert.h>` 取宏 | 放心用（goc 解析期即拒，诊断干净） |
| c11_align.c | `_Alignas`/`_Alignof`/对齐宏 | PASS | 5 | 一致（内置 alignas/alignof 可用，实测 16）；`_Alignof(struct Tag)` 与标准 `offsetof(type,m)` 为 goc 缺口，用例改用 sizeof/指针减法 | 用内置 alignas/alignof；勿用 `_Alignof(struct Tag)`、标准 offsetof 宏 |
| c11_thread_local.c | `_Thread_local`/thread_local | PASS | 5 | 一致（goc 原生 TLS：文件作用域、static 函数内、取地址、跨函数读均正常）；**块作用域非 static TLS 触发 panic**（codegen.go:4724） | 放心用；**勿用非 static 块作用域 TLS**；gcc 本机无 threads.h，用例用 `#define thread_local _Thread_local` 兜底 |
| c11_noreturn.c | `_Noreturn`/noreturn 宏 | PASS | 3 | 一致 | 放心用 |
| c11_anon.c | 匿名 struct/union 成员 | PASS | 7 | 一致（扁平访问/初始化透明/嵌套/union 重叠） | 放心用 |
| c11_uchar.c | `<uchar.h>`/char16_t/`u""`/`U""` | UNSUPPORTED | 4 | `note: skipping unavailable system header <uchar.h>`；`parse error: line 11: expected type specifier, got "char16_t"`；gcc 编译通过 | 避开 |
| c11_atomic.c | `<stdatomic.h>`/atomic_int | UNSUPPORTED | 1 | `note: skipping unavailable system header <stdatomic.h>`；`parse error: line 13: expected ";", got "a"`；裸 `_Atomic int x;`（不 include）`parse error: line 1: expected ";", got "int"` —— **`_Atomic` 不是 goc 关键字** | 避开（roadmap 后置项，语法也未接受） |
| c11_threads.c | `<threads.h>`/thrd_t 等 | PASS（兜底） | 1 | 经 `__has_include` 落到 opaque typedef，sizeof 与 gcc 一致（本机 gcc 也无 threads.h） | 避开原生 threads 库；类型可用自造 opaque 替身 |

---

## C17（2 文件：1 PASS / 1 PARTIAL-记录）

| 文件 | 特性 | 状态 | 子用例 | goc 证据 / gcc 对拍 | 写标准库建议 |
|---|---|---|---|---|---|
| c17_stdver.c | 版本宏记录 | PARTIAL（设计性 OUTPUT_DIFF） | 5 | goc：`STDC_VERSION=undefined / STDC=undefined / HOSTED=undefined / NO_ATOMICS=not-defined / NO_THREADS=not-defined`；gcc `-std=c17`：`201710 / 1 / 1 / not-defined / not-defined` | 注意：goc 三个 STDC 宏全未定义，**条件编译依赖 `__STDC_VERSION__` 的代码会落 `#else` 分支** |
| c17_smoke.c | C17 冒烟回归（_Generic/_Static_assert/_Alignas/匿名成员/thread_local 抽查） | PASS | 5 | 与 gcc 一致（C11 特性集在 goc 行为稳定） | 放心用 |

---

## 交叉发现（跨版本，写标准库前必读）

### ABI：LP64 vs Windows LLP64（最重要）
goc 是 **LP64**（`sizeof(long)=8`、指针 8、size_t 8），本机 Windows gcc 是 **LLP64**（`sizeof(long)=4`、指针 8）。凡依赖 long 宽度（`sizeof(long)`、`LONG_MIN/MAX`、`ULONG_MAX`、含 long 的 struct/union 布局）的对拍必然 DIFF，用例已主动避开或标 PARTIAL。**写可移植代码不要把 long 当 4 字节假设；跨编译器比对 long 相关 sizeof 不做断言。**

### 静默错译清单（最危险：无报错、退出码 0、输出错值）
| 缺口 | 现象 | 出处 |
|---|---|---|
| 八进制字面量 | `010` 按十进制解析得 10（应为 8），无任何报错 | c89_lit_octal.c |
| 字符串内 UCN | `"\u00e9"` 吞反斜杠输出字面 `u00e9`，不报错 | c99_ucn.c |
| `sizeof(复合字面量)` | `sizeof((int[]){1,2,3})` 恒为 0（应为 12） | c99_compound.c case8 |
| `%a` 十六进制浮点打印 | `printf("%a %a",...)` 退化成字面打印 `a a` | c99_hexfloat.c 对拍记录 |

### 硬错误缺口（gcc 过、goc 编译失败，报错原文）
- `\ooo`/`\xhh` 转义：`preprocess error: ... line 13: unterminated character literal`
- `.5` 前导点浮点：`parse error: line 12: unexpected token "." in expression`
- 多 `#elif` 链（真分支落 #else）；单 `#elif` 正确
- stdint.h/inttypes.h 缺失：`note: skipping unavailable system header` + `expected ";", got "i8"`
- `__func__`：`line 9: undeclared identifier "__func__"`
- `va_copy`：`codegen error: unknown function "va_copy": not in goclib`
- K&R 旧式定义：`parse error: line 1: expected type specifier, got "a"`
- 顶层变量 extern 重声明：`type error(s): line 2: redefinition of "x" in the same scope`（extern 只对函数声明工作）
- 标准 `offsetof(type,member)`：`unexpected token "struct"`（用 `(char*)&p.b-(char*)&p` 替代）
- `_Alignof(struct Tag)`：parse error（`_Alignof(标量/数组)` 正常）
- gets：`codegen error: unknown function "gets": not in goclib (...)`（符合 C11 移除，goc 同样无）
- 块作用域非 static `_Thread_local`：**panic**（`IsArray` nil，codegen.go:4724）——唯一会崩溃的触发点，写库时绝对避开

### UNSUPPORTED（设计取舍/后置，报错原文）
- trigraph：`parse error: line 13: expected ";", got "?"`（gcc 需 `-trigraphs`）
- 文件作用域复合字面量：`compound literal requires block scope (file-scope static literals are not supported)`
- VLA：`parse error: line 11: expected ";", got "n"`
- `_Complex`：`expected ";", got "z"`（complex.h 被跳过）
- `_Pragma`：`expected type specifier, got "_Pragma"`（整个 token 不接受）
- `<uchar.h>`：`expected type specifier, got "char16_t"`；`u""`/`U""`/`L""` 前缀被 lexer 吞空：`expected ";", got ""`
- `<stdatomic.h>`/`_Atomic`：`expected ";", got "a"`；裸 `_Atomic` 非关键字
- `<threads.h>`：被跳过；本机 gcc 亦无（用例走 `__has_include` 兜底）
- `#include 宏路径`：`malformed #include`；孤立 `#`：`unknown preprocessing directive`；`#line` 行号差 1；`__has_include` 未实现（落 #else 分支）

### 与 gcc 行为一致的高价值点（可放心依赖）
atexit LIFO、qsort/bsearch、memmove 自重叠、strncpy 补零、`%e/%g` 浮点格式、char 有符号（0xFF→-1）、算术右移、枚举尾随逗号（双方都当扩展接受）、零宽位域必须无名（`int:0;`）、复合赋值全家、函数宏 `#`/`##` 与空实参、嵌套 `#if/#else`、`__FILE__/__LINE__`、stdio/string/ctype 全函数、C99 math 宏函数、`_Generic`/`_Static_assert`/`_Alignas`/匿名成员/thread_local 行为。

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
| `__STDC_VERSION__` | **未定义** | 201710L（c89/c99/c11 分别为未定义/199901L/201112L） |
| `__STDC__` | 未定义 | 1 |
| `__STDC_HOSTED__` | 未定义 | 1 |
| `__STDC_NO_ATOMICS__` | 未定义 | 未定义（gcc 有原子） |
| `__STDC_NO_THREADS__` | 未定义 | 未定义 |
| `__DATE__` / `__TIME__` | 未定义 | defined |
| `__FILE__` / `__LINE__` | **可用** | defined |
| `__func__` | 未实现（undeclared identifier） | defined |

### 库头可用性（goc 自带）
| 类别 | 头文件 | 说明 |
|---|---|---|
| ✅ 可用（无 note） | assert ctype errno float limits math stdarg stddef stdio stdlib string tgmath time **stdckdint** | 头内标识符/函数可正常使用 |
| ⚠️ 被跳过但关键字内建 | stdbool（bool/true/false/_Bool 内建）、stdalign（alignas/alignof 内建）、stdnoreturn（noreturn 内建） | include 打 `note: skipping unavailable system header` 但不影响使用 |
| ❌ 缺失（note 后所有标识符未定义） | complex fenv inttypes iso646 locale setjmp signal **stdatomic stdint threads uchar** wchar wctype | include 不报错但头内一切未定义，等同没有 |
| 📌 特例 | stddef.h **不提供 max_align_t**（`_Alignof(max_align_t): type not known at parse time`）；stdlib.h 无 `RAND_MAX`；math.h 无 `HUGE_VAL`；stdint.h/inttypes.h 缺失（intN_t/INT64_C/PRId64 全不可用） | 写库时自造或避开 |

### 类型模型快照
int=4、long=8（LP64）、long long=8、指针=8、float=4、double=8、long double=8（降级 double，标 `__goc_long_double_is_double`）、wchar_t=8、size_t=8；char 有符号；sizeof(enum)=4（int 宽）；plain int 位域有符号。

### 给标准库作者的最终建议（放心用 / 避开速查）
- **放心用**：对象/函数宏（含 `#` `##`）、嵌套 `#if/#else`、`//` 注释、long long、`_Bool`、restrict、声明混排、十六进制浮点常量、尾随逗号、stdint 之外的全套 stdio/string/ctype/stdlib/math（含 C99 math 宏）、`_Generic`、`_Static_assert`、alignas/alignof、thread_local（静态/文件作用域）、`_Noreturn`、匿名 struct/union、stdckdint.h、`__FILE__/__LINE__`
- **避开**：八进制字面量、`\ooo`/`\xhh`、`.5` 写法、多 `#elif` 链、UCN、`__func__`、`__STDC_VERSION__/__STDC__/__DATE__/__TIME__`、`%a` 打印、`sizeof(复合字面量)`、VLA、`_Complex`、`_Pragma`、trigraph、K&R 定义、va_copy、gets、`offsetof(type,m)` 宏、`_Alignof(struct Tag)`、`_Atomic`/`<stdatomic.h>`/`<threads.h>`/`<uchar.h>`、`#include 宏路径`、`#line`、块作用域非 static TLS（panic）、stdint.h/inttypes.h
