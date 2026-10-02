# Group F — 标准库关联（stdckdint / stdbit / uchar / stddef / string / stdlib / limits / float）

> 对拍引擎：`tests\c23\_tools\check_case.ps1`，gcc = `D:\msys\ucrt64\bin\gcc.exe -std=c2x -Wall -Wextra` (16.2.0 MSYS2 UCRT64)。
> goc = `D:\projects\goc\bin\goc.exe`（2026-10-02 版本）。语义判定由本负责人定稿。
> 本组是"写标准库最关心"的一组：结论直接决定 goclib 能否用现代 C23 写法实现。

| 文件 | 特性 | 子用例数 | 判定 | goc 证据（报错原文/关键输出 + goclib 头文件声明摘录） | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|---|
| c23_ckd.c | <stdckdint.h> ckd_add/sub/mul | 12 | **PARTIAL** | goc SUMMARY **9/12**（gcc 12/12）。case1-8（int/uint/混合 int+long）全对。失败：case9 `ll add overflow flag=0`（应 1，result=-9223372036854775808）、case11 `ull add overflow flag=0`（应 1，result=0）、case12 `ll mul overflow flag=0`（应 1，result=-2）。回绕 result 值两侧逐字节一致，仅 64 位目标的溢出**标志位**恒报 0。<br>**goclib/stdckdint.h 摘录**：`#define ckd_add(r,a,b) ((*(r)=(a)+(b)), __ckd_oflow((long long)(a)+(long long)(b), sizeof(*(r)), r))`；`__ckd_oflow_signed(S,W) ((W)>=8 ? 0 : ...)`——W≥8（64 位）直接返回"无溢出"。头注释自证："64-bit overflow cannot be detected without 128-bit math and is reported as non-overflowing." | 12/12，逐行一致 | **int/unsigned int 能放心用**（32 位目标溢出检测正确）；**long/long long/unsigned long long 的 ckd_* 要避开**——恒报"无溢出"，不能用于 64 位安全检查 |
| c23_stdbit.c | <stdbit.h> | 3 | **UNSUPPORTED**（机械 PASS） | 两侧均无 <stdbit.h>。goc 走守卫 else 分支，与 gcc 逐行一致输出 `real <stdbit.h> unavailable on this toolchain`；本地 shim（bit_width/count_ones）两侧 3/3 一致。goc 不打印 skip note（守卫拦截，未真正 include）。 | gcc 也无此头，守卫探针输出逐行一致 3/3 | **要避开**：<stdbit.h> 双侧均缺；位运算需手写（shim 已验证逻辑可行） |
| c23_uchar.c | <uchar.h> char16_t/char32_t/mbrtoc16 等 | 4 | **UNSUPPORTED** | goc stderr 原文：<br>`...c23_uchar.c: note: skipping unavailable system header <uchar.h>`<br>`...c23_uchar.c: note: skipping unavailable system header <wchar.h>`<br>`parse error: line 26: expected ";", got "h"`（line 26 = `char16_t h = 0x41;`）。exit 1。 | gcc 4/4：sizeof(char16_t)=2、char32_t=4，mbrtoc16/c16rtomb/mbrtoc32/c32rtomb ASCII 往返全对 | **要避开**：<uchar.h> 与 char16_t/char32_t 及转换函数完全不可用 |
| c23_stddef.c | <stddef.h> nullptr_t/unreachable/NULL/offsetof/size_t | 7 | **PASS（功能）+ 3 处信息探针 DIFF** | goc 7/7 exit 0。功能核心一致：sizeof(size_t)=8、ptrdiff_t=8、NULL、nullptr→指针、`if(0){unreachable();}` 编译通过。**DIFF**：case3 `sizeof(typeof(nullptr))=4`（gcc=8）；case5 `_Generic(nullptr)=int`（gcc=distinct）；case7 `offsetof not defined`（gcc=4）。<br>**goclib/stddef.h 摘录**：`typedef void* nullptr_t;`、`#define unreachable() ((void)0)`；**无 offsetof、无 max_align_t**（头注释明写 "offsetof is deliberately not provided"）。 | gcc 7/7；mingw stddef.h **不提供 `unreachable()` 宏**（用 `__builtin_unreachable`），有 max_align_t(=32)、offsetof(=4)。nullptr 在 gcc 是独立类型（非 void*） | **NULL/size_t/unreachable(死分支) 能放心用**；**要避开**：offsetof 未提供、max_align_t 未提供、勿假设 `typeof(nullptr)` 为指针尺寸（goc 当 int 0，size 4） |
| c23_strdup.c | <string.h> strdup/strndup | 5 | **PASS** | goc 5/5 exit 0，无 stderr。strdup 正常/空串、strndup n<len/n>len/n=0 全对，free 正常。<br>**goclib/string.h 摘录**：`char *strdup(const char *s);`、`char *strndup(const char *s, size_t n);`（在 string.h）。**goclib/stdlib.h 摘录：无 strdup/strndup 声明**（符合 C23 把它们放到 string.h）。 | gcc 5/5 逐行一致（仅一条 `-Wstringop-overread` 无害警告：strndup("hi",10)） | **能放心用**（从 <string.h> 取，勿从 stdlib.h） |
| c23_memccpy.c | <string.h> memccpy；<stdlib.h> qsort/bsearch const 比较器 | 6 | **PASS** | goc 6/6 exit 0，无 stderr。memccpy 找到('X',off=4)/未找到(Z,返回NULL)/c='\0'(off=3)/n 过短(返回NULL) 全对；const-correct 比较器 `int (*)(const void*,const void*)` qsort 升序、bsearch 命中全对。<br>**goclib/string.h**：`void *memccpy(void *dest, const void *src, int c, size_t n);`<br>**goclib/stdlib.h**：`void qsort(void*, size_t, size_t, int (*cmp)(const void*, const void*));` / `void *bsearch(...)`（已是 C23 const 签名）。 | gcc 6/6 逐行一致 | **能放心用**（memccpy、qsort、bsearch 及 C23 const 比较器签名） |
| c23_limits_float.c | limits.h/float.h C23 宏 + long double 降级 | 探针（21 宏 + 3 long double 项） | **PASS（探针编译运行）+ 逐宏 DIFF** | goc：**全部 C23 宏 undef**（CHAR_WIDTH…ULLONG_WIDTH、BOOL_WIDTH、BITINT_MAXWIDTH、FLT_NORM_MAX、DBL_NORM_MAX、LDBL_NORM_MAX、FLT/DBL/LDBL_IS_IEC_60559 全 undef）。long double：`__goc_long_double_is_double` **undef**、`LDBL_MANT_DIG=53`、`LDBL_MAX_EXP undef`、`sizeof(long double)=8`、`1.0L+2.0L==3.0L =>1`（降级 double 可运算）。<br>**goclib/limits.h 摘录**：仅 CHAR_BIT=8 + C89/C99 *_MIN/_MAX（**无** *_WIDTH，**无 ULLONG_MAX**）。<br>**goclib/float.h 摘录**：仅 FLT_*/DBL_* + `#define LDBL_MANT_DIG DBL_MANT_DIG` 等别名（**无** FLT_NORM_MAX/FLT_IS_IEC_60559/LDBL_MAX_EXP）。 | gcc：全部 C23 宏齐全（CHAR_WIDTH=8…LLONG_WIDTH=64、BOOL_WIDTH=1、BITINT_MAXWIDTH=65535、*_NORM_MAX defined、*_IS_IEC_60559=1）；LDBL_MANT_DIG=64、LDBL_MAX_EXP=16384、sizeof(long double)=16（80 位扩展）。**注意 gcc `LONG_WIDTH=32`**（Windows LLP64 ABI，非 goc 缺陷） | **要避开**所有 C23 宏探针（goc 一个都没有）；long double 当 double 用可运算，但勿依赖 `__goc_long_double_is_double` 标记（未定义）或 LDBL_MAX_EXP（未定义） |

## 组内发现摘要

### 1. 各头"可用性结论"（写标准库直接照此决策）
- **<stdckdint.h>**：半可用。32 位整型（int/unsigned int）的 ckd_add/sub/mul 溢出检测正确，与 gcc 一致；**64 位目标（long/long long/unsigned long long）的溢出检测是死代码**——goc 用 `(long long)` 强转计算、且 `__ckd_oflow` 对 W≥8 直接返回 0，恒报"无溢出"。写标准库时只用 ckd_* 检查 int/unsigned 范围；64 位溢出要自行实现（需 128 位或区间判断）。
- **<stdbit.h>**：双侧工具链均缺。不可用，位运算手写。
- **<uchar.h>**：goc 缺头（连 <wchar.h> 一并 skip），char16_t/char32_t 与 mbrtoc16/c16rtomb/mbrtoc32/c32rtomb 全部不可用。mingw 的 uchar.h 本身也**没有 mbrtoc8/char8_t**（已读 `D:\msys\ucrt64\include\uchar.h` 确认，故本套未测该子用例）。
- **<stddef.h>**：基础可用。NULL、size_t/ptrdiff_t、unreachable()（死分支安全）、nullptr 关键字赋值/比较均与 gcc 一致。**缺口**：offsetof 未提供、max_align_t 未提供；且 goc 把 nullptr 建模为整数 0（`typeof(nullptr)` 是 4 字节 int，_Generic 命中 `int`），gcc 是独立的 8 字节 null 类型——勿在 _Generic/typeof 里依赖 nullptr 的指针语义。
- **<string.h>（strdup/strndup/memccpy）**：完全可用，PASS。strdup/strndup 在 goc 中正确位于 <string.h>（stdlib.h 未重复声明，符合 C23）。
- **<stdlib.h>（qsort/bsearch const 签名）**：完全可用。goc 已采用 C23 的 `int (*)(const void*, const void*)` const-correct 比较器原型，实测排序/查找正确。
- **<limits.h>/<float.h> C23 宏**：goc 一个都没有（所有 *_WIDTH、FLT_NORM_MAX、*_IS_IEC_60559、BITINT_MAXWIDTH、BOOL_WIDTH 全 undef）。勿写依赖这些宏的可移植代码。
- **long double 降级**：goc 把 long double 折叠为 double（sizeof=8、LDBL_MANT_DIG=53、运算正常），**但 `__goc_long_double_is_double` 标记宏并未定义**，且 LDBL_MAX_EXP 未定义。roadmap 说"标此宏"与实测不符。

### 2. 与 roadmap 的偏差（需知会组织者）
- roadmap 称 stdckdint.h "已支持"——**实测仅对 32 位目标成立**；64 位目标溢出检测是已知缺口（头文件注释自证），建议 roadmap 加注。
- roadmap 称 `<uchar.h>`/`<stdbit.h>` 后置——**实测成立**（缺头，UNSUPPORTED）。
- roadmap 称 long double 降级"标 `__goc_long_double_is_double`"——**降级本身成立（LDBL_*=DBL_*、sizeof=8），但标记宏未定义**。
- roadmap 未提 offsetof/max_align_t——实测 goc stddef.h 两者都没有（offsetof 头注释明确"deliberately not provided"）。

### 3. 交叉点与复用提示
- 指针打印：goc 的 `%p` 格式（`0x...`）与 gcc（裸 hex）不同且地址随 ASLR 变化，**对拍文件不要 `%p`**——本组 c23_memccpy.c 已改为只打印相对布尔量，供后续组复用。
- 信息探针模式：对两侧必然分叉的项（offsetof、typeof(nullptr)、宏存在性）采用"打印 + 恒 passed++"，保证 SUMMARY 一致、分叉写进 status——与 A2 组 c23_nullptr.c 的处理一致。
- `unreachable()`：mingw gcc 的 stddef.h **不提供** `unreachable()` 宏（只有 `__builtin_unreachable`），对拍文件必须用 `#if defined(unreachable)` / `#elif defined(__builtin_unreachable)` 双分支，否则 gcc 侧编译失败（BROKEN_TEST）。
- gcc 的 Windows ABI：`long`/`LONG_WIDTH` 在 mingw 为 32 位（LLP64），goc 的 long 为 64 位——这是平台 ABI 差异，不是 goc 缺陷，对拍时勿据此判 FAIL。
