# Group E — 废弃 / 移除与语义变更（Removed / deprecated / semantic change）

对拍基线：`bin\goc.exe`（2026-10-02）vs `gcc.exe (Rev4) 16.2.0 MSYS2 UCRT64 -std=c2x -Wall -Wextra`。
所有 .c 均 LF / UTF-8 无 BOM / ASCII-only。本组以负向测试为主：gcc -std=c2x 的立场先逐一探针实测，再定 EXPECT 标签。

| 文件 | 特性 | 子用例数 | 判定 | goc 证据（含报错原文） | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|---|
| c23_knr.c | K&R（旧式）函数定义——C23 移除 | 1 个构造 | **PASS（GOC-REJECT）** | goc **拒绝**：退出码 1，`parse error: line 9: expected type specifier, got "a"`（K&R 定义的参数名 `a` 出现在参数列表后，goc parser 不接受）。即 goc 前端已无 K&R 语法（与"号称 C89 全量"略有出入，但符合 C23 移除） | gcc **仅警告仍通过**：`warning: old-style function definition [-Wold-style-definition]`，编译 exit=0，运行 exit=42（40+2）。gcc 在 c2x 下对 K&R 定义保持宽容 | **标准库一律不要写 K&R 定义**（`f(a,b) int a; {...}`）。goc 直接 parse error；即便在宽容的 gcc 上也是 -W 警告。全用原型 `int add(int a, int b)` |
| c23_removed_noreturn.c | `_Noreturn` 关键字 / `noreturn` 宏 / `<stdnoreturn.h>`（C23 保留，推荐 `[[noreturn]]`） | 3（SUMMARY 3/3） | **PASS** | goc **接受**，退出码 0，stdout 与 gcc 逐行一致（3/3）。stderr 一行：`note: skipping unavailable system header <stdnoreturn.h>`——**goc 没有该头**；但 `_Noreturn` 声明/定义与裸 `noreturn` 拼写均被原生接受（头被跳过、`#ifdef noreturn` 实测为 UNDEFINED 仍 parse 通过，见摘要） | gcc **干净接受**，编译 exit=0、运行 exit=0，stdout 3/3 一致。stderr 仅两条无害提示（`zero-length gnu_printf format string`、`'noreturn' function does return`），**无任何 `_Noreturn`/`noreturn` 弃用警告** | `_Noreturn` 与 `[[noreturn]]` 都能用；**但别 `#include <stdnoreturn.h>` 后依赖 `noreturn` 宏**（goc 无此头，宏不存在）。标准库要移植性就直接写 `_Noreturn` 关键字，或用属性 `[[noreturn]]`（A/C 组已测） |
| c23_noarg_args.c | 无参 `f()` 带参调用必须报错 | 1 个构造 | **PASS（REJECT）** | goc **拒绝**：退出码 1，`type error(s): call to "f": expected 0 arguments, got 2`。goc 把 `int f();` 当 0 参原型并做了实参个数检查 | gcc **拒绝**：`error: too many arguments to function 'f'; expected 0, have 2`（`note: declared here: int f();`），编译 exit=1 | goc 与 gcc 一致：`int f();` 在两侧都等价于 0 参，多传参会被拒。标准库保持用原型 `f(void)`；**不要用裸 `f()` 再带参调用**（写了也编译不过） |
| c23_implicit_int.c | 隐式 int（返回类型省略）——C23 移除 | 1 个构造 | **PASS（REJECT）** | goc **拒绝**：退出码 1，`parse error: line 9: expected type specifier, got "foo"`。**注意：goc 并未保留 C89 隐式 int**（非偏差，行为与 C23 一致） | gcc **拒绝**：`error: return type defaults to 'int' [-Wimplicit-int]`，编译 exit=1（GCC 14+ 默认按 error） | 标准库函数**必须显式写返回类型**（`int foo(void)`）。goc/gcc 两侧都会对隐式 int 报错，无 C89 兼容退路 |
| c23_implicit_func.c | 隐式函数声明——C23 移除 | 1 个构造 | **PASS（REJECT）** | goc **拒绝**，但拒绝点在 codegen 而非语义层：退出码 1，`codegen error: unknown function "g": not in goclib (... 全部 goclib 函数清单 ...), and no DLL named on its prototype (declare it as 'extern ret g(args), dllname;')`。goc 不会给出"implicit declaration of function"式诊断 | gcc **拒绝**：`error: implicit declaration of function 'g' [-Wimplicit-function-declaration]`，编译 exit=1 | 调用任何函数前**必须先原型声明/头文件包含**。goc 对未声明调用的报错信息是"未知函数不在 goclib"（定位在 codegen，报错偏晚但确实拒绝）；标准库切勿依赖隐式声明 |

## 组内发现摘要

### 与 roadmap 预期不符 / 需记录的 goc 行为

1. **K&R 定义——goc 前端已硬移除（领先于 gcc）**。
   实测：gcc 16.2.0 `-std=c2x -Wall -Wextra` 对 K&R 定义**只警告不报错**（`-Wold-style-definition`，exit=0），是本组唯一"gcc 宽容、goc 严格"的构造，故标签用 `GOC-REJECT`。goc 报 `parse error: line 9: expected type specifier, got "a"`。这说明 goc parser 已不需要 K&R 语法——与其"C89 全量"定位有出入，但方向正确（符合 C23 移除）。**机械判定 PASS（goc 拒绝即达标）。**

2. **goc 并不像 C89 那样宽容：隐式 int、隐式函数声明、无参带参调用三项 goc 全部拒绝**。
   组织者预估"C89 背景可能接受 → FAIL 偏差"，实测**全部不成立**：
   - 隐式 int：goc `parse error ... expected type specifier, got "foo"`（拒绝）。
   - 无参 `f()` 带参：goc `type error(s): call to "f": expected 0 arguments, got 2`（拒绝，且报错信息与 gcc 措辞高度对齐）。
   - 隐式函数声明：goc 在 **codegen 阶段** `unknown function "g": not in goclib ...`（拒绝，但**不**产生 gcc 式 "implicit declaration" 诊断；报错偏晚，定位会看到一长串 goclib 函数清单）。
   三个 REJECT 文件机械判定全部 **PASS**。结论：**goc 在"废弃/C23 移除"这一档比预期严格，没有留下 C89 兼容后门。**

3. **`<stdnoreturn.h>` 是 goc 缺失头，但影响被"裸 `noreturn` 拼写被原生接受"部分抵消**。
   - goc `#include <stdnoreturn.h>` → `note: skipping unavailable system header <stdnoreturn.h>` 后继续（不致命）。
   - 跳过头后 `noreturn` 宏**不存在**（探针 `#ifdef noreturn` 实测 UNDEFINED），但 `noreturn void macro_fn(void);` 仍能 parse 通过——说明 goc 把裸 `noreturn` 当原生拼写（等价于 `_Noreturn`）处理，而非仅靠头文件宏。
   - 因此 c23_removed_noreturn.c 的 stdout 不能依赖宏是否定义（否则 goc/gcc 输出会 DIFF）；文件只用固定 printf 行，stderr 的 header-skip note 不参与对拍，故仍 PASS。
   - **写标准库建议**：要"noreturn 不返回"语义，直接用 `_Noreturn` 关键字或属性 `[[noreturn]]`；**不要 include `<stdnoreturn.h>` 再用 `noreturn` 宏**（goc 无此头）。

### 交叉点说明
- 与 **A2 组 c23_noarg_func.c** 分工：A2 测"无参 `f()` 声明语义与**正常**调用"；本组 c23_noarg_args.c 只测"无参声明**带参调用必须报错**"，不重复。
- `[[noreturn]]` 属性正面用例由 A/C 组 c23_attr_noreturn.c 负责；本组只测遗留的 `_Noreturn`/`noreturn` 关键字与头。
- 本组"goc 接受 gcc 拒绝的构造"（FAIL 偏差）**一个都没有**：5 个文件机械判定全 PASS，goc 在废弃/移除这一档立场与 C23 期望一致（K&R 与 noreturn 头除外，二者已按各自标签单独记录）。
