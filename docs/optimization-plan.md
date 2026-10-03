# goc 性能优化方案（optimization-plan）

> 版本：2026-10-03（v3 全面刷新：基于 HEAD=edb3738 全量实测，更新现状/已完成/剩余机会/目标口径） · 状态：方案（执行中） · 适用范围：`src/` 代码生成与优化管线  
> 配套实测：`bench/optprobe.c`、`bench/bench2.c`（见文末）；本版数据全部为本机 2026-10-03 实测

---

## 1. 现状盘点（实测，2026-10-03）

### 1.1 优化管线全景（已成型，非待建）

goc 仍是**单趟架构**（`parser → AST → check → codegen`，codegen 产出线性 `Inst[]` 文本指令流），但自 v2 方案以来已落地大量优化基础设施：

- **pass 链**（`codegen.go` Gen()，`c.opt >= 1`）：`inlineCalls` → `constProp`（真删除+消费点内联）→ `peepholeIR` → `deadStores` → `livenessDSE` → `elimRedundantExt`（shl/sar 32 对折叠）→ `slotCache`（槽 load/store 转发）→ `algebraicIdent`（cmp 归零→test）→ `sibFold`（SIB 索引寻址折叠）
- **寄存器分配**：`int` 局部静态分配 callee-save（rbx/r12/r13/r14），T2.1 扩展了参数寄存器化 + leaf 函数扩池（r8/r9）
- **int 承载模型**：C1-C5 完成——int 算术 32 位承载（goa 宽度修复 + 窄化点 N1-N23），回绕对从 237 → 11 条
- **分析器**：`ptrcap.go`（int 变量持 64 位指针时抑制 movsxd，按值不按生命周期标记）
- **轻量 CFG**：`liveness.go` 基本块段切分 + 槽活跃分析（服务 livenessDSE）；非完整 CFG（缺 succ/pred/loop）

### 1.2 实测数据（Windows，gcc 16.2.0 -O2 对照，5 次中位数）

**bench2.c 整体**（fib(35) + 10000 冒泡，`-O2`）：

| 版本 | goc 耗时 | gcc 耗时 | 相对 gcc | 备注 |
| --- | --- | --- | --- | --- |
| v2 方案基线（2026-10-02） | 340 ms | 33.9 ms | 10.0× | 优化前 |
| F1+F2 后（3fe37b9） | 244 ms | — | — | 组织者实测 |
| **当前（edb3738）** | **238 ms** | **~44.7 ms** | **≈5.3×** | 本机同口径复测 |

**工作负载拆分**（决定优化优先级的关键数据）：

| 负载 | goc | gcc | 差距 | 特征 |
| --- | --- | --- | --- | --- |
| fib(35)×3（递归） | **186 ms** | 41 ms | **4.5×** | 29.86M 次调用，**调用开销主导**（每调用 14 条 prologue/epilogue+参数装载） |
| bsort 10000（冒泡） | **163 ms** | 31 ms | **5.3×** | ~50M 次比较，**索引计算 + 循环边界重算 + 内存流量主导** |

**静态指标口径修正（重要）**：
- bench2 asm 指令行 9427 → **1593**（edb3738 的 `vfmt_lite` 按 call graph 裁掉了浮点格式化机器：vfmt/__goclib_double_*/frexp/log/log10/fmod 全部出图，movsd 604→0）。**bench2 指令数已不再代表"应用+库"全貌**，不再适合作为静态代理指标。
- `optprobe.c`（完整 vfmt 路径）仍稳定：**9288 条**——继续作为静态指标主基准。

## 2. 已完成（截至 HEAD=edb3738，全部落库）

| 阶段 | 提交 | 内容 | 实测收益 |
| --- | --- | --- | --- |
| Tier 1 | 69decc6→07f1289 | 级别分层 / constProp 真删除 / 代数恒等（比较归零）/ 槽缓存 / 零初始化（.bss 已覆盖）/ elimRedundantExt | 340→324ms（指令 10043→9954） |
| T1.6 远期 C1-C5 | a6500f8→bf3c143 | int 算术 32 位承载改造（goa 宽度修复 + N1-N23 窄化 + ptrCapable） | 324→303ms（回绕对 237→11） |
| T2.1 R1-R3 | 9a99b78 | 参数寄存器化 + leaf 函数扩池 r8/r9 | 9571 行（-215 条） |
| F1+F2 | 3fe37b9 | SIB 索引寻址折叠 + 索引寄存器直用 | 9571→9427 行，274.6→244ms（-11%） |
| F2 窄索引 | 4a8db3f | char/short 下标符号扩展修正 | 正确性修复 |
| vfmt_lite | edb3738 | 嵌入式 printf 精简路径（printf("%d") 省 82%） | bench2 asm 9427→1593（裁库） |
| -rt/-rtdiag | c82b993 | 编译期可选堆诊断运行时（红区/越界/崩溃转储） | 功能（默认零开销） |
| parser | 17e633c | 全局变量逗号分隔 declarator | 语言补全 |

**F3 copyProp（回退未提交）**：完整尝试（挂起 mov + 链替代 + 死拷贝删除），调试中修复 10 个正确性 bug，mov 链-only 形态实测 -15.6% 耗时；但**挂起+删除在 -O1/-Os 用户复杂函数仍破坏**（28 个回归失败），静态推演无法预算内定位——**回退干净**（HEAD=3fe37b9，即 F1+F2 态），全部工作存 `scratch\f3_copyprop_work.diff`。结论：跨指令"挂起延迟+删除"机制不可靠，需重设计（见 §3 F3-R）。

## 3. 剩余机会清单（实测支撑，按优先级）

### P0 — 立即（探针/设计已就绪）

| # | 优化项 | 原理 | 预期收益 | 风险/依赖 |
| --- | --- | --- | --- | --- |
| **F4** | **slotCache 值快照** | 现状：`mov [s], r` 后 r 被改即丢转发（错误 kill——内存已 store，值不受源寄存器影响）。改"值快照"语义：记录拷贝链根（`mov rax,r12` 建立别名），load 时别名根存活则转发 | mov/load 削减，中 | 中。T1.4 踩坑史（linux register-arg spills 致 sum7 错算、ar_print/brace 等 8 示例曾失败）——PE/ELF 双目标回归，探针统计"store 后源改但 load 转发失败"形态 |
| **F3-R** | **拷贝消除重设计（基本块本地）** | F3 教训：跨指令挂起不可靠。改为**同基本块、无分支/call 干扰**的 mov 链折叠（替代照做、删除只限相邻指令），杜绝跨指令重排 | 静态 -1~2%（mov 4288 条/占 45% 中的 reg-reg 链） | 低（局部窗口，借鉴 F3 已修的 10 个 bug 清单） |

### P1 — 下一批（本轮实测数据直接支撑）

| # | 优化项 | 原理 | 预期收益 | 风险/依赖 |
| --- | --- | --- | --- | --- |
| **T2.2** | **尾调用优化（TCO）** | `return fib(n-1)` 尾位置 → 参数重排后 `jmp fib`（省 14 条/调用的 prologue/epilogue+装载）。fib(35) 29.86M 次调用中一半是尾位置 | **fib 4.5×→2.5-3×**，bench2 整体 -15~20%（238→~200ms） | 中（栈参数重排、与 T2.1 交互；先做"call 后立即 ret"形状识别） |
| **LICM** | **循环不变代码外提** | bsort 内层循环 `j < 9999-i` 每次迭代重算（读 i / 读 9999 / sub / cmp 4 条）；`a` 基址、`9999-i` 边界可外提 | **bsort 5.3×→~4×**，bench2 整体再 -10~15% | 中（需轻量循环识别——可先做"函数级局部外提"，不等完整 CFG） |
| **IVS** | **索引强度削减（并入 LICM）** | 数组访问 `movsxd r,idx; mov x,[base+r*4]` 在循环里 → 地址累加（lea 增量） | bsort 内层每访问再省 1-2 条 | 中（与 F1 SIB 叠加；指针别名纪律） |

### P2 — 顺手项

| # | 优化项 | 原理 | 预期收益 | 风险/依赖 |
| --- | --- | --- | --- | --- |
| F5 | 参数装载直算 | `lea rax,[s]; mov [s2],rax; mov rcx,[s2]` → `lea rcx,[s]`（装载直接用 ABI 目标寄存器） | 每 call -1~2 条 | 低 |
| F6 | 内部调用免影子空间 | 自家函数不从 [rsp+8..24] 读参 → 省 `sub rsp,32; add rsp,32` | 每 call -2 条 | 低/中（先确认 goclib 与全部内部函数无依赖；外部 CRT/API 调用保留） |
| F7 | 死常量增强 | 32 位 `mov eax,X` 零扩展=全 64 位覆写 → isDeadConstDef 视为 redef，前置常量 def 可删 | 小 | 低 |
| T2.4 | 分支优化 | jmp 链消除、jcc→jmp 合并、相邻块合并、不可达块删除 | 中（循环体瘦身） | 低（局部窗口版可不依赖完整 CFG） |

### P3 — 远期

| # | 优化项 | 说明 |
| --- | --- | --- |
| T1.3 余 | 算术恒等剩余（×0/×1/±0/×2^k→lea） | 发射层改造，暂缓 |
| T2.3 | CFG 升级（succ/pred/loop） | 为 LICM/GVN/完整寄存器分配打地基 |
| T3.3 | 内建指令映射（popcnt/lzcnt 等） | 位操作内建 → 单条指令 |
| T3.1 | 完整寄存器分配（线性扫描/图着色） | 14 GP；需 CFG+活跃区间 |

> 设计决策不变：**暂不引入 SSA**（单趟线性 Inst[] + 轻量 CFG 规模下，SSA 是后端重写，与 goc 子集规模不成比例；GVN/CSE 可用基于哈希的值编号替代）。

## 4. 目标（基于新基线）

| 阶段 | 目标（goc -O2 vs gcc -O2，bench2.c） |
| --- | --- |
| v2 方案目标（Tier1 后） | 5-7×（170-230ms）——**已达成上沿**（现 5.3× / 238ms，拆分测下 fib 4.5× 已优于该档） |
| **下一阶段（P0+P1）** | **~4×（175-190 ms）**：F4+F3-R 收 mov 冗余 → TCO 收 fib 调用 → LICM 收 bsort 边界重算 |
| 中期（P1+P2） | 3-4×（130-150 ms） |
| 远期（P3） | ≤2× |

**拆分目标**：fib 4.5×→2.5-3×（TCO）；bsort 5.3×→3.5-4×（LICM+IVS）。

## 5. 实施顺序与依赖

```
P0: F4 值快照（组织者探针续跑） ──┐
P0: F3-R 基本块本地拷贝消除 ─────┤（F4 完成即启动，互不依赖）
                                 │
P1: T2.2 TCO ────────────────────┤（递归热点，独立）
P1: LICM+IVS（bsort 侧） ────────┘（先函数级局部版，不等完整 CFG）
P2: F5/F6/F7 ─── T2.4 分支优化（局部窗口版）
P3: T2.3 CFG → T3.x（按需）
```

**建议的下一落地组合**：**F4（值快照）+ T2.2（TCO）并行**——F4 是 mov 削减（数据流），TCO 是调用削减（控制流），互不干扰、各有实测收益；F3-R 紧随 F4（复用其拷贝链基础设施）。LICM 需要轻量循环识别，建议放在 TCO 之后（bsort 侧收益独立）。

## 6. 基准与验收方法论

### 6.1 基准集

| 文件 | 负载 | 角色 |
| --- | --- | --- |
| bench2.c | fib(35) + 10000 冒泡 | 整体基准（耗时） |
| optprobe.c | fib(30) + 100 万循环 | 静态指标（asm 指令行，完整 vfmt 路径） |
| bench\_build/split_fib.c、split_bsort.c | 拆分负载 | 分项定位（TCO/LICM 收益归因） |
| 待补：bench_str.c、bench_loop.c | 字符串/嵌套循环 | v2 遗留，按需补 |

### 6.2 指标（口径修正后）

- 运行耗时：5 次中位数，同 exe 对比；整体 + 拆分（fib/bsort）双口径
- 静态指标：**optprobe asm 指令行**（主）、bench2 应用函数域指令行（辅，库函数随 vfmt_lite 形态变化，不做跨版本硬对比）
- 正确性：输出与 gcc -O2 对拍逐字节一致；`go test ./src ./src/goa` 全绿；gocregress 463/0 不回退；tests\cstd 60 PASS/0 MISMATCH；tests\c23 49 PASS 无新回归；src/examples 与 expected 一致

### 6.3 每个 pass 的验收清单

1. 独立开关可单独关闭；2. 单元测试覆盖触发/失效形状（label、内联 asm、间接写、flag 边界、32/64 位、子寄存器）；3. gcc 对拍一致；4. 指令数只降不升（内联类除外）——静态断言；5. 双目标（PE/ELF）回归（F4/F6 等涉及 ABI 形态的 pass 必查）。

## 7. 风险登记（增量）

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| F4 值快照误转发（T1.4 踩坑史） | 中 | 拷贝链根失效纪律；sum7/ar_print/brace 等历史失败用例回放；双目标回归 |
| TCO 与参数寄存器化交互 | 中 | 先识别"call 后立即 ret"最简形状；栈参数重排单测；-Os 跳过 |
| LICM 缺别名分析 | 中 | 只外提纯值（不变式由寄存器/槽承载、无副作用）；数组基址外提需确认无别名写 |
| F6 免影子空间 | 中 | 先全库扫描 [rsp+8..24] 引用；外部 CRT/API 调用保留 |
| 指令数口径漂移（vfmt_lite） | 低 | 静态基准固定在 optprobe；bench2 按函数域统计并注明 |

## 8. 附录：实测复现（2026-10-03 口径）

```powershell
cd D:\projects\goc
$env:GO111MODULE='off'; $env:HOME="$env:TEMP"; $env:GOCACHE="$env:TEMP\gocache"
go build -o bin\goc.exe ./src
# 整体：副本目录编译 + 5 次中位计时
Copy-Item bench\bench2.c bench\_build\ -Force; Set-Location bench\_build
& D:\projects\goc\bin\goc.exe -O2 bench2.c
(1..5 | ForEach-Object { $s=[Diagnostics.Stopwatch]::StartNew(); .\bench2.exe | Out-Null; $s.Stop(); $s.ElapsedMilliseconds }) | Sort-Object | Select-Object -Index 2
# gcc 对照
& D:\msys\ucrt64\bin\gcc.exe -O2 -o gcc_bench2.exe bench2.c
# 拆分（fib/bsort）见 bench\_build\split_fib.c / split_bsort.c
```

> 数据口径：指令行 = asm 中 tab 缩进、非点开头的行；耗时 = 5 次中位数含进程启动（两端同口径，可比）。
