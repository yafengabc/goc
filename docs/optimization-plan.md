# goc 性能优化 Roadmap 与进度（optimization-plan）

> 版本：2026-10-03 v4（整合 Roadmap 批次化 + 进度台账） · 状态：**执行中** · 适用范围：`src/` 代码生成与优化管线  
> 配套实测：`bench/optprobe.c`、`bench/bench2.c`、`bench/_build/split_*.c`；本版数据全部为本机 2026-10-03 实测（HEAD=edb3738）

---

## 0. 总览（一眼看全貌）

**进度带**：现状 **5.3× / 238ms** → P0（F4/F3-R，进行中）→ P1（TCO/LICM，目标 ~4× / 175-190ms）→ P2（F5-F7/分支，3-4×）→ P3（CFG/内建，≤2×）

| 批次 | 项目 | 状态 | 提交/备注 | 实测收益 |
| --- | --- | --- | --- | --- |
| ✅ Tier 1 | T1.1-T1.6 六项 | 完成 | 69decc6→07f1289 | 340→324ms（指令 10043→9954） |
| ✅ T1.6 远期 | C1-C5 int 32 位承载改造 | 完成 | a6500f8→bf3c143 | 324→303ms（回绕对 237→11） |
| ✅ T2.1 | R1-R3 参数寄存器化+leaf 扩池 | 完成 | 9a99b78 | 9571 行（-215 条） |
| ✅ F1+F2 | SIB 索引寻址+索引寄存器直用 | 完成 | 3fe37b9 | 274.6→244ms（-11%），9571→9427 行 |
| ✅ F2 窄索引 | char/short 下标修正 | 完成 | 4a8db3f | 正确性 |
| ✅ 功能 | vfmt_lite / -rt / parser 逗号 | 完成 | edb3738 / c82b993 / 17e633c | 嵌入式 printf 省 82%；堆诊断运行时 |
| 🔴 F3 | copyProp 跨指令挂起 | **回退** | scratch\f3_copyprop_work.diff | 完整尝试后回退（见 §3） |
| 🟡 P0 | F4 slotCache 值快照 | **进行中** | 探针就绪，组织者待续 | 预期 mov/load 削减 |
| ⬜ P0 | F3-R 拷贝消除（基本块本地重设计） | 待做 | 依赖 F4 拷贝链基础设施 | 预期静态 -1~2% |
| ⬜ P1 | T2.2 尾调用优化（TCO） | 待做 | fib 29.86M 调用 | 预期 fib 4.5×→2.5-3× |
| ⬜ P1 | LICM+IVS 循环外提/强度削减 | 待做 | bsort 边界重算 | 预期 bsort 5.3×→~4× |
| ⬜ P2 | F5 参数装载 / F6 免影子空间 / F7 死常量 / T2.4 分支 | 待做 | — | 每项小-中 |
| ⬜ P3 | T1.3 余 / T2.3 CFG / T3.3 内建 / T3.1 完整寄存器 | 待做 | — | 远期 |

---

## 1. 现状盘点（实测，2026-10-03）

### 1.1 优化管线全景

goc 是**单趟架构**（`parser → AST → check → codegen`，codegen 产出线性 `Inst[]` 文本指令流），自 v2 方案以来已落地：

- **pass 链**（`codegen.go` Gen()，`c.opt >= 1`）：`inlineCalls` → `constProp`（真删除+消费点内联）→ `peepholeIR` → `deadStores` → `livenessDSE` → `elimRedundantExt` → `slotCache` → `algebraicIdent` → `sibFold`
- **寄存器分配**：`int` 局部静态分配 callee-save（rbx/r12/r13/r14）；T2.1 扩展参数寄存器化 + leaf 扩池（r8/r9）
- **int 承载模型**：C1-C5 完成——int 算术 32 位承载（goa 宽度修复 + 窄化点 N1-N23），回绕对 237→11
- **分析器**：`ptrcap.go`（int 持 64 位指针时抑制 movsxd）
- **轻量 CFG**：`liveness.go` 基本块段切分+槽活跃分析；非完整 CFG（缺 succ/pred/loop）
- **库裁剪**：`vfmt_lite` 按 call graph 裁 printf 浮点路径（嵌入式目标）

### 1.2 实测数据（Windows，gcc 16.2.0 -O2 对照，5 次中位数）

**bench2.c 整体**（fib(35) + 10000 冒泡）：

| 版本 | goc | gcc | 相对 | 备注 |
| --- | --- | --- | --- | --- |
| v2 基线（10-02） | 340 ms | 33.9 ms | 10.0× | 优化前 |
| F1+F2 后（3fe37b9） | 244 ms | — | — | 组织者实测 |
| **当前（edb3738）** | **238 ms** | **~44.7 ms** | **≈5.3×** | 本机同口径复测 |

**工作负载拆分**（决定优先级）：

| 负载 | goc | gcc | 差距 | 特征 |
| --- | --- | --- | --- | --- |
| fib(35)×3 | **186 ms** | 41 ms | **4.5×** | 29.86M 次调用，**调用开销主导**（每调用 ~14 条 prologue/epilogue+参数装载） |
| bsort 10000 | **163 ms** | 31 ms | **5.3×** | ~50M 次比较，**索引计算+循环边界重算+内存流量主导** |

**静态指标口径修正**：edb3738 的 `vfmt_lite` 使 bench2 asm 9427→**1593**（浮点格式化机器全裁出图，movsd 604→0）——bench2 指令数不再代表"应用+库"全貌；**optprobe（9288 条，完整 vfmt 路径）作为静态主基准**。

---

## 2. Roadmap（批次化）

> 每个 pass 的通用验收（§5.3）：独立开关、单测覆盖触发/失效形状、gcc 对拍、指令数只降不升、双目标（PE/ELF）回归、四件套红线。

### 批次 P0 — 进行中/立即（mov 冗余削减）

| # | 项目 | 原理 | 实施点 | 预期收益 | 风险 | 状态 |
| --- | --- | --- | --- | --- | --- | --- |
| **F4** | **slotCache 值快照** | 现状 `mov [s], r` 后 r 被改即丢转发（错误 kill——内存已 store 不受源寄存器影响）。改值快照语义：记录拷贝链根（`mov rax,r12` 建别名），load 时别名根存活则转发 | `src/opt.go` slotCache | mov/load 削减，中 | **中**——T1.4 踩坑史（linux register-arg spills 致 sum7 错算、ar_print/brace 等 8 示例曾失败）；PE/ELF 双目标回归 | 🟡 探针就绪，组织者待续 |
| **F3-R** | **拷贝消除重设计（基本块本地）** | F3 教训：跨指令挂起不可靠。改为同基本块、无分支/call 干扰的 mov 链折叠（替代照做、删除只限相邻指令） | `src/opt.go` 新 pass | 静态 -1~2%（mov 4288 条占 asm 45%，其中 reg-reg 链） | 低（局部窗口；复用 F3 已修 10 bug 清单） | ⬜ 依赖 F4 拷贝链基础设施 |

### 批次 P1 — 下一批（实测直接支撑）

| # | 项目 | 原理 | 实施点 | 预期收益 | 风险 | 状态 |
| --- | --- | --- | --- | --- | --- | --- |
| **T2.2** | **尾调用优化（TCO）** | `return fib(n-1)` 尾位置 → 参数重排后 `jmp fib`（省 14 条/调用的 prologue/epilogue+装载）。fib(35) 29.86M 调用中一半是尾位置 | codegen 函数 epilogue 识别"call 后立即 ret"形状 | **fib 4.5×→2.5-3×**，bench2 整体 -15~20% | 中（栈参数重排、与 T2.1 交互；-Os 跳过） | ⬜ |
| **LICM+IVS** | **循环不变外提 + 索引强度削减** | bsort 内层 `j < 9999-i` 每次迭代重算 4 条（读 i/读 9999/sub/cmp）；`a` 基址、`9999-i` 可外提；数组访问可地址累加 | 先函数级局部外提（不等完整 CFG） | **bsort 5.3×→~4×**，bench2 再 -10~15% | 中（只外提纯值；基址外提需别名纪律） | ⬜ |

### 批次 P2 — 顺手项

| # | 项目 | 原理 | 实施点 | 预期收益 | 状态 |
| --- | --- | --- | --- | --- | --- |
| F5 | 参数装载直算 | `lea rax,[s]; mov [s2],rax; mov rcx,[s2]` → `lea rcx,[s]` | codegen 调用装载序列 | 每 call -1~2 条 | ⬜ |
| F6 | 内部调用免影子空间 | 自家函数不从 [rsp+8..24] 读参 → 省 `sub rsp,32; add rsp,32` | codegen 调用序列（先全库扫描确认无依赖；外部 CRT/API 保留） | 每 call -2 条 | ⬜ |
| F7 | 死常量增强 | 32 位 `mov eax,X` 零扩展=全 64 位覆写 → isDeadConstDef 视为 redef | constProp | 小 | ⬜ |
| T2.4 | 分支优化 | jmp 链消除、jcc→jmp 合并、相邻块合并、不可达块删除 | 局部窗口版（可不依赖完整 CFG） | 中（循环体瘦身） | ⬜ |

### 批次 P3 — 远期

| # | 项目 | 说明 |
| --- | --- |
| T1.3 余 | 算术恒等剩余（×0/×1/±0/×2^k→lea） | 发射层改造，暂缓 |
| T2.3 | CFG 升级（succ/pred/loop） | 为 LICM/GVN/完整寄存器分配打地基 |
| T3.3 | 内建指令映射（popcnt/lzcnt 等） | 位操作内建→单条指令 |
| T3.1 | 完整寄存器分配（线性扫描/图着色） | 14 GP；需 CFG+活跃区间 |

> 设计决策不变：**暂不引入 SSA**（单趟线性 Inst[] + 轻量 CFG 规模下，SSA 是后端重写；GVN/CSE 用哈希值编号替代）。

---

## 3. 进度台账（已完成，按提交链）

| 提交 | 阶段 | 内容 | 实测收益 |
| --- | --- | --- | --- |
| 69decc6 | T1.1 | 优化级别真实分层（-O1→1 / -O2→3 / -O3/-Ofast→4 / -Os→2） | 语义正确，为后续挂载 |
| 7f836b6 | T1.6 | elimRedundantExt：shl32/sar32 对折叠 | 符号扩展消除（10× 差距头号杀手） |
| 9644c5f | T1.4 | slotCache：槽 load 转发+冗余 store 删除 | 高——栈式模型核心流量 |
| ce84a34 | T1.3 | algebraicIdent：cmp r,0→test r,r（安全集判定） | 比较归零 |
| 07f1289 | T1.2 | constProp 真删除+消费点内联（含子寄存器/内存操作数纪律，8 示例回归教训） | 5 示例 -137 条 |
| eab6df0 | T1.6 C0 | int 承载模型探针+基线文档（T1.6_int_carry_model.md） | 基线 |
| 37e78f9 | T2.1 设计 | 寄存器扩面设计文档（T2.1_regalloc_expansion.md） | — |
| a6500f8 | C1 | int 算术 32 位承载：goa 宽度修复（21 条编码断言）+ 窄化点 N1-N23 + ptrCapable | 9857→303ms |
| 1af90f7 | C2 | inc/dec 32 位化+索引/switch 窄化 | wrapS 237→33 |
| cb4fbf2 | C3 | ABI 统一：int 参数低 32 位有效跨调用 | 对齐 mingw/gcc |
| bdd06aa | C4 | ptrCapable 分析器（按值不按生命周期；`(long)v` 4294967295 踩坑实测修复） | 抑制冗余 movsxd |
| af0abfa | C5-a | constProp 宽度化（32 位 op 不再常量传播死路） | — |
| fa59c91 | C5-b | elimRedundantExt 复验 | shl/sar 32 对→movsxd |
| bf3c143 | C5-c | 结论入库 | — |
| 9a99b78 | T2.1 R1-R3 | 参数寄存器化 + leaf 扩池 r8/r9 | -215 条 |
| 3fe37b9 | F1+F2 | SIB 索引寻址折叠 + 索引寄存器直用（F2 曾踩 tmpIdxSlot 嵌套覆盖→局部变量修复） | 9571→9427 行，274.6→244ms（-11%） |
| 4a8db3f | F2 窄索引 | char/short 下标按其位宽符号扩展 | 正确性修复 |
| 17e633c | parser | 全局变量逗号分隔 declarator | 语言补全 |
| c82b993 | 功能 | `goc -rt/-rtdiag` 编译期可选堆诊断运行时（红区/越界/崩溃转储，默认空桩零开销） | 功能 |
| edb3738 | 功能 | vfmt_lite 精简 printf 路径（scanLiteFormat 按需选路） | printf("%d") 省 82%（嵌入式） |
| 974bb48 | 文档 | optimization-plan v3 | — |

**F3 copyProp（回退记录）**：完整尝试（挂起 mov + 链替代 + 死拷贝删除），调试修复 10 个正确性 bug（子寄存器替代/writesReg 识别/文本级替换/内存操作数 flush/x86 两操作数 dstReadWrite/div 隐含 rax-rdx/emitPend 旧名/substituted 生命周期/movq-movd-cvttsd2si/pop）。mov 链-only 形态实测 -15.6% 耗时；但挂起+删除在 -O1/-Os 用户复杂函数仍破坏（28 个回归失败）→ **回退干净**，工作存 `scratch\f3_copyprop_work.diff`。结论：跨指令"挂起延迟+删除"机制不可靠 → **F3-R 重设计为基本块本地拷贝消除**。

---

## 4. 目标

| 阶段 | 目标（goc -O2 vs gcc -O2，bench2.c） |
| --- | --- |
| v2 目标（Tier1 后） | 5-7×（170-230ms）——**已达成上沿**（5.3×/238ms，fib 4.5× 已优于该档） |
| **下一阶段（P0+P1）** | **~4×（175-190ms）**：F4+F3-R 收 mov 冗余 → TCO 收 fib 调用 → LICM 收 bsort 边界重算 |
| 中期（P1+P2） | 3-4×（130-150ms） |
| 远期（P3） | ≤2× |

**拆分目标**：fib 4.5×→2.5-3×（TCO）；bsort 5.3×→3.5-4×（LICM+IVS）。

## 5. 实施顺序、依赖与验收

### 5.1 顺序

```
P0: F4 值快照（组织者探针续跑） ──┐
P0: F3-R 基本块本地拷贝消除 ─────┤（F4 完成即启动）
P1: T2.2 TCO ──────────────────┤（递归热点，独立，可与 P0 并行）
P1: LICM+IVS ──────────────────┘（先函数级局部版）
P2: F5/F6/F7 ─── T2.4 分支优化
P3: T2.3 CFG → T3.x（按需）
```

### 5.2 基准集

| 文件 | 负载 | 角色 |
| --- | --- | --- |
| bench2.c | fib(35)+10000 冒泡 | 整体基准（耗时） |
| optprobe.c | fib(30)+100 万循环 | 静态指标（asm 指令行，完整 vfmt 路径） |
| bench/_build/split_fib.c、split_bsort.c | 拆分负载 | 分项归因（TCO/LICM 收益） |
| 待补：bench_str.c / bench_loop.c | 字符串/嵌套循环 | 按需 |

### 5.3 每个 pass 的验收清单

1. 独立开关可单独关闭；2. 单元测试覆盖触发/失效形状（label、内联 asm、间接写、flag 边界、32/64 位、子寄存器）；3. gcc -O2 对拍逐字节一致；4. 指令数只降不升（内联类除外）静态断言；5. 双目标（PE/ELF）回归（F4/F6 等 ABI 形态必查）；6. 四件套红线：`go test ./src ./src/goa` 全绿、gocregress 463/0、tests\cstd 60 PASS/0 MISMATCH、tests\c23 49 PASS 无新回归、src/examples 与 expected 一致。

## 6. 风险登记（增量）

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| F4 值快照误转发（T1.4 踩坑史） | 中 | 拷贝链根失效纪律；sum7/ar_print/brace 历史失败用例回放；双目标回归 |
| TCO 与参数寄存器化交互 | 中 | 先识别"call 后立即 ret"最简形状；栈参数重排单测；-Os 跳过 |
| LICM 缺别名分析 | 中 | 只外提纯值（寄存器/槽承载、无副作用）；数组基址外提需确认无别名写 |
| F6 免影子空间 | 中 | 先全库扫描 [rsp+8..24] 引用；外部 CRT/API 调用保留 |
| 指令数口径漂移（vfmt_lite） | 低 | 静态基准固定 optprobe；bench2 按函数域统计并注明 |

## 7. 附录：实测复现（2026-10-03 口径）

```powershell
cd D:\projects\goc
$env:GO111MODULE='off'; $env:HOME="$env:TEMP"; $env:GOCACHE="$env:TEMP\gocache"
go build -o bin\goc.exe ./src
Copy-Item bench\bench2.c bench\_build\ -Force; Set-Location bench\_build
& D:\projects\goc\bin\goc.exe -O2 bench2.c
(1..5 | ForEach-Object { $s=[Diagnostics.Stopwatch]::StartNew(); .\bench2.exe | Out-Null; $s.Stop(); $s.ElapsedMilliseconds }) | Sort-Object | Select-Object -Index 2
& D:\msys\ucrt64\bin\gcc.exe -O2 -o gcc_bench2.exe bench2.c   # 对照
```

> 数据口径：指令行 = asm 中 tab 缩进、非点开头的行；耗时 = 5 次中位数含进程启动（两端同口径，可比）。
