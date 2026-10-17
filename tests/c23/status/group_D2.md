# Group D2 - 表达式/语义

对拍基线：`bin\goc.exe`（2026-10-02）vs `gcc.exe (Rev4) 16.2.0 MSYS2 UCRT64 -std=c2x -Wall -Wextra`（LLP64）。
所有 .c 均 LF / UTF-8 无 BOM / ASCII-only。"子用例数"指主对拍文件里实际参与 printf 的 case 数；被移除的 goc 缺口子用例在文件注释与本表格"goc 证据"列中说明。

> **2026-10-17 复核**：`c23_generic.c` 已扩到 14 子用例并 **14/14 逐行一致**（goc 与 gcc 退出码均为 0）。
> 本文档早前记录的 `_Generic` 缺口——① long/unsigned 在 LLP64 上折叠成 int、② typedef 名关联不命中、
> ③ 指针所指 const（`const int *` vs `int *`）被折叠——在当前构建上**均已不成立**；③已由前端修复
> （`genericTypeMatch` 增加逐层 const 比较）。为免误导，原"goc 5/10"判定与第 1 条发现改写为历史记录，
> 以本次实测为准。另新增区分：`char`/`signed char`/`unsigned char`、`long`/`long long`、
> `double`/`long double` 四组"表示相同但标准要求不同类型"的类型。

| 文件 | 特性 | 子用例数 | 判定 | goc 证据（含报错原文） | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|---|
| c23_compound_literal.c | 复合字面量 (T){...} | 10 | **PASS（带备注）** | 10/10 逐行一致（块作用域标量值、&(int){5} 取地址解引用、数组退化 (int[]){1,2,3}、循环内每轮重新初始化、struct/union、指示符 .x/.y、按值传参 f((int){13})、嵌套聚合）。**两个 goc 缺口已移除**：①文件作用域复合字面量 → `type error: compound literal requires block scope (file-scope static literals are not supported)`；②`(const int){11}` const 限定 → `type error: compound literal is const-qualified` | 全部一致，SUMMARY 10/10，退出 0；文件作用域 `&(int){33}` 与 const 字面量 gcc 均接受（各 33 / 11） | **块作用域复合字面量可放心用**；不要在文件作用域写 `int *p=&(T){...}`，不要用 `(const T){...}` |
| c23_generic.c | _Generic | 14 | **PASS** | **14/14 逐行一致**（2026-10-17 复核）。int/long/unsigned 字面量分别选中各自分支、控制表达式不求值（x++ 不生效）、char 与 short 均不提升、typedef `mylong` 关联命中、default、嵌套、宏 KIND 全部一致。另新增四组"表示相同但标准要求区分"的类型全部正确区分：指针所指 const `int*` vs `const int*` → 1/2；`char`/`signed char`/`unsigned char` → 1/2/9(default)；`long`/`long long` → 1/2；`double`/`long double` → 1/2 | gcc 14/14，输出与 goc 逐行相同，退出 0 | `_Generic` 可放心用于上述全部分派；注意 `unsigned char` 落在 `char`/`signed char` 之外的 default 是标准行为 |
| c23_constexpr_use.c | constexpr 编译期用途 | 7 | **FAIL** | goc **整文件编译失败**，首错 `parse error: line 20: expected integer constant, got "N"`（`_Static_assert(N==5,...)`）。探针逐一确认 goc 把 constexpr 变量挡在所有编译期语法位置外：case 标签 `case CK:` → `parse error: case label must be an integer constant (expected integer constant, got CK)`；局部 `int arr[N]` → `parse error: expected ';', got N`；文件作用域 `int garr[N]` → `expected integer constant, got N`；位域宽度 `int a:BW` → `expected bit width number after ':'`。goc 仅支持 constexpr 在表达式/printf 里当常量值用 | gcc 7/7（数组尺寸/case 标签/Static_assert/位域宽度/全局数组/算术折叠/sizeof 全部=常量表达式） | **别指望 goc 的 constexpr 能当编译期常量**：不能 `constexpr int N=...; int a[N];`、不能 `case 常量`、不能进 `_Static_assert`/位域宽度。标准库需要这些位置时用 `enum` 或字面量宏代替 |
| c23_constexpr_ptr.c | constexpr 指针/数组 | 5 | **PASS（带备注）** | 5/5 逐行一致（constexpr 空指针 np==0、constexpr 数组元素访问、sizeof 计数=3、数组求和=60、空指针比较）。**三项 gcc 拒绝/goc 缺陷已移除**：①`constexpr int *p=&obj` → gcc 拒 `'constexpr' pointer initializer is not null`，goc 虽过语法但 `*p` 运行时崩溃 0xC0000005；②`constexpr const char *s="lit"` → gcc 拒（同上），goc 过语法但 codegen `cannot index non-array global "gs"`；③`_Static_assert(&a!=&b,...)` 地址比较 → gcc 接受，goc `parse error: expected ';' after global declaration` | 全部一致，SUMMARY 5/5 | **constexpr 数组与 constexpr 空指针可放心用**；别用 constexpr 指真实对象地址（gcc 本工具链也不支持），别在 `_Static_assert` 里比对象地址 |
| c23_vla.c | VLA 与变修改类型 | 5 | **FAIL** | goc **整文件编译失败**：`parse error: line 21: expected integer constant, got n`（`int vsum(int n, int a[n])` 的 VLA 形参即拒，main 未进入）。goc **不定义 `__STDC_NO_VLA__`**（无任何预定义宏），按 C23 未定义该宏即须支持 VLA——故为真实缺口，非声明取舍 | gcc 5/5（运行时数组求和=30、sizeof VLA 运行时求值 sz=12/k=4、多维 mat[1][2]=7、VLA 形参 vsum=15、`int a[static 5]` 形参） | **VLA 完全不可用**：不要在标准库写 `int a[n]`/`sizeof(变长)`/VLA 形参。需要可变长时用固定上界数组或显式 malloc |
| c23_bitfield.c | 位域 | 6 | **PASS（带备注）** | 6/6 逐行一致（宽度 1/2/3、signed s:3=-1 vs unsigned u:3=7、纯 int f:3 赋 7=-1 **goc 与 gcc 同为有符号**、零宽位域跨分配 sizeof=8、无名位域 sizeof=4、volatile 位域=-4）。**一处移除**：`_BitInt(7) x:5` 位域 → goc `parse error: bit-field base type must be an integer type, got _BitInt(7)`（gcc 接受，x=-1/y=15/sizeof=2） | 全部一致，SUMMARY 6/6；纯 int 位域符号性 goc=gcc=signed | **普通整数位域可放心用**（符号性与 gcc 一致、布局 sizeof 一致）；`_BitInt(N)` 位域不可用 |

## 组内发现摘要

### 与 roadmap 预期不符 / 需记录的 goc 行为

1. **（历史记录，2026-10-17 已不成立）`_Generic` 在 LLP64 上把 long/unsigned 折叠成 int**。本条为 2026-10-02 基线的发现，当时的构建下 `_Generic(1L, long:..., int:...)` 选中 int 分支、`1U` 选中 int 分支、typedef `mylong` 关联也不命中。**2026-10-17 用当前构建复核：全部选中正确分支（c23_generic.c 14/14 与 gcc 逐行一致）**。roadmap #125 所称"精确类型匹配、宽+符号递归比较已修"现已由实测确认。保留本条仅为记录演进，写标准库时不再受此限制。

2. **goc 的 "轻量 constexpr" 只到表达式值，不进编译期常量语法**。roadmap 标 constexpr 轻量✅，实测：声明 `constexpr int N=5` 并 `printf("%d",N)` 正常；但 `_Static_assert(N==5)`、`case CK:`、`int a[N]`、`int g[N]`、位域 `int a:BW` 全部在解析期拒绝（`expected integer constant, got <name>` / `case label must be an integer constant` / `expected bit width number after ':'`）。**写标准库时不要用 constexpr 变量当数组尺寸/case/Static_assert/位域宽度的编译期常量，用 enum 或字面宏。**

3. **constexpr 指针：gcc 16.2 只允许 constexpr 指针为 null**。`constexpr int *p=&obj` 与 `constexpr const char *s="lit"` 都被本工具链 gcc 拒为 `'constexpr' pointer initializer is not null`——这是 gcc 的限制，非 goc 问题。goc 反而接受这两种语法，但 `*p` 运行时崩溃（0xC0000005）、下标字符串报 codegen 错，**两边都不可用**。真正能用的是 constexpr 数组与 constexpr 空指针。

4. **VLA 完全未支持且未声明**。goc 对 `int a[n]`/`int a[static 5]`/VLA 形参一律解析拒绝，又不定义 `__STDC_NO_VLA__`。C23 下这是 conformance 缺口（未声明即须支持），不是显式取舍。

5. **复合字面量：块作用域全过，但文件作用域与 const 限定被拒**。roadmap #127 已列"取地址/数组退化/循环内新对象/{} 零初始化全过"，本组复核一致并补充：循环内 gcc 复用同一栈槽但每轮重新初始化（goc 也正确复位值）；goc 不支持文件作用域复合字面量与 `(const T){...}`。

6. **位域与 `_BitInt`**：普通位域 goc 与 gcc 完全一致（含纯 int 位域按有符号、布局 sizeof 一致）；但 `_BitInt(7) x:5` 位域 goc 拒绝（`bit-field base type must be an integer type`）。gcc 16.2 实测**接受** `_BitInt` 位域。`_BitInt` 本体归 A2 组，位域交互在此记录。

### 交叉点说明
- `_Generic` 顶层 const 被忽略（C 标准行为，goc/gcc 一致）：控制表达式经左值转换丢掉顶层限定，因此 `char * const` 匹配 `char *` 关联、`const int` 控制表达式匹配 `int` 关联而非 `const int` 关联。**指针所指 const（`const int *` vs `int *`）2026-10-17 起 goc 可正确区分**（此前被折叠，报 `type int* appears twice`），修复方式是 `genericTypeMatch` 逐层比较 const 限定——与 D1"类型匹配"主题相关。
- LLP64 平台效应（long 宽度=int）曾是本组多个 _Generic/constexpr 发现的共同根因；2026-10-17 复核后 `_Generic` 相关结论已更新（见第 1 条），constexpr/VLA 的结论仍以 LP64 平台复测为准。
- `_Static_assert` 本身可用（D1/A 组 c23_static_assert 已覆盖），本组发现的是它**内接 constexpr 变量/地址比较**时的缺口，属常量表达式折叠范畴。
