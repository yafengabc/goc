# goc 编译器 Code Review 报告

- 评审对象：`D:\projects\goc`（`src/` 编译器与汇编器、`docs/` roadmap）
- 评审日期：2026-10-02
- 评审方式：全量通读 + 构建验证 + 测试运行 + 运行时复现
  - 通读：`main/version/headers/lexer/ast/types/parser/check/cpp/ufcs/liveness/print/codegen`（约 1.8 万行）+ `goa/asm/pe/elf/main`（约 3.6 千行）+ 全部单元测试
  - 验证：`go build` 通过；`go test ./src` **失败**（1 个用例）；`go test ./src/goa` 通过；4 个疑点用真实编译/运行复现
- 评审基准：仓库 HEAD `c4c8d8a`，工作区干净

---

## 结论摘要

| 严重度              | 数量 | 主题                                                                              |
| ---------------- | -- | ------------------------------------------------------------------------------- |
| P1（正确性/管线失效，已验证） | 2  | `-O1` 内联完全失效且测试门禁为红；复合赋值 `E1 op=E2` 对左值副作用求值两次                                  |
| P2（可复现 bug，已验证）  | 2  | `-MF/-MT/-MQ` 参数解析死代码并吞掉取值；UTF-8 BOM 不跳过                                        |
| P3（潜伏错误，当前不触发）   | 3  | 汇编器 3 处静默错码：带索引的立即数存储丢索引、`test r,imm` 误编码为 `add`、`bt [rip+],imm8` RIP 修复少算 1 字节 |
| P3（设计/一致性问题）     | ~8 | README 严重滞后、`varTypes` 跨 TU 泄漏、`#include` 缺失静默跳过、`#if` 除零、类型系统过度宽松等             |

**总体评价**：这是一份工程质量明显高于同类"玩具编译器"的代码。优化管线（常量传播、死存储、活性 DSE）的保守性论证、goa 编码器对 REX/SIB/段前缀的处理、PE/ELF 写出器的布局推理，都属于认真且可追溯的工程写作；大量注释以"踩坑史"形式沉淀了历史 bug 的教训（如 #81 C7 误写字节存储、TLS 目录必须写 ImageBase+VA 等）。代码风格统一、错误处理基本完整、测试对优化 pass 的断言非常细。问题集中在两处：**一个被重构漏掉的内联形状检查**（导致招牌优化静默失效、测试门禁持续为红），以及**文档/CLI 与实现不同步**。

---

## P1 严重问题

### 1. `-O1` 内联优化完全失效，仓库自带测试门禁为红（已验证）

**现象**：`go test ./src` 在 HEAD 上失败——`TestOsKeepsCalls` 断言 `-O1` 必须内联 `add3`，实际生成的是 `call add3`。

**根因**（两处形状检查都与真实前导码不一致）：

- 前导码实际发射顺序（`codegen.go:4129-4144`，**无条件**推送全部 4 个 callee-save 寄存器）：
  ```
  push rbp
  mov rbp, rsp
  push rbx
  push r12
  push r13
  push r14
  sub rsp, N
  ```
- 但候选函数判定 `inlineCalls.regular`（`codegen.go:2912-2918`）与模板提取 `extractInlineCand`（`codegen.go:2737-2743`）都要求第 3 条指令必须是 `sub rsp, N`：
  ```
  push rbp / mov rbp, rsp / sub rsp, N   ← 旧前导码形状
  ```
  两条注释至今写着 "Canonical prologue: push rbp / mov rbp, rsp / sub rsp, N"。

真实流中第 3 条是 `push rbx` → `regular=false` → **每个函数都被排除出内联候选** → `-O1` 的内联 pass 是死代码（`codegen.go:2226` 确实会调用 `inlineCalls`，但它永远返回原样）。

**影响**：

- `-O1` 与 `-Os` 行为趋同（内联是两者唯一区别，见 `codegen.go:2220-2223` 注释），优化能力名存实亡；
- `run_tests.sh:219` 会执行 `go test ./...`，因此整套测试门禁当前是红的，且是在已提交状态上红的（`git status` 干净），说明该重构（插入 callee-save push）后没有跑或没有修测试。

**修复方向**：把两处形状检查改为"`push rbp; mov rbp,rsp` 之后，跳过若干个 `push rbx/r12/r13/r14`，再要求 `sub rsp, N`"。注意：即使形状修复，`extractInlineCand:2798` 的 `calleeSaveWrites` 安全筛选会拒绝任何把 rbx/r12/r13/r14 写入目的地的指令，而寄存器主本地（register-homed locals）在函数体中正是通过 `mov rbx,[rbp-8]` 这类指令读写的——**候选集在形状修复后仍然极窄**（只有不碰 callee-save 寄存器的叶函数）。要让内联真正生效，需要同步考虑"被内联函数把寄存器主本地重映射到栈"或放宽模板筛选，否则内联收益依旧微乎其微。

---

### 2. 复合赋值 `E1 op= E2` 对左值副作用求值两次（已运行时复现）

**现象**（`parser.go` 的 `parseAssign`）：`a += b` 被脱糖为 `a = a + b`，其中 `a` 作为二元运算的左操作数和赋值目标**各生成一份**，两侧副作用各执行一次。C11 6.5.16.2 规定 `E1 op= E2` 等价于 `E1 = E1 op E2` **但 E1 只求值一次**。

**运行时复现**：

```c
int main() {
    int a[4] = {1, 2, 3, 4};
    int i = 0;
    a[i++] += 10;
    return a[0]*100 + a[1]*10 + i;
}
```

- gcc：退出码 **1121**（正确语义：`i=1`，`a[0]=11`，`a[1]=2`）
- goc：退出码 **212**（`i` 自增两次=2，`a[0]` 未被改写，值 11 被写入了错误下标）

**影响**：`a[i++] += x`、`*p++ += x`、`p[i] -= x` 等含副作用的左值复合赋值全部错误。属语义正确性 bug，编译器内部一致、但与 C 语义不符，无法靠优化 pass 掩盖。

**修复方向**：不要脱糖；为 AssignExpr 生成"左值地址求值一次 → 读取 → 运算 → 存储"的序列（codegen 的 `genLValue` + `genStoreElem` 管线已具备该能力，只需保留 LHS 的求值结果）。

---

## P2 可复现 bug

### 3. `-MF/-MT/-MQ` 参数解析：吞分支是死代码，取值被当成输入文件（已复现）

**现象**（`main.go:433-443`）：

```go
case "-Wall", ..., "-M", "-MM", "-MD", "-MP", ...:
    // swallow a possible separate value for -MF/-MT/-MQ style flags
    if name == "-MF" || name == "-MT" || name == "-MQ" { ... }
```

switch 的 case 列表**漏掉了 `-MF/-MT/-MQ`**，所以：

- 分支内的 `if name == "-MF" ...` 永不执行（死代码，注释还写错了两次 "-MQ"）；
- `-MF deps.d` 中 `-MF` 落入 `default` 被忽略，`deps.d` 被当作**输入文件**。

**运行时复现**：`goc -MF deps.d mf_test.c` → `open deps.d: The system cannot find the file specified`（编译目标是 `deps.d`）。

与 `main.go` 头部文档声称接受 `-M*` 直接矛盾；`run_tests.sh` 中的 `splitRunArgs` 也把 `-MF/-MT/-MQ` 视为带值参数——说明实现意图支持，只是 case 列表忘了写。

**修复**：把三个旗标加入 case 列表（并顺带修正注释里的 `-MQ` 重复）。

### 4. 词法层不跳过 UTF-8 BOM（已复现）

**现象**：对以 BOM（EF BB BF）开头的源文件，cpp/lexer 报 `unexpected character 'ï'`。gcc/clang 都会静默跳过 BOM。Windows 上记事本、"另存为 UTF-8"、PowerShell `Set-Content -Encoding UTF8` 都会产生 BOM，这是 Windows 用户最普通的保存方式——我第一次构造测试文件就被这个绊住。修复极小：cpp 读取文件后若前 3 字节为 BOM 则裁剪。

---

## P3 潜伏的汇编器编码错误（当前 goc 输出不会触发，手写 asm/未来变更会踩）

以下三处位于 `goa/asm.go` 的手写编码器，**goc 自身目前不发射这些形式**（已用 grep 核对 codegen 的发射模式），因此是"静默错码"而非"已触发错误"。危险在于它们都**静默产出错误机器码而不是报错**，且注释/结构上看起来是支持的。

| # | 位置                                            | 问题                                                                                                                                                         |
| - | --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 5 | `encodeMov:2100-2106` → `encodeMovMemImm`     | `mov qword [base+index*scale+disp], imm` 被路由到只处理 `[base+disp]` 的函数，**索引寄存器被静默丢弃**，编码成 `[base+disp]`。正确做法：带 index 时走 SIB 路径，或直接报错                           |
| 6 | `arithCode["test"]` + `encodeArith:2267-2279` | `test r64, imm` 的立即数形式使用 `0x83 /0`——这是 **ADD** 的编码（test 的立即数形式应为 `F7 /0 ib/id`）。`test rax, 5` 会被汇编成 `add rax, 5`，标志位语义完全不同                                 |
| 7 | `encodeBit:2873-2875` + `emitOpRM` RIP 分支     | `bt qword [rip+sym], imm8` 在 disp32 之后还有 1 字节 imm8，但 fixup 未设 `ripAdj=1`（对比 `mov [rip+sym],imm8` 在 `asm.go:2093` 特意设置了 `ripAdj:1`），rel32 会整体差 1 字节 → 目标地址错 |

建议：为 5、6 增加"形式未支持则报错"的守卫（第 5 处至少把 `memIndex >= 0` 的情形拒绝），为 7 补 `ripAdj`。并考虑给 goa 加一条"按已知 goc 发射模式穷举"的编码回归测试，防止 codegen 未来开始发射这些形式时静默踩雷。

---

## P3 设计/一致性/文档问题

### 8. README 严重滞后于实现（已实测）

`README.md:197-198` 仍写着"没有 `long long`、VLA、复合字面量、`_Generic` 等 C99+ 特性"、"没有数组指定初始化器 `[i] = v`"，而：

- 实测一段同时使用 `long long`、`_Generic`、复合字面量 `(int[]){...}`、指定初始化器 `{[1]=7}` 的程序，goc **全部编译通过且运行结果与手算完全一致**；
- `docs/c23-roadmap.md` 明确标记这些特性已完成；
- README 中"没有 scanf/文件 I/O/math.h/time.h"也与 `6ffec3c`（FILE-based stdio、fopen 家族）等提交矛盾。

README 是该项目唯一的对外说明，建议全面对齐 roadmap 与当前代码（文档任务，不涉及实现）。

### 9. `varTypes` 跨编译单元泄漏（代码证据）

`Parse()`（`parser.go` 顶部）重置 `typedefs`/`structs`/`enumConsts` 三张全局表并注释"tables must start fresh"，但**漏了 `varTypes`**。goclib 的 TU 先于用户 TU 编译，若两个 TU 出现同名变量，后一个 TU 的 `typeof(name)` 等解析可能命中前一个 TU 留下的过期类型。低危但属真实状态泄漏，且与同段代码的意图直接矛盾。

### 10. `#include <缺失头>` 静默跳过（设计使然，但风险大）

`cpp.go` 对磁盘上找不到、内嵌集合里也没有的 `<file>` 头打印 `note: skipping unavailable system header` 并继续编译。拼错头名（如 `#include <stdioo.h>`）会静默通过，相关符号全部按"未声明 extern int 函数"推测——错误被推迟到链接期甚至运行期。gcc 对缺失头是硬错误。对"帮助移植第三方代码"这是特性，但对日常使用这是隐患；建议至少增加 `-Werror` 级别或提示开关。

### 11. `#if` 条件中除零静默（代码证据）

`cpp.go` 常量表达式 `ceMul`：`case '/': if right != 0 { left /= right }` —— `#if (5/0)` 得到 5（非零真值），gcc 对 `#if 1/0` 直接报错。`#if` 里除零属错误输入，应报错而非静默延续。

### 12. 类型系统过度宽松（知情取舍，但面很大）

- `typesEqual` 把所有整数视为相等（`int*` 与 `char*` 可互相赋值，roadmap 有专门章节记录这一取舍的教训——`_Generic` 曾因此把 `int*` 误配给 `char*`）；
- 任意整数可直接赋给指针（`dst.IsPtr() && src.IsIntClass() → true`，标准只允许整数常量 0/NULL 或显式转换）；
- **未声明的函数调用被静默接受**为返回 int 的 extern（`check.go`），C23 已删除隐式函数声明——`prinf(...)` 这类拼写错误要等到汇编/链接阶段符号未定义才暴露，且若拼错的名字恰好命中某 goclib 函数则完全无感。

前三项可以接受（玩具编译器的取舍），第 4 项建议至少给 warning。

### 13. 其他小问题（低危）

- **枚举类型身份丢失**：所有 `enum` 建模为 int，`enum A` 与 `enum B` 可互赋不报错，`enum E : long` 底层类型被忽略（已知简化）；
- **`_BitInt` 上限不一致**：`types.go` 注释写 1..4096，parser 允许到 4194304，字面量 `pushBig` 上限 4096；超大宽度（如 `_BitInt(4194304)`，单值 512KB）的帧分配与 PE 64MiB 栈预留配套，但建议把注释对齐并考虑对极端宽度给实现上限错误；
- **嵌套 case 标签**：`switch` 内层块中的 `case` 标签 checker 放行（`swDepth > 0`），codegen 报 "case label outside a switch"——报错文案与实际不符（C 标准允许嵌套块中的 case 标签，虽然跳入块属 UB）；
- **`-ofoo` 形式**：gcc 兼容的 `-ofoo`（粘连形式）被静默忽略，只有 `-o foo` / `-o=foo` 生效；
- **`#define F()` 空参调用**：`F()` 对需要 1 个参数的宏也接受（args=[[]]），标准是错误；
- **仓库卫生**：`.cache/` 286.5 MB / 3113 个文件、`tmp/` 626 个文件堆在工作目录（gitignore 已覆盖，但建议定期清理或移出到系统临时目录）；
- **样式**：`goa/asm.go:1657` 有一处 tab 换行格式瑕疵；`lexer.go` 数字分支缩进混用 tab/空格。

---

## 测试与门禁状况

- `go test ./src`：**1 失败**（`TestOsKeepsCalls`，即 P1#1）——在已提交的 HEAD 上红着，`run_tests.sh` 会执行它，所以整个回归门禁当前是红的；
- `go test ./src/goa`：全过（编码器测试覆盖到位，含 16 种条件码派生、REX/spl 规则等）；
- `gocregress`（roadmap 记载 456 pass/1 fail）与 `run_tests.sh` 的 golden 腿是行为级回归，内联失效不影响它们（-O1 与 -Os 行为趋同），**所以这个 bug 只有单元测试能抓住——而门禁正好红着没修**，这个教训值得记入流程（"重构前导码后必须跑 go test"）。

**测试空白（本次 bug 漏网的直接原因）**：

- 无 `-MF/-MT/-MQ` 的 CLI 测试（P2#3 漏网）；
- 无 BOM 输入测试（P2#4 漏网）；
- 无复合赋值副作用语义测试（P1#2 漏网）；
- 无"前导码形状变化后内联仍生效"的回归测试（P1#1 靠现有测试抓住但门禁未保持绿）。

## 正面评价（值得保持的部分）

1. **优化 pass 的保守性哲学**：`constProp`/`deadStores`/`livenessDSE` 都只对编译器自己发射的少数指令形状做知识传递，其余一律作废（"guessing x86 semantics is how this project ended up retiring a hand-written interpreter"），并在注释里逐条列出失效规则——这是正确的编译器工程姿态。
2. **goa 编码器**：REX 发射规则（spl/bpl 强制 REX、窄存储 C6 vs C7 的 #81 教训、movsxd 独立编码、SSE 的 cvttsd2si 反向操作数特判）都正确且留有注释；SIB/ModRM 规划在 `[rbp]`/`[rsp]`/无基址绝对地址等边界上处理无误。
3. **PE/ELF 写出器**：合并段布局、TLS 目录（必须写 ImageBase+VA 的教训）、ELF 符号表/节表不参与加载映像的划分，推理都清晰；`p_memsz` 含 .bss、单 PT_LOAD 的取舍都有说明。
4. **ABI 细节**：Windows shadow space、SysV 无 shadow 时的 variadic 拷贝顺序、XMM 参数编号差异、16 字节对齐、`__chkstk` 式探测循环——处理完整且注释到位。
5. **历史 bug 注释**：错误信息常带成因（如 `#81 _Bool/char zero-init bug`），对维护者极其友好。
6. **测试精度**：优化 pass 的单元测试断言到具体指令序列，ISA 测试覆盖条件码全家，golden 腿对 -O0/-O1/-Os 三档并行比对——测试文化明显高于平均水平。

## 修复优先级建议

1. **立即**（1 个 PR 可同时处理）：P1#1 内联形状检查 + 保持 `go test` 全绿；P2#3 补 `-MF/-MT/-MQ` case；P2#4 跳 BOM。
2. **尽快**：P1#2 复合赋值左值单次求值（语义正确性）；P3#5/#6/#7 三个编码器守卫（至少把未支持形式改成报错）。
3. **排期**：P3#8 README 对齐；#10 缺失头告警开关；#9 varTypes 重置。
4. **可选**：枚举身份、`_BitInt` 上限对齐、`#if` 除零报错、嵌套 case 文案修正。

---

## 修复与验证状态（2026-10-02）

本报告所列 P1/P2/P3 已验证 bug 已全部修复，每个修复配有单元测试；`go test ./src` 与 `go test ./src/goa` 全绿，端到端复现全部通过。基线：`go test ./src` 修复前 1 失败（TestOsKeepsCalls，即 P1#1）、`go test ./src/goa` 全过。

### P1（编译正确性）
- **#1 -O1 内联恒失效**（`codegen.go`）：`extractInlineCand` 前导码形状检查改为在 `mov rbp,rsp` 后跳过 1..4 个 callee-save `push`（按 `calleeSaveRegs` 判断）再要求 `sub rsp,N`；模板构建同步跳过 callee-save restores；`inlineCalls` 的 `b.regular` 检查同样跳过。此前所有候选 `regular=false`，`inlineCalls` 为死代码、-O1≡-Os。
  - 测试：`TestO1InlineSkipsCalleeSavePushes`（-O1 内联 / -Os 保留 2 处 call）+ 既有 `TestOsKeepsCalls`。
- **#2 复合赋值双重求值**（`ast.go`/`parser.go`/`check.go`/`codegen.go`）：`AssignExpr` 增 `Op` 字段，parser 不再脱糖为 `E1 = E1 op E2`；codegen 新增 `genCompoundAssign`（标量：快路径 loadVar/storeVar，一般路径地址+旧值各求值一次并停放帧槽，经内部节点 `TmpLoad` 复用旧值做算术后写回）、`genBigCompoundAssign`（_BitInt）。`TmpLoad.Slot` 语义为帧偏移（`tmpSlot` 输出），直接内存读取。
  - 测试：`TestCompoundAssignEvalOnce`（`a[i++] += 10` 中 `i++` 恰好一次）、`TestCompoundAssignScalarFastPath`。端到端：修复前 goc 退出 212、gcc 1121；修复后 1121，与 gcc 一致。

### P2（可复现）
- **#3 -MF/-MT/-MQ 参数死代码**（`main.go`）：从 default 忽略清单移入显式 case 并消费分离值，依赖文件名不再落入 `cfg.inputs`（此前 `open deps.d: file not found`）。
  - 测试：`TestMFValueNotTreatedAsInput`（分离式与 `-MF=path` 两种形态）。注：写 .d 依赖文件属功能新增，不在本 bug 范围，未实现。
- **#4 BOM 不跳过**（`cpp.go`）：`spliceContinuations` 开头 `TrimPrefix("\xEF\xBB\xBF")`，主 TU 与 include 均覆盖。
  - 测试：`TestBOMPrefixedSource`。端到端：BOM 源程序正常编译运行（退出 9）。
- **#5 varTypes 跨 TU 泄漏**（`parser.go`）：`Parse()` 顶部补 `varTypes` 重置（与 typedefs/structs/enumConsts 一致）。
  - 测试：`TestVarTypesResetAcrossTUs`（TU1 声明 `v` 后，TU2 的 `_Alignof(v)` 必须报类型未知）。
- **#6 #if 除零静默**（`cpp.go`）：Preprocessor 增 `ceErr`，`ceMul` 的 `/`、`%` 除零置位，`#if`/`#elif` 检查后报 `division by zero in #if/#elif expression`。
  - 测试：`TestIfDivideByZeroErrors`（`#if 1/0` 与 `#elif 2/0` 两条路径）。端到端：修复前静默取 0，修复后报错退出。

### P3（goa 汇编器潜伏错码）
- **#5 mov mem, imm 丢索引寄存器**（`goa/asm.go`）：`encodeMovMemImm` 改收完整 `Operand`，经 `planMem` 编码 base/index/scale/disp（原签名只收 base+disp，`[rax+rbx*4]` 静默编成 `[rax]`）。
  - 测试：`TestMovMemImmKeepsIndexScale`（含高基址寄存器 + 索引的 SIB 断言，及 base+disp 旧形态字节不变）。
- **#6 test r64, imm 被编成 add**（`goa/asm.go`）：`encodeArith` 加 `mnem=="test"` 特判 → F6 /0 ib / F7 /0 id（83/81 组 /0 是 ADD）。
  - 测试：`TestTestImmUsesTestOpcode`（imm8/imm32/高寄存器三形态）。
- **#7 bt [rip+sym], imm8 fixup 缺 ripAdj**（`goa/asm.go`）：`emitOpRM` RIP 分支在存在 tail（指令尾随 imm8）时 fixup 记 `ripAdj:1`，disp32 后的 trailer 计入 RIP 基准。
  - 测试：`TestBTRipImmFixupHasRipAdj`（ripAdj=1 + 字节布局）。

### 验证汇总
- `go build`（src 与 src/goa）：exit 0；`bin/goc.exe`、`bin/goa.exe` 已重建。
- `go test ./src`：**全绿**（含既有全套 + 新增 7 个回归测试）。
- `go test ./src/goa`：**全绿**（含既有全套 + 新增 3 个回归测试）。
- 端到端（`bin\goc.exe` 实编译实运行）：复合赋值 1121 ✓；BOM 源退出 9 ✓；`-MF deps.d` 不再报 file not found、程序退出 7 ✓；`#if 1/0` 报错拒绝 ✓；`-O1` 内联程序运行结果正确（退出 15）✓。

### 未做（明确在范围外）
README 同步、`-ofoo` 粘连形态、`#define F()` 空参、缺头告警开关、枚举身份、`_BitInt` 上限对齐、嵌套 case 文档修正等排期/可选事项未改动；`-MF` 未实现写 .d 依赖文件（属功能新增）。
