# Group B — 预处理（Preprocessor）

对拍引擎：`_tools/check_case.ps1`（goc vs gcc 16.2.0 `-std=c2x -Wall -Wextra`）。
机械判定见各文件；语义判定为本组定稿。除注明外，goc 编译成功时 stdout 的两行
`compiled ...` 日志已由引擎剥离。

| 文件 | 特性 | 子用例数 | 判定 | goc 证据（报错/诊断原文） | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|---|
| c23_embed.c | `#embed` 基本 | 4 | PASS | 无；sizeof/逐字节/多段 #embed 全部与 gcc 一致（blob={65,66,67,10}, twice=9 字节） | 与 goc stdout 完全一致 | 能放心用（注意：分隔逗号必须单独成行，紧跟文件名的逗号会被当成 #embed 选项） |
| c23_embed_limit.c | `#embed limit()` | 4 | PASS | 无；limit(0) 拼 0 字节、limit(2)/limit(4)/limit(10 超长钳到文件长) 全部一致 | 与 goc stdout 完全一致 | 能放心用（与现有示例 limit(4) 行为吻合） |
| c23_embed_adv.c | `#embed prefix/suffix/if_empty/__has_embed` | 5 | PARTIAL | case1-4（prefix/suffix/if_empty）与 gcc 一致；case5 输出 `__has_embed avail=0 exist=-1 missing=-2`（goc 未提供 `__has_embed` 宏） | gcc：prefix/suffix 需自带分隔逗号、if_empty 在 limit(0) 时补 token，`__has_embed` 在 `#if` 内 exist=1/missing=0，SUMMARY 5/5 | prefix/suffix/if_empty 能放心用；`__has_embed` 不能用（goc 未实现，guard 必走 else） |
| c23_elifdef.c | `#elifdef`/`#elifndef` | 5 | PASS | 无；链式/与 #ifdef #ifndef 混用/嵌套/#else 回退全部一致 | 与 goc stdout 完全一致 | 能放心用 |
| c23_warning.c | `#warning "msg"` | 1 | PASS | stderr 原样：`...c23_warning.c:11: warning: c23_warning probe: this preprocessor warning must not stop the build`（仅警告，不阻断，退出码 0） | gcc stderr 原样：`...c23_warning.c:11:2: warning: #warning "..." [-Wcpp]`；均编译运行成功 | 能放心用（goc 警告格式略简，但语义=可继续编译） |
| c23_has_include.c | `__has_include` | 6 | PASS | 无；`<stdio.h>`=1、`<string.h>`=1、missing=0、`<threads.h>`=0、`"stdio.h"`=1、逻辑组合=1，SUMMARY 6/6 | 与 goc stdout 完全一致 | 能放心用（见下方摘要——组织者报告的缺陷在本版本不复现） |
| c23_has_c_attribute.c | `__has_c_attribute` | 7 | FAIL | `defined(__has_c_attribute)`=0，标准 guard `#if defined(__has_c_attribute) && __has_c_attribute(...)` 恒走 else：deprecated/nodiscard/noreturn/maybe_unused/fallthrough 全部回落到 0，goc SUMMARY 2/7 | gcc guard_active=1，五个属性=1、likely/unlikely=0，SUMMARY 7/7 | 要避开：不能按标准 portable guard 探测属性；若直接（不 guard）调用 goc 其实返回正确逐属性值（见摘要） |
| c23_va_opt.c | `__VA_OPT__` | 5 | PASS | 无；空变参逗号省略/非空保留/多参/嵌套转发/与 ## 组合全部一致 | 与 goc stdout 完全一致 | 能放心用 |
| c23_empty_macro.c | `#define F()` 空参宏 / `F(...)` | 5 | PASS | 无；空参空展开、有体零参宏、单参对比、变参空调用全部一致 | 与 goc stdout 完全一致 | 能放心用（小坑：goc 不接受空宏展开后紧跟 `= +常量` 的一元正号写法，避免即可） |
| c23_macro_limits.c | 自引用不递归/30 层嵌套/#/## | 6 | PASS | 无；`#define X X` 在 #if 中折叠到 0（不无限递归）、L30=30、# 串化、## 生成新 token、空操作数边界全部一致 | 与 goc stdout 完全一致 | 能放心用 |
| c23_pp_const_expr.c | `#if` 常量表达式 | 8 | PASS | 无；算术/位/逻辑/三目/字符常量/defined()/十六进制/intmax 回绕全部一致（gcc 的两条 overflow 警告 goc 未报，但判定值一致） | gcc 判定 E_WRAP=1 且报 `integer overflow in preprocessor expression` / `integer constant is so large that it is unsigned`，goc 静默同值 | 能放心用 |
| c23_pp_bad.c | `#if 1/0`（负向） | 1 | PASS（双方拒绝） | stderr 原样：`preprocess error: ...c23_pp_bad.c:11: division by zero in #if expression`，退出码非 0 | gcc stderr 原样：`...c23_pp_bad.c:11:7: error: division by zero in #if`，退出码非 0 | goc 与 gcc 一致拒绝，语义正确 |

## 组内发现摘要

1. **`__has_include` 可放心用——组织者报告的缺陷在本版本（2026-10-02 `bin\goc.exe`）不复现。**
   组织者手记称 "goc `__has_include(<stdio.h>)` 返回 no"。本组实测推翻该结论：goc 对 stdio/stdlib/string/math 均返回 1，对其缺失的头（wchar/complex/stdatomic/iso646）返回 0，与 goc 真实头文件清单一致；对不存在的头返回 0。即 goc 的 `__has_include` 能正确反映"自己到底有没有这个头"，按标准 guard 探测是可靠的。写标准库可用 `__has_include(<...>)` 做有/无头的可选包装。

2. **真正的缺陷是 `__has_c_attribute` 的标准 guard（`#if defined(__has_c_attribute)` 恒为假）。**
   goc 不把 `__has_c_attribute` 暴露给 `defined()`（探测 `defined(__has_c_attribute)`=0），所以契约推荐的 portable 守卫
   `#if defined(__has_c_attribute) && __has_c_attribute(deprecated)` 在 goc 上恒走 else 分支，看似"所有属性都不支持"。
   但绕开 guard、直接在 `#if` 里调用 `__has_c_attribute(deprecated)` 时，goc 实际返回正确逐属性值
   （deprecated/nodiscard/noreturn/maybe_unused/fallthrough=1，likely/unlikely=0），与 gcc 一致。
   → 标准库**不要**用 `#if defined(__has_c_attribute) && ...` 这种 guard 来门控属性（会被静默关闭）；若要用，得直接 `#if __has_c_attribute(x)`。建议向 goc 报告：把 `__has_c_attribute` 注册为 `defined()` 可见的 feature-test 宏。

3. **`#embed` 的 prefix/suffix/if_empty 其实已支持**（roadmap 标注"后置未确认"不准确）。实测 goc 与 gcc 行为一致，且与 gcc 相同的语法要求：选项 token 自带分隔逗号（`prefix(11,)` / `suffix(,22)` / `if_empty(0xEE,)`），gcc 不会在选项 token 与字节流之间自动补逗号。唯一缺口是 `__has_embed` 未提供。

4. **跨组交叉发现（不属于本组，但影响标准库写法）：goc 对"直接下标字符串字面量"有 bug。**
   实测 `"hello"[0]` 在 goc 返回垃圾值 `1819043176`（gcc 为 104），但 `const char *p="world"; p[0]` 返回正确的 119。
   → 标准库若需要读字符串字面量首字节，先赋给 `const char*` 再下标。此现象应转交词法/语义组核实。

5. 其他细节：goc 对 `#if` 溢出不像 gcc 那样告警（静默同值）；`#warning` 可正常编译运行，goc 警告格式比 gcc 简短但语义一致；`#if 1/0` goc 与 gcc 都拒绝。
