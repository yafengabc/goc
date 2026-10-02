# tests\c23 测试契约（C23 特性测试套件）

本目录是 goc（Go 写的 C 编译器，项目根 `D:\projects\goc`）的 C23 全特性测试套件。  
目标：为 C23 全部特性编写详尽测试代码，逐文件用 goc 与 gcc -std=c2x 对拍，产出就绪度矩阵，  
回答"写标准库时哪些语言特性已 ready、哪些要避开"。

## 目录结构（已建好骨架）

- `cases\`   测试 .c（命名 `c23_<feature>.c`）
- `status\`  各组状态文档 `group_<X>.md`（由各子代理写）
- `_tools\`  对拍引擎 `check_case.ps1`（组织者已写好并验证，勿改）
- `_build\`  临时产物（gcc 输出 exe、探针文件），勿提交
- `C23_STATUS.md` 最终矩阵（组织者汇总，子代理不写）
- `run_c23_tests.ps1` 一键重跑脚本（组织者写）

## 工具链（组织者已实测可用）

- goc：`D:\projects\goc\bin\goc.exe` —— 把 .c 编译为同目录同名 .exe 并自动运行；  
  编译成功时 stdout 有两行 `compiled ...` 日志（引擎已剥离），程序退出码非 0 时才打印  
  `(program exited with code N)`；进程退出码成功恒为 0；报错走 stderr（格式如  
  `type error(s):` / `parse error: line N: ...` / `note: skipping unavailable system header <X>`），退出码非 0。
- gcc：`D:\msys\ucrt64\bin\gcc.exe -std=c2x -Wall -Wextra`（16.2.0，MSYS2 UCRT64）。
- 对拍引擎（每写完一个 .c 就跑一次）：
  ```
  powershell -NoProfile -ExecutionPolicy Bypass -File D:\projects\goc\tests\c23\_tools\check_case.ps1 -CaseFile <文件的绝对路径>
  ```
  引擎输出：goc 侧 stdout/stderr/退出码、gcc 编译+运行两侧 stdout/stderr/退出码、  
  以及 `MECHANICAL VERDICT: <PASS|FAIL|UNSUPPORTED|PARTIAL|DIFF|BROKEN_TEST|TIMEOUT>`。  
  机械判定是参考，最终语义判定由你定稿并写入 status 文档。

## 文件规范

1. 命名 `c23_<feature>.c`，一个特性一个文件（或紧密相关的小簇），放 `cases\`。
2. 头部块注释固定含 4 行：
   ```
   /* C23 feature: <特性名>
    * Clause:     <C23 条款/章节号>
    * Strategy:   <测试策略一句话>
    * Status:     <判定，写文件时可为 PENDING，定稿后改>
    * EXPECT: <PASS|REJECT|GOC-REJECT|UNSUPPORTED> */
   ```
   `EXPECT` 是引擎判定的依据，必须写。
3. 多组独立子用例：正常路径 + 边界值 + 组合/嵌套/与既有特性交互。每个子用例  
   `printf` 输出可区分的 `caseN: ...`（结果或 ok），结尾固定一行  
   `SUMMARY: <通过数>/<总数>`，全部通过 `return 0`，否则 `return 1`。宁细勿粗。
4. 源码 ASCII-only（注释英文），LF 换行（用 Write 工具写；写完用字节检查确认无 `0D 0A`，有则转）。
5. 不得依赖 goc 私有扩展；不得包含 goc 未实现的头（stdio.h 恒可用）。
6. 缺失头/缺失特性的写法：
   - 直接 `#include <缺失头>` 时，goc 只打 `note: skipping unavailable system header <X>` 并继续，  
     后续用到该头类型会报错——这正是要记录的 UNSUPPORTED 证据，原样抄录进文档。
   - 属性类探测用标准守卫 `#if defined(__has_c_attribute) && __has_c_attribute(...)`。  
     **注意：实测 goc 的 `__has_include`/`__has_c_attribute` 对任何实参都返回 no（2026-10-02 版本，疑似缺陷）**——  
     守卫对 goc 恒走 else 分支，这本身就是一个要记录的 goc 行为；守卫不得影响文件在 gcc 侧的正常测试。
7. 负向测试（C23 移除/应拒绝的构造）：
   - gcc -std=c2x 也拒绝 → `EXPECT: REJECT`（双方都拒绝 = PASS；goc 接受 = FAIL 偏差，记录）。
   - gcc 只警告仍接受（如 K\&R 定义：gcc 仅 `-Wold-style-definition` 警告）→ `EXPECT: GOC-REJECT`  
     （goc 拒绝 = PASS；gcc 立场仅作参考记录）。
   - 写前先跑一次 gcc 确认其真实立场，再定标签。
8. `EXPECT: UNSUPPORTED` 仅用于 roadmap 已确认的 goc 设计取舍（缺失头、long double 降级等）。

## 判定定义（四种，语义判定）

- **PASS** = goc 与 gcc 行为一致（输出+退出码一致；或双方都拒绝负向用例）
- **FAIL** = gcc 通过而 goc 编译/运行错误 = 真实缺口。**必须把 goc 报错原文原样抄录进 status 文档**
- **UNSUPPORTED** = goc 明确设计取舍（缺头、long double 降级等），记录其报错/降级行为
- **PARTIAL** = 部分子用例通过（如 SUMMARY 不一致），需指明哪些子用例失败

## 写 status 文件（UTF-8，LF）

每组写 `D:\projects\goc\tests\c23\status\group_<X>.md`：

- 标题：`# Group <X> — <组名>`
- 每文件一节或一行小表：**文件 | 特性 | 子用例数 | 判定 | goc 证据（FAIL/UNSUPPORTED 附报错原文） | gcc 对拍结论 | 写标准库建议（能放心用/要避开）**
- 末尾"组内发现摘要"：与 roadmap 预期不符的 goc 行为、与其他组的交叉点说明。

## 硬性红线

- 绝不修改 `src\` 下任何文件（含 `src\goclib\` 头文件、`src\examples\`、`src\expected\`），只读可以
- 绝不运行 gocregress、绝不改动 `tools\`、`bin\gocregress.exe`
- 只在 `tests\c23\` 与你自己的工作区写文件；不删改别人的文件
- 测试代码 LF 换行；不引入 CRLF

## 回报

最终消息逐文件给出：`文件名 | 判定 | 子用例数 | 一句话证据`（FAIL 含 goc 报错首行）。细节在 status 文件里。
