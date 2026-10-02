# C23 特性就绪度状态矩阵（goc tests\c23）

> 测试套件：`D:\projects\goc\tests\c23\`（cases 66 个用例文件 + 一键重跑脚本 `run_c23_tests.ps1` + 对拍引擎 `_tools\check_case.ps1`）
> 对拍基线：`D:\projects\goc\bin\goc.exe`（2026-10-02 版本）vs `D:\msys\ucrt64\bin\gcc.exe -std=c2x -Wall -Wextra`（GCC 16.2.0，MSYS2 UCRT64，Windows LLP64）
> 判定四类：**PASS**（goc 与 gcc 行为一致）/ **FAIL**（gcc 通过而 goc 错误 = 真实缺口，附报错原文）/ **UNSUPPORTED**（goc 明确设计取舍）/ **PARTIAL**（部分子用例通过，含两侧输出不同但都能跑的 DIFF 情形）
> 全量机械判定：49 PASS / 6 PARTIAL / 6 FAIL / 5 DIFF / 1 UNSUPPORTED（2026-10-02：批次E c23_version/c23_has_c_attribute 转 PASS；批次D 新增 c23_str_subscript.c PASS；批次H c23_uchar 转 PASS（uchar.h/uchar.c 落地）；详见下方分组表；机械判定与语义判定差异处已注明）
> 所有用例均在 gcc -std=c2x 下编译运行通过（负向用例为双方拒绝），源代码 LF/UTF-8 无 BOM/纯 ASCII。
> 2026-10-02 更新：P0 _BitInt 修复落地（src\codegen.go、src\goclib\bitint.c/h），`c23_bitint.c` 由 PARTIAL(12/15) 转 **PASS(15/15)**，新增 `c23_bitint_edge.c`（PASS 7/7，覆盖裸字面量比较/全局与 static 非零初始化/嵌套同宽 cast/6 参 conv 栈通道）；go test 全绿，C89-C17 套件 0 MISMATCH。
> 2026-10-02 更新：P0.2 块作用域非 static `_Thread_local` panic 修复落地（src\check.go 存储类约束：块作用域 + 非 static/extern 直接干净拒绝），新增 `c23_thread_local_bad.c`（EXPECT: REJECT，双方拒机械 PASS）；原 codegen.go:4785 IsArray nil panic 路径不可达。

## 0. 对写标准库的核心结论（先看这个）

### 能放心用（PASS，可直接采用）
- **基础类型**：`bool/true/false`（勿 include <stdbool.h>，goc 无此头）、`nullptr` 判空、`typeof/typeof_unqual`（实参限类型名或标量变量）、`auto` 推导、`_BitInt(N)` **全宽度**（1..1024 位，≤64 位回绕、跨宽度符号扩展、嵌套 cast、裸字面量比较均与 gcc 逐位一致）
- **存储/聚合**：文件作用域 `thread_local` 与函数内 `static thread_local`、单层匿名 struct/union、无参 `f()` ≡ `f(void)`、普通整数位域（含纯 int 位域按有符号，与 gcc 一致）
- **预处理**：`#embed`（基本 + limit + prefix/suffix/if_empty）、`#elifdef/#elifndef`、`#warning`、`__has_include`（直接调用）、`__VA_OPT__`、空参宏、宏展开 30 层嵌套、`#if` 常量表达式（含 0b、intmax 回绕）
- **字面量**：0b 二进制、数字分隔符（合法位置）、带 p 指数的十六进制浮点、u8 字符串值/大小/拼接、自动存储期空 `{}` 初始化（关键处用 `{0}` 更稳）
- **属性**：`[[noreturn]]`、函数级 `[[nodiscard]]`、`[[maybe_unused]]`、`[[deprecated]]`、switch 内 `[[fallthrough]]`、`[[unsequenced]]/[[reproducible]]`、声明/变量/函数位置属性、`_Noreturn` 关键字
- **C23 移除项**：隐式 int / 隐式函数声明 / 无参 f() 带参调用 / K&R 定义——goc 与 gcc 一致拒绝（goc 无 C89 兼容后门）
- **标准库**：`strdup/strndup`（在 <string.h>）、`memccpy`、`qsort/bsearch`（C23 const 比较器签名）、32 位目标的 `ckd_add/sub/mul`、`NULL/size_t/unreachable()`（死分支）
- **复合字面量**（块作用域）、**constexpr 数组与空指针**、**`static_assert(条件, "消息")`**（必须带消息）

### 要避开（FAIL / PARTIAL / UNSUPPORTED，写标准库不得依赖）
| 类别 | 特性 | 原因（goc 行为） |
|---|---|---|
| 编译器直接报错 | **VLA** | `int a[n]`/VLA 形参全部解析拒绝，且不定义 `__STDC_NO_VLA__`（conformance 缺口） |
| 编译器直接报错 | **constexpr 当编译期常量** | 不能作数组尺寸/case 标签/_Static_assert 条件/位域宽度（只能当运行时值）；且不强制初始化器为常量（`constexpr int bad = glob;` 被接受） |
| 编译器直接报错 | **属性在类型位置** | `struct [[nodiscard]] T`、参数上属性、typedef 尾属性全部 parse error |
| 编译器直接报错 | **_Atomic / <stdatomic.h>** | `_Atomic int` 连语法都不解析（roadmap #129 "语法接受"不成立）；头缺失 |
| 编译器直接报错 | **块作用域 constexpr**、`static T = {}`、无消息 `static_assert(e)`、结构体内 static_assert、`__typeof__`/嵌套 typeof、`typeof(&x)` | 均解析拒绝 |
| 值错误 | **双层嵌套匿名成员** | 夹具名字段的两级匿名字段错位（`a=11` vs gcc `a=10`）；位域成员不可花括号初始化 |
| 值错误 | **alignas 对齐成员/数组** | 标量变量真对齐，但结构体成员/字符数组/alignas(64) 局部数组实际未对齐（偏移 8/8/48） |
| 值错误 | **enum E:T 布局** | 语法与枚举值对，但 sizeof(enum) 恒为 4（`enum:uchar`/`enum:llong` 都 4，gcc 为 1/8） |
| 值错误 | **空 {} 初始化首标量** | 已有局部后第一个 `{}` 标量不真正归零（读到稳定垃圾 71302960）；`static int s={}` 被拒 |
| 值错误 | **ckd_* 64 位目标** | 溢出标志恒报 0（goclib 头自证：无 128 位数学，W≥8 直接判无溢出） |
| 值错误 | **_Generic 区分 long/unsigned** | LLP64 上 `1L`/`1U` 误配 int 分支（宽度相同即折叠）；typedef 名关联与 `const int*` vs `int*` 也不可靠 |
| 值错误 | **u8 字面量类型** | u8 字符串按 `char*` 衰减、`sizeof(u8'A')=8`（应 1）；char8_t 类型名 goc 内建可用但语义仍 C11 模型 |
| 值错误 | **`[[gnu::aligned(N)]]`** | 解析但不生效（_Alignof 仍自然对齐 4）；未知/vendor 属性静默忽略 |
| 语义偏差 | **`[[fallthrough]]` switch 外** | goc 静默接受（gcc 硬错误） |
| 语义偏差 | **非法数字分隔符** | goc 静默删 `'` 照常解析（`1''000`/`0x'FFFF'` 等 6 种全不报错）；十六进制浮点指数内分隔符报 "unterminated character literal"（`0x1p10'0`，归 P2.13）；前导小数点 `.12`/`.1'2` 已支持（2026-10-02 lexer 前导点修复） |
| 语义偏差（已修） | **`__has_c_attribute` guard** | **已修复（P2.15，2026-10-02）**：`defined(__has_c_attribute)`/`defined(__has_include)` 现返回 1，标准 portable guard 正常激活（与 gcc 一致） |
| 语义偏差 | **nullptr_t 类型模型** | goc `typedef void* nullptr_t`、`typeof(nullptr)` 为 4 字节 int（gcc 为独立 8 字节类型）；gcc 侧 mingw 的 <stddef.h> 也不暴露 nullptr_t 类型名 |
| 缺失头/宏 | **<stdbit.h>**、**<stdatomic.h>** | goc 无此二头（"note: skipping unavailable system header"），mingw 侧 <stdbit.h> 也无；<uchar.h>/<threads.h>/<stdnoreturn.h>/<stdbool.h> 已于批次H提供 |
| 缺失宏 | （批次H 已提供） | limits.h/float.h C23 宽度/归一化宏全套已落地（CHAR_WIDTH…ULLONG_WIDTH/BOOL_WIDTH/BITINT_MAXWIDTH/FLT_NORM_MAX/*_IS_IEC_60559/EXP 系）；LONG_WIDTH=64 为 goc LP64 语义（gcc LLP64 报 32），LDBL_* 按 long double=double 降级值 |
| 缺失设施 | （批次H 已提供） | stddef.h 现提供标准 offsetof 宏与 max_align_t（goc 8 字节对齐 vs gcc 16——long double 模型差异） |
| 平台细节 | **printf %a / %wN** | goc 无 %a（打印字面 'a'）、无 %wN；`"hello"[0]` 直接下标已修复（P0.8，2026-10-02，见 c23_str_subscript.c PASS 4/4） |
| 编译器崩溃（已修） | **块作用域非 static thread_local** | **已修复（P0.2，2026-10-02）**：现干净拒绝 `_Thread_local variable "x" at block scope must be static or extern`（exit 非 0），不再 Go panic；gcc 同样拒绝；合法形式须 `static thread_local` |

### 需要上报 goc 的缺陷清单（按优先级）
1. ~~`_BitInt` ≤64 位不做宽度掩码（赋值/运算不回绕）~~ ——**已修复**（2026-10-02：8 处转换落点改 `from_i64_trunc`/`conv` 按目标宽度回绕 + 算术/复合赋值就地回绕）
2. ~~`_BitInt` 与无类型整数字面量比较触发编译器 panic~~ ——**已修复**（比较分支 nil 类型兜底 `IntType()`）；另修复：全局/static 非零初始化被静默丢弃（bigInitWords 剥 cast 链+回绕+负值全宽扩展）、goa dq 拒 16 位十六进制、callBigLib 5+ 参栈通道、同宽 big->big cast 缺失 copyBytes、返回 _BitInt 的函数调用误判 struct
3. ~~块作用域非 static `thread_local` 触发编译器 panic~~ ——**已修复**（2026-10-02：src\check.go 语义层拒绝块作用域 + 非 static/extern 的 `_Thread_local` 组合，codegen panic 路径不可达；负向用例 c23_thread_local_bad.c 双方拒绝）
4. 空 `{}` 初始化：首个标量不归零（codegen bug）、static 存储期被拒——P1
5. `_Generic` 在 LLP64 把 long/unsigned 折叠为 int（genericTypeMatch 宽度相等时丢符号/类型）——P2
6. ~~不定义 `__STDC_VERSION__`/`__STDC__`/`__STDC_NO_VLA__` 等任何预定义宏~~ ——**已修复**（P1.7，2026-10-02：注入 `__STDC__`=1、`__STDC_HOSTED__`=1、`__STDC_VERSION__`=202311、`__DATE__`=`"Mmm dd yyyy"`、`__TIME__`=`"hh:mm:ss"`；`__STDC_NO_VLA__` 仍缺，随 P2.3 VLA 决策一并处理）
7. ~~`__has_c_attribute`/`__has_include` 对 `defined()` 不可见~~ ——**已修复**（P2.15，2026-10-02：cePrimary defined 分支对 `__has_*` 特判返回 1）
8. alignas 不作用于结构体成员/数组——P2
9. ~~字符串字面量直接下标返回垃圾值~~ ——**已修复**（P0.8，2026-10-02：codegen elemWidthOf/elemSignedOf 将字符串字面量按 char[] 处理，`"hello"[0]` 现返回 104 与 gcc 一致；新增 c23_str_subscript.c PASS）
10. long double 降级已生效，但 `__goc_long_double_is_double` 标记宏未定义（与 roadmap 不符）——P3
11. `enum E:T` 布局忽略（sizeof 恒 4）、`__goc_long_double_is_double`、`offsetof` 缺失——P2/P3

---

## A. 关键字与类型

| 特性 | 状态 | 子用例 | goc 证据 | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|
| bool/true/false 关键字 | **PASS** | 7 | 全过；stderr 仅 `note: skipping unavailable system header <stdbool.h>`（goc 无此头，关键字本身可用） | 7/7，输出逐行一致 | 放心用；勿 `#include <stdbool.h>` |
| nullptr / nullptr_t | **PARTIAL** | 9 | 值/比较/传参全过；`sizeof(nullptr)=8`；`_Generic(nullptr)` 落 int；goclib 为 `typedef void* nullptr_t` | 9/9；gcc 中 nullptr 是独立 8 字节类型，但此 mingw 构建经 <stddef.h> 不暴露 nullptr_t 类型名 | 判空放心用；勿假设 nullptr_t 独立类型、勿用 _Generic 判 nullptr 类型 |
| alignas/alignof | **PARTIAL** | 7 | goc 4/7：标量变量真对齐（alignas(16) 偏移 0）；`alignas(32) char[64]` 全局偏移 **8**、结构体成员偏移 **8**、`alignas(64) char[128]` 局部偏移 **48**——声明了但没真对齐 | 7/7 全对齐 | 标量变量可用；**别用 alignas 对齐成员/数组/SIMD/缓存行** |
| constexpr 对象（文件作用域） | **PASS（附缺口）** | 8 | 文件作用域标量/指针/数组/`static constexpr` 折叠正确；**拒**块作用域 constexpr（`parse error: expected ";", got "int"`）与 constexpr 数组界（`expected ";", got "N"`） | 8/8 全支持 | 文件作用域放心用；勿在函数内写、勿作数组维 |
| constexpr 非常量初始化（负向） | **FAIL（偏差）** | 1 | goc **接受** `constexpr int bad = glob;`（exit 0 无诊断） | gcc 拒：`initializer element is not constant` | 别指望 constexpr 做编译期校验（goc 不强制常量） |
| thread_local / _Thread_local | **PASS** | 7 | 文件作用域 + 函数内 static TLS 读写/sizeof/取址全对（goa 原生 TLS 实证） | 7/7 逐行一致 | 放心用（合法形式）；**块内必须 `static thread_local`**（非 static 现被干净拒绝，P0.2）；单线程局限待 threads.h |
| 块作用域非 static thread_local（负向） | **PASS（双方拒）** | 3 构造 | goc 干净拒：`type error(s): line N: _Thread_local variable "x" at block scope must be static or extern`（P0.2） | gcc -std=c2x 拒：`function-scope 'x' implicitly auto and declared '_Thread_local'` | 非法写法；块作用域 TLS 必须 static/extern |
| typeof / typeof_unqual | **PASS（附缺口）** | 7 | typeof(类型/标量变量/常量) 与 typeof_unqual(const/volatile) 可用；**拒** `__typeof__`（`parse error`）、`typeof(&x)`（`only typeof(type), typeof(var) and typeof(constant) are supported`）、嵌套 typeof；`typeof(2.0)` 得 0.0（bug） | 7/7 全支持 | 放心用，实参限类型名/标量变量；勿用旧拼写/嵌套/地址表达式 |
| auto 类型推导 | **PASS** | 7 | int/指针/数组衰减/函数指针/const/static/for-init/文件作用域全过 | 7/7（gcc 对 `static auto` 仅警告声明序） | 放心用 |
| auto 无初始化器（负向） | **PASS（双方拒）** | 1 | `type error(s): line 11: auto declaration of "x" requires an initialiser` | `error: 'auto' requires an initialized data declaration` | 一致拒绝 |
| enum E:T 底层类型 | **PARTIAL** | 5 | 语法解析 + 枚举常量值正确（9000000000LL/4000000000U）；但 `sizeof(enum)` **恒为 4**（`enum:uchar`→4、`enum:llong`→4） | 5/5，sizeof 按底层类型（1/8） | 枚举值可用；**别用于紧凑存储/大范围枚举**（布局被忽略） |
| static_assert 两形式 | **PASS（附缺口）** | 4 | `static_assert(e,"msg")`/`_Static_assert(e,"msg")` 文件与块作用域可用；**拒**无消息形式 `static_assert(e)`（`expected "," after static_assert condition`）与结构体内形式 | 4/4 全支持 | 放心用但**必须带消息**；勿写无消息形式、勿放 struct 体内 |
| static_assert(0) 假条件（负向） | **PASS（双方拒）** | 1 | `parse error: static_assert failed: this condition is always false` | `error: static assertion failed` | 一致拒绝 |
| 匿名 struct/union 成员 | **PARTIAL** | 7 | 单层（扁平访问/union 重叠/指示符/箭头/sizeof）全对；**双层嵌套匿名错位**（`a=11` vs gcc `a=10`）；位域成员不可花括号初始化（`cannot brace-initialise bit-field member "lo"`） | 7/7 | 单层放心用；**双层嵌套避开**；位域用逐字段赋值 |
| _BitInt(N) 大整数 | **PASS** | 15 | 全部宽度（1/8/17/31/32/64/65/127/128/256/1024）回绕/算术/移位/跨宽度符号扩展/除法/嵌套同宽 cast 与 gcc 逐位一致；`sizeof(_BitInt(1..32))=8`（gcc 1/2/4，ABI 差异，非缺陷） | 15/15 逐行一致 | **全宽度放心用**（2026-10-02 P0 修复后）；≤64 位当窄整型用回绕正确；比较可直接写裸字面量 |
| _BitInt 边界用例（edge） | **PASS** | 7 | 裸字面量比较（S8/U64）、文件作用域/static 非零初始化（含嵌套 cast、负值全宽扩展）、全局算术、嵌套同宽 cast 的声明/赋值/传参/返回、窄化 6 参 conv 全部与 gcc 一致（`cases\c23_bitint_edge.c`，2026-10-02 新增） | 7/7 逐行一致 | 覆盖本次 P0 修复的全部触发点，写标准库回归必跑 |
| u8 字面量类型身份（char8_t） | **PARTIAL（DIFF）** | 5 | u8 字符串按 `char*` 衰减（C11 模型）；`sizeof(u8'A')=8`（异常，应 1）；goc 内建 char8_t 类型名（无需 uchar.h） | gcc：u8"abc"=unsigned char*、u8'A'=unsigned char、sizeof=1；不 include <uchar.h> 时不暴露 char8_t 名 | 别按 char8_t 类型身份判断；u8 字符串当普通 char* 用 |
| 无参 f() == f(void) | **PASS** | 5 | 声明等价/重声明兼容/函数指针类型同一，全对 | 5/5 逐行一致 | 放心用 |
| __STDC_VERSION__ 等预定义宏 | **PASS** | 5 | 2026-10-02 P1.7 修复：`__STDC_VERSION__`=202311、`__STDC__`=1、`__STDC_HOSTED__`=1，与 gcc -std=c23 逐行一致（SUMMARY 3/3）；`__DATE__`=`"Oct  2 2026"`、`__TIME__`=`"hh:mm:ss"` | 一致 | **条件编译恢复**：可 `#if __STDC_VERSION__ >= 202311L` 判断 C23；`__DATE__`/`__TIME__` 可用 |
| _Atomic / <stdatomic.h>（探针） | **UNSUPPORTED** | 1 | `note: skipping unavailable system header <stdatomic.h>`；`parse error: line 15: expected ";", got "int"`（`_Atomic int` 连语法都不解析——roadmap #129 "语法接受"**不成立**） | gcc 干净编译运行 | 原子类型/操作完全不可用，需绕开 |

## B. 预处理

| 特性 | 状态 | 子用例 | goc 证据 | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|
| #embed 基本 | **PASS** | 4 | 字节数组/sizeof/多段拼接与 gcc 逐字节一致 | 一致 | 放心用（分隔逗号须单独成行，紧跟文件名的逗号会被当选项） |
| #embed limit() | **PASS** | 4 | limit(0/2/4/10 钳长) 全部一致 | 一致 | 放心用 |
| #embed prefix/suffix/if_empty/__has_embed | **PARTIAL** | 5 | prefix/suffix/if_empty 与 gcc 一致（roadmap "后置未确认"**不准确，实际已支持**）；`__has_embed` 未提供（`avail=0`） | 5/5 | prefix/suffix/if_empty 放心用；**__has_embed 不能用**（guard 必走 else） |
| #elifdef / #elifndef | **PASS** | 5 | 链式/混用/嵌套/#else 回退全一致 | 一致 | 放心用 |
| #warning | **PASS** | 1 | 仅警告不阻断：`c23_warning.c:11: warning: c23_warning probe: ...` | gcc：`warning: #warning "..." [-Wcpp]` | 放心用（goc 警告格式更简） |
| __has_include | **PASS** | 6 | 直接调用正确反映有无该头（stdio/string=1、缺失/threads=0，与 gcc 一致）；`defined(__has_include)` 2026-10-02 P2.15 修复后返回 1 | 6/6 | 直接 `#if __has_include(<x>)` 可用；portable guard 写法亦可用 |
| __has_c_attribute | **PASS** | 7 | 2026-10-02 P2.15 修复：`defined(__has_c_attribute)`=1，portable guard 激活；SUMMARY 7/7 与 gcc 逐行一致（deprecated/nodiscard/noreturn/maybe_unused/fallthrough=1，likely/unlikely=0） | guard_active=1，7/7 | 标准 portable guard 放心用 |
| __VA_OPT__ | **PASS** | 5 | 空变参逗号省略/非空保留/多参/嵌套转发/与 ## 组合全一致 | 一致 | 放心用 |
| #define F() 空参宏 / F(...) | **PASS** | 5 | 空参空展开/有体零参/变参空调用全一致 | 一致 | 放心用（避免空展开后 `= +常量` 写法） |
| 宏展开嵌套/递归边界 | **PASS** | 6 | 自引用不递归（#if 折叠 0）/30 层嵌套/# 串化/## 空操作数边界全一致 | 一致 | 放心用 |
| #if 常量表达式 | **PASS** | 8 | 算术/位/逻辑/三目/字符常量/defined()/十六进制/intmax 回绕与 gcc 判定一致（gcc 的 overflow 警告 goc 静默） | 一致 | 放心用 |
| #if 1/0（负向） | **PASS（双方拒）** | 1 | `preprocess error: ...:11: division by zero in #if expression` | `error: division by zero in #if` | 一致拒绝 |

## C. 属性 [[...]]

| 特性 | 状态 | 子用例 | goc 证据 | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|
| [[noreturn]] / _Noreturn | **PASS** | 4 | 声明/定义/混合拼写/取地址全接受，stderr 空 | 4/4 干净 | 放心用 |
| [[nodiscard]] 函数级 | **PASS** | 5 | 丢弃返回值警告：`return value of function should not be discarded`（声明处一次）；`(void)` 抑制生效 | gcc 按使用点警告并带 reason | 函数级放心用（诊断在声明处、不带 reason 文本） |
| [[nodiscard]] 类型/枚举级 | **FAIL** | 3 | `parse error: line 13: anonymous struct/union requires a body`（`struct [[nodiscard]] Node`）；typedef 尾属性 `expected declarator name, token "["` | gcc 编译运行 + 类型丢弃警告 | **类型级属性避开**（goc 不能解析） |
| [[maybe_unused]] | **PASS** | 5 | 接受语法，且 goc **不产生任何 unused 诊断**（属性实际是 no-op） | gcc 对裸未用变量/函数警告 | 可用（实际 no-op） |
| [[deprecated]] / [[deprecated("msg")]] | **PASS** | 4 | 每实体声明处警告一次：`declaration is deprecated`；**丢弃自定义消息文本**；`&fn` 不重复警告 | 按使用点警告并带消息 | 语法放心用；别依赖消息文本传递 |
| [[fallthrough]]（switch 内） | **PASS** | 3 | 接受 `[[fallthrough]];` 空语句；goc 无隐式穿透诊断 | gcc 对隐式穿透/末 case 分别警告 | switch 内放心用（goc 不校验穿透正确性） |
| [[fallthrough]] switch 外（负向） | **FAIL（偏差）** | 1 | goc **静默接受**（exit 0 无诊断） | gcc 硬错误：`invalid use of attribute 'fallthrough'` | 别依赖 goc 校验属性位置 |
| [[unsequenced]] / [[reproducible]] | **PASS** | 3 | 接受前缀写法 + 与 nodiscard 叠加；无运行期语义（编译期提示） | gcc 支持但提示放 `)` 后 | 语法放心用（gcc 侧建议 post-`)` 写法） |
| 未知属性忽略 / [[gnu::...]] | **PARTIAL（DIFF）** | 3 | `[[foo]]`/`[[gnu::unused]]` 静默接受；`[[gnu::aligned(16)]]` 解析但**不生效**（`_Alignof=4` vs gcc 16） | gcc：`'foo' attribute ignored` 警告；aligned 生效 | 未知/vendor 属性在 goc 是惰性注解；**别依赖 gnu::aligned 真对齐** |
| 属性位置变体 | **PASS** | 5 | 声明开头/定义/变量/堆叠/存储类前全接受；参数上属性被拒（`expected type specifier, token "["`） | 同位置接受 | 声明/变量/函数位置放心用；参数位置避开 |
| [[using gnu: ...]]（属性作用域） | 双方未实现 | — | goc `expected type specifier, token ";"` | gcc `expected ']' before 'gnu'` | 避开（非 goc 独有缺口） |

## D. 表达式 / 字面量 / 语义

| 特性 | 状态 | 子用例 | goc 证据 | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|
| 二进制字面量 0b/0B | **PASS** | 11 | 值/后缀（u/UL）/与 0xFF 对照/混算/`#if 0b1100==12` 全一致 | 一致 | 放心用（含 #if 中） |
| 数字分隔符（合法位置） | **PASS** | 12 | 1'000'000/0xFF'FF/0b1010'1010/1.2'34e5/.12/.1'2/1'000e3/3.14'15f/1'000UL/4'2L 全一致（SUMMARY 12/12）。`.12`/`.1'2` 前导点子用例已加回（2026-10-02 lexer 前导点修复）；`0x1p10'0`（hex 浮点指数内分隔符）仍 `preprocess error: unterminated character literal`，归 P2.13 | 一致 | 合法位置放心用；前导点 `.12` 亦支持；仅 `0x1p10'0` 指数内分隔符避开 |
| 非法数字分隔符（负向） | **FAIL（goc 过宽）** | 6 构造 | goc **静默删 `'` 照常解析**（`1''000`/`0x'FFFF'`/`1'.2`/`1.'5`/`0x1'p0`/`123'` 全部接受并运行，exit 66661，无诊断） | gcc 全部拒绝（adjacent/after base/adjacent to point/exponent 各报错） | 别指望 goc 揪出分隔符笔误，写法自检 |
| 十六进制浮点 | **PASS** | 10 | 0x1.8p3/0x.8p1/0x1p-2/f/L 后缀/== 比较与 gcc 一致。**%a 已支持**（P0.7, 2026-10-02，与 gcc 逐字节一致）；**无指数形式 0x1.8 goc 接受（=1.5）但本 gcc 拒**（`require an exponent`，C23 新特性 gcc 16.2 未实现） | 一致（用 %.3f/==/%a 对拍） | 带 p 指数放心用；%a 放心用；0x1.8 是 goc 私有扩展，移植 gcc 不过 |
| u8 字符串/字符值语义 | **PASS** | 8 | sizeof(u8"abc")=4、拼接 u8"ab""cd"、u8'x'=120、尾部 NUL 全一致 | 一致 | 值/大小/拼接/单字节字符放心用（原始串与多字节字符本 gcc c2x 也不接受，未纳入） |
| 字符串字面量直接下标 | **PASS** | 4 | "hello"[0]='h'(104)、[1]='e'(101)、与 const char* 控制组一致、0xE4 字节按有符号 char 得 -28（SUMMARY 4/4） | 一致（104/101/104/-28） | 放心用；字符串字面量可直接下标（P0.8 修复 2026-10-02，原垃圾值） |
| 空 {} 初始化 | **PASS（附两坑）** | 9 | int/指针/double/数组/struct/union/嵌套/块内 `{}` 归零一致；**坑1**：已有局部后第一个 `{}` 标量不归零（稳定垃圾 71302960）；**坑2**：`static int s={}` → `codegen error: invalid braced initialiser for scalar type int` | 9/9 全归零 | 自动存储期基本可用；**关键位置用 `{0}`**；static 用 `= {0}` |
| 复合字面量（块作用域） | **PASS** | 10 | 取地址/数组退化/循环内每轮重初始化/struct/union/指示符/按值传参全一致。**两个缺口移除**：文件作用域 → `type error: compound literal requires block scope`；`(const int){}` → `type error: compound literal is const-qualified` | 一致 | 块作用域放心用；勿写文件作用域 `&(T){...}`、勿用 const 限定字面量 |
| _Generic | **PARTIAL** | 10 | goc 5/10：int 匹配/不求值（x++ 不生效）/char 与 short 不提升/default/宏全对；**`1L` 与 `1U` 误配 int 分支**（LLP64 宽度相同即折叠）、typedef 名关联不命中、`const int*` vs `int*` 报 `type int* appears twice`（gcc 可区分） | 10/10 | int/char/short 分支与"不求值"可靠；**别用 _Generic 区分 long/unsigned 与 int** |
| constexpr 编译期使用（数组尺寸/case/_Static_assert/位域宽） | **FAIL** | 7 | 整文件拒：`parse error: line 20: expected integer constant, got "N"`；case 标签/局部与全局数组尺寸/位域宽度全部 `expected integer constant` 类拒绝；**只能当运行时值** | 7/7 全折叠 | **用 enum 或字面量宏代替 constexpr 编译期常量** |
| constexpr 指针/数组 | **PASS（附备注）** | 5 | constexpr 数组 + 空指针与 gcc 一致；`constexpr int *p=&obj` 语法过但 `*p` 运行时崩溃 0xC0000005（gcc 侧也拒非空指针：`'constexpr' pointer initializer is not null`）；地址比较 _Static_assert 被拒 | 一致 | constexpr 数组/空指针放心用；勿用 constexpr 指真实对象地址 |
| VLA 与变修改类型 | **FAIL** | 5 | 整文件拒：`parse error: line 21: expected integer constant, got n`（VLA 形参即拒）；**不定义 `__STDC_NO_VLA__`**（未声明取舍即 conformance 缺口） | 5/5（运行时尺寸/sizeof 运行时求值/多维/VLA 形参/static 数组参数） | **完全不可用**；可变长用定长上界或 malloc |
| 位域（普通整型） | **PASS** | 6 | 宽度 1..3/signed=unsigned 区分/纯 int 位域按有符号（与 gcc 一致）/零宽/无名/volatile/sizeof 布局全一致。**`_BitInt(7) x:5` 位域被拒**：`parse error: bit-field base type must be an integer type, got _BitInt(7)` | 一致（gcc 接受 _BitInt 位域） | 普通位域放心用；_BitInt 位域避开 |

## E. 废弃 / 移除与语义变更

| 特性 | 状态 | 子用例 | goc 证据 | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|
| K&R 函数定义（C23 移除） | **PASS（GOC-REJECT）** | 1 构造 | goc **拒绝**：`parse error: line 9: expected type specifier, got "a"`（前端已无 K&R 语法） | gcc 仅警告仍通过：`-Wold-style-definition`（exit 0） | 一律不用；全用原型 `int f(int a, int b)` |
| _Noreturn / noreturn（C23 保留，弃用） | **PASS** | 3 | 接受 `_Noreturn` 与裸 `noreturn` 拼写；`<stdnoreturn.h>` 缺失（`note: skipping unavailable system header`），宏不存在 | 干净接受，无弃用警告 | 用 `_Noreturn` 或 `[[noreturn]]`；勿 include <stdnoreturn.h> 依赖宏 |
| 无参 f() 带参调用（负向） | **PASS（双方拒）** | 1 构造 | `type error(s): call to "f": expected 0 arguments, got 2` | `error: too many arguments to function 'f'; expected 0, have 2` | 一致拒绝；写 `f(void)` |
| 隐式 int（负向） | **PASS（双方拒）** | 1 构造 | `parse error: line 9: expected type specifier, got "foo"`（**未保留 C89 隐式 int**） | `error: return type defaults to 'int'` | 函数必须显式返回类型 |
| 隐式函数声明（负向） | **PASS（双方拒）** | 1 构造 | codegen 层拒绝：`codegen error: unknown function "g": not in goclib (...)`（无 gcc 式 implicit declaration 诊断，报错偏晚） | `error: implicit declaration of function 'g'` | 调用前先声明/包含头文件 |

## F. 标准库关联

| 特性 | 状态 | 子用例 | goc 证据（含 goclib 头摘录） | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|
| <stdckdint.h> ckd_add/sub/mul | **PARTIAL** | 12 | goc 9/12：int/uint/混合 32 位目标溢出检测全对；**64 位目标（long long/unsigned long long）溢出标志恒报 0**（goclib 头自证：`__ckd_oflow((long long)(a)+(long long)(b), sizeof(*(r)), r)` 且 `(W)>=8 ? 0 : ...`——"64-bit overflow cannot be detected without 128-bit math"）；回绕结果值两侧一致 | 12/12 | **32 位放心用**；**64 位溢出检查避开**（自实现 128 位或区间判断） |
| <stdbit.h> | **UNSUPPORTED** | 3 | 双侧均无此头，守卫探针输出一致 `unavailable`；本地 shim（stdc_bit_width/count_ones）两侧 3/3 一致 | gcc 也无此头 | 避开；位运算手写（shim 已验证语义可行） |
| <uchar.h> char16/char32/mbrtoc16 系 | **PASS** | 4 | 批次H：src\goclib\uchar.h/uchar.c（go:embed 内嵌）实现 C11 7.28 四函数（含 mbstate_t 代理半字状态机，非 BMP 走 -3/-1 协议），4/4 与 gcc 逐字节一致；char8_t/mbrtoc8 双方均缺（mingw 无） | gcc 4/4：sizeof=2/4，mbrtoc16/c16rtomb/mbrtoc32/c32rtomb ASCII 往返全对 | char16_t/char32_t 与 UTF-16/32 往返放心用；u""/U"" 字面量仍避开 |
| <stddef.h> nullptr_t/unreachable/NULL/offsetof | **PASS（附 2 信息分叉）** | 7 | 功能核心 7/7；批次H 提供 offsetof（=4 与 gcc 一致）与 max_align_t（goc 8/8 vs gcc 16/32，long double 模型差异）；剩余分叉：`typeof(nullptr)`=4 字节 int（gcc 8 字节独立类型）、`_Generic(nullptr)` 命中 int | gcc：mingw 无 unreachable() 宏（用 __builtin_unreachable），有 offsetof=4、max_align_t=32 | NULL/size_t/unreachable(死分支)/offsetof/max_align_t 放心用；勿依赖 nullptr 的指针类型语义 |
| <string.h> strdup/strndup | **PASS** | 5 | 正常/空串/strndup n<len/n>len/n=0/free 全对；goclib 声明在 string.h（stdlib.h 未重复，符合 C23） | 5/5 一致（一条无害 -Wstringop-overread） | 放心用（从 <string.h> 取） |
| <string.h> memccpy + <stdlib.h> qsort/bsearch const 签名 | **PASS** | 6 | memccpy 找到/未找到/c='\0'/n 过短全对；goclib qsort/bsearch 已用 C23 `int (*)(const void*, const void*)` 签名，排序/查找正确 | 6/6 一致 | 放心用 |
| limits.h/float.h C23 宏 + long double | **PASS（探针）+ 逐宏 DIFF** | 21 宏+3 项 | 批次H：全部 C23 宽度/归一化宏落地（CHAR_WIDTH…ULLONG_WIDTH、BOOL_WIDTH=1、BITINT_MAXWIDTH=65535、FLT/DBL/LDBL_NORM_MAX、FLT/DBL/LDBL_IS_IEC_60559=1、FLT_RADIX/ROUNDS/EXP 全套），`__goc_long_double_is_double` 标记已补；剩余 DIFF 全为模型差异：LONG_WIDTH/ULONG_WIDTH=64（goc LP64 vs gcc 32）、LDBL_MANT_DIG=53/LDBL_MAX_EXP=1024/sizeof=8（long double 降级 double，gcc 64/16384/16）、标记宏 goc defined vs gcc undef | gcc 全宏齐全（LONG_WIDTH=32 是 Windows LLP64 ABI，非缺陷）；LDBL_MANT_DIG=64、sizeof=16 | 宽度/归一化宏可引用（long 相关值按 goc LP64 语义）；long double 当 double 用；跨 ABI 比对 long 值先确认 |

---

## 附：跨组交叉发现与平台注意

1. **字符串字面量直接下标 bug**（B 组）：~~`"hello"[0]` 在 goc 返回垃圾 `1819043176`（gcc 104）~~ ——**已修复（P0.8，2026-10-02）**：现返回 104 与 gcc 一致（新增 c23_str_subscript.c PASS 4/4）；先赋 `const char *p = "hello"; p[0]` 仍正常。标准库可直接下标字符串字面量。
2. **前导小数点浮点已支持**（D1 组，2026-10-02）：`.12`/`.1'2` 现正常解析（lexer 前导点 float 修复），与 gcc 逐行一致；此前报 `unexpected token "."` 的缺口已消除。
3. **printf 能力**：`%a` 已支持（P0.7, 2026-10-02，与 gcc 逐字节一致）；无 `%wN`（_BitInt）；`%p` 格式与 gcc 不同且 ASLR 漂移，对拍文件已规避。
4. **LLP64 平台效应**（A1/D2/F 组交叉）：gcc 侧 `long`=32 位（LONG_WIDTH=32）；goc 侧 `long`/`unsigned long`=64 位；`_Generic` 的 long/unsigned 折叠即源于 goc 以宽度为主键比较。若目标是 LP64（Linux），long 相关结论需复测。
5. ~~**预定义宏全线缺失**~~ ——**已修复**（P1.7，2026-10-02）：`__STDC_VERSION__`=202311、`__STDC__`=1、`__STDC_HOSTED__`=1、`__DATE__`/`__TIME__` 已注入；`__STDC_NO_VLA__` 仍缺（随 P2.3 VLA 决策一并处理）。
6. ~~**`defined()` 不可见**~~ ——**已修复**（P2.15，2026-10-02）：`defined(__has_include)`/`defined(__has_c_attribute)` 返回 1，portable guard 写法成立。
7. **编译器健壮性**（A2 组）：块作用域非 static `thread_local` 原触发 goc 编译期 Go panic（nil 解引用，exit 2）——**已于 2026-10-02 P0.2 修复**（语义层干净拒绝，codegen panic 路径不可达）；_BitInt 与裸字面量比较的同类 panic 亦已于 2026-10-02 修复。
8. **roadmap 修正汇总**：#129 _Atomic"语法接受+lock 前缀"不成立（parse 拒绝）；#131 _BitInt 窄宽度掩码缺失（π 对拍只覆盖宽宽度）——**已于 2026-10-02 修复**；<stdbit.h>/<uchar.h> 后置成立；long double 降级成立但标记宏未定义；#embed prefix/suffix/if_empty 已支持（"后置未确认"不准确）；stdckdint "已支持"仅 32 位成立；stddef.h 的 offsetof/max_align_t 缺失未记录。

## 附：重跑方法

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File D:\projects\goc\tests\c23\run_c23_tests.ps1
```
遍历 `cases\*.c`，逐文件 goc 编译运行 + gcc -std=c2x 对拍，输出机械判定汇总表与 CSV（`_build\results.csv`）；FAIL/DIFF 时打印两侧完整输出（含 goc 报错）。语义判定（UNSUPPORTED 理由、PARTIAL 明细）以本文档为准。
