# Group A1 — C23 关键字与标量类型

对拍基线：goc `bin\goc.exe`（2026-10-02 版本）vs `gcc -std=c2x -Wall -Wextra`（MSYS2 UCRT64 16.2.0）。
goc 不定义 `__STDC_VERSION__`/`__STDC__`（种子 c23_version.c 结论），本批用例均不依赖它们。
平台为 Windows x64（LLP64：gcc 侧 `long`=32 位；goclib 侧 `long`/`unsigned long`=64 位，故取址用 `unsigned long long` 保证 64 位）。

| 文件 | 特性 | 子用例数 | 判定 | goc 证据（含报错原文） | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|---|
| c23_bool.c | bool/true/false 关键字 | 7 | PASS | 全过 7/7；stderr 仅 `note: skipping unavailable system header <stdbool.h>`（goc 无此头，关键字仍可用） | 7/7，真 stdbool.h，输出逐行一致 | **能放心用** bool/true/false；勿 `#include <stdbool.h>`（goc 无该头） |
| c23_nullptr.c | nullptr/nullptr_t | 9 | PARTIAL | 9/9、exit 0；`sizeof(nullptr)=8`；`_Generic(nullptr)` 落到 `int`；goclib `typedef void* nullptr_t` | 9/9；`__STDC_VERSION__=202311`；nullptr 值可用；`nullptr_t` 类型名经 `<stddef.h>` 仍**不暴露**（此 mingw 构建），且为与 void* 不相容的独立类型 | **能放心用 nullptr 判空**；别假设 `nullptr_t` 是独立类型（goc 即 void*）；勿用 `_Generic(nullptr)` 判类型 |
| c23_alignas_alignof.c | alignas/alignof + 旧名 | 7 | PARTIAL | goc 4/7；标量变量 alignas 真实对齐（local alignas(16) offmod16=0）；但 `alignas(32) char[64]` 全局 offmod32=**8**、结构体成员 alignas(16) offmod16=**8**、局部 `alignas(64) char[128]` offmod64=**48**——实际未对齐；`alignof()` 仍报告期望值 | 7/7，全部真实对齐（offmod 全 0） | **要避开**：别用 alignas 对齐结构体成员/数组/SIMD/缓存行；标量变量 alignas 可放心用 |
| c23_constexpr.c | constexpr 对象 | 8 | PASS | 8/8；文件作用域 constexpr 标量/指针/数组/`static constexpr` 正确折叠。goc **拒绝**块作用域 constexpr（`parse error: line N: expected ";", got "int"`）和 constexpr 标识符作数组界（`expected ";", got "N"`） | 8/8，块作用域 constexpr 与 constexpr 数组界均支持 | **能放心用文件作用域 constexpr**；勿在函数内写 constexpr、勿用 constexpr 变量作数组维度 |
| c23_constexpr_neg.c | constexpr 非常量初始化（负向） | 1 | FAIL（偏差） | goc **接受** `constexpr int bad = glob;`（exit 0，无报错） | gcc 拒绝：`error: initializer element is not constant` | **要避开**：别指望 constexpr 在编译期拦截非常量初始化（goc 不强制） |
| c23_typeof.c | typeof/typeof_unqual | 7 | PASS（带记录缺口） | 7/7；支持 typeof(类型)、typeof(常量)、typeof(标量变量)、typeof_unqual(const/volatile)、typeof(int*)、typeof(数组变量)。goc **拒绝**：`__typeof__`（`parse error: line N: expected ";", got "y"`）、`typeof(&x)`（`only typeof(type), typeof(var) and typeof(constant) are supported`）、嵌套 typeof（`unexpected token ")"`）；`typeof(2.0)` double 得 0.0 | 7/7，全部支持（含 __typeof__/嵌套/typeof(&x)） | **能放心用 typeof/typeof_unqual**（实参限类型名或标量变量）；勿用 `__typeof__`、嵌套 typeof、地址表达式 |
| c23_auto.c | auto 类型推导 | 7 | PASS | 7/7；int/char/long long/指针/数组衰减/函数指针/const/static/for-init/文件作用域全过 | 7/7；gcc 对 `static auto` 警告 `'auto' is not at beginning of declaration` | **能放心用 auto**；注意声明序（gcc 建议 `auto const`/`static auto` 后置警告） |
| c23_auto_neg.c | auto 无初始化器（负向） | 1 | PASS（双方都拒） | goc：`type error(s): line 11: auto declaration of "x" requires an initialiser` | gcc：`error: 'auto' requires an initialized data declaration` | 双方一致拒绝，符合预期 |
| c23_enum_type.c | enum E:T 底层类型 | 5 | PARTIAL | goc 4/5；语法解析且枚举常量值正确（9000000000LL、4000000000U）；但 `sizeof(enum)` **恒为 4**：`enum:unsigned char`→4（gcc 1）、`enum:long long`→4（gcc 8） | 5/5，sizeof 按底层类型（1/8） | **要避开**：别用 `enum E:char` 做紧凑存储；枚举变量装不下 >int 范围值（布局被忽略） |
| c23_static_assert.c | static_assert 两形式 + 旧名 | 4 | PASS（带记录缺口） | 4/4；支持 `static_assert(e,"msg")` 与 `_Static_assert(e,"msg")` 文件/块作用域。goc **拒绝**无消息形式 `static_assert(e)`（`parse error: expected "," after static_assert condition`）和结构体体内 static_assert（`expected type specifier, got "static_assert"`） | 4/4，无消息形式与结构体内均支持 | **能放心用 static_assert 但必须带消息**；勿写 `static_assert(e)`、勿放进 struct 体内 |
| c23_static_assert_neg.c | static_assert(0) 假条件（负向） | 1 | PASS（双方都拒） | goc：`parse error: static_assert failed: this condition is always false` | gcc：`error: static assertion failed: "..."` | 双方一致拒绝，符合预期 |
| c23_char8.c | u8 字面量类型身份 | 5 | PARTIAL（DIFF） | 5/5；u8 字符串按 `char*` 衰减（C11 行为）；`_Generic(u8'A')` 落 `other`；`sizeof(u8'A')=8`（异常）；goc 内建 `char8_t` 类型名可用（无需 uchar.h） | 5/5；u8"abc"=unsigned char*/char8_t*（gcc 警告 signedness）；u8'A'=unsigned char；`sizeof(u8'A')=1`；不 include uchar.h 时 `char8_t` 类型名**不暴露** | **要避开**按 char8_t 类型身份做判断；u8 字符串当普通 char* 用；u8 字符字面量大小异常（8 字节）勿依赖 |

## 组内发现摘要

1. **goc 的 constexpr 是"轻量且宽松"的**：只在文件作用域生效，且**不强制初始化器为常量**（c23_constexpr_neg.c：gcc 拒、goc 收）。写标准库不能拿 constexpr 当编译期校验工具。
2. **sizeof/布局是最大的两类偏差**：alignas 对数组与结构体成员"声明了但没真对齐"；`enum E:T` 语法接受但 sizeof 恒为 int(4)。涉及内存布局/对齐/紧凑存储的标准库设施要绕开。
3. **类型身份探针两侧都"缺类型名"**：此 gcc -std=c2x 构建在不 include `<uchar.h>`/`<stddef.h>` 时不暴露 `nullptr_t`、`char8_t` 类型名（尽管 `__STDC_VERSION__=202311`）；goc 侧 `nullptr_t` 直接 typedef 成 `void*`。跨编译器不要用这些类型名做静态断言。
4. **typeof/auto 已较成熟**：auto 全形态可用；typeof 限"类型名/标量变量"实参，`__typeof__`、嵌套 typeof、`typeof(&x)`、typeof(浮点常量) 不支持或有 bug。
5. **static_assert 必须带消息**：无消息形式与结构体体内形式 goc 均拒；`static_assert(0,...)` 假条件双方一致拒绝。
6. **与 roadmap 预期一致/补充**：roadmap 称"static_assert 两形式（无消息未逐项确认）"——实测无消息形式**不支持**；"enum E:int 仅 int 确认"——实测其他底层类型**语法解析但布局忽略**；"u8 前缀✅（char8_t 语义未确认）"——实测 goc 仍按 C11 char* 模型，`sizeof(u8'A')=8`。
7. **交叉点**：`nullptr_t`/`char8_t` 类型名暴露问题与 F 组（`<uchar.h>` 整体探针）、D1 组（u8 值/大小/拼接）有重叠；本组只覆盖类型身份探测，值与拼接归 D1，头整体探针归 F。
