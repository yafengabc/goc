# goc LLVM 后端路线图

> 状态：架构已定案（2026-10-04）。本文记录「全量 LLVM」后端的目标、现状、缺口与分阶段计划。
> 默认后端（goa 自研 x86-64 代码生成）**完全不受影响**，回归基线 `gocregress pass=475`（81 example × 6 腿）。

---

## 1. 架构决策

用户原话：「我感觉这么弄还不如全走 LLVM，goa 只负责连接，LLVM 端先不走全量测试了，慢慢修就行。」

含义：**一个符号只由一个生成器拥有**，避免旧式「按函数分给两个生成器」带来的符号表分歧（全局名、未定义符号的归属、变参格式串改写等，每一个都曾导致链接错误或静默错误答案）。

```
C 源码 ──front end(预处理/解析/类型检查)──► Program
        │
        ▼  -fllvm
   genLLVMAll：用户函数 + goclib 全部函数 + 全局变量(含初值)
               统一降为单个 LLVM IR 模块
        │
        ▼  libLLVM 23.1.2（纯 Go 无 cgo 绑定，syscall.LazyProc）
   COFF 对象（与 .text/.data 同段布局，重定位走 goa 的 Fixup 模型）
        │
        ▼  goa 链接
   ├─ 入口桩（main / wWinMain / WinMain，非 C，按平台 ABI 建栈）—— 由 goa 合成
   └─ 上面的 COFF 对象拼进同一镜像
        │
        ▼
   .exe (Win) / .elf (Linux，未实现)
```

非 `-fllvm` 时：`genWith(prog, …, nil)` 走 goa 全量代码生成，行为不变。

---

## 2. 当前已实现

- **管线打通**：`-fllvm` 在 `main.go` 单字母循环**之前**识别；`genLLVMProgram → genLLVMAll → libLLVM → compileIR → goa 链接`。
- **全量 IR 生成**：`genLLVMAll` 把整个程序（用户 + goclib + 全局初值）降为单一模块；goclib 全量 IR **已能生成成功**（模块骨架正确）。
- **符号所有权闭环**：`genLLVMAll` 返回 `claimed` = 全部用户函数名 + 全部 goclib 函数名 + `G_` 前缀全局名；`main.go` 把它作为 `skipFuncs` 传给 `genWith`，goa 据此**只合成入口桩**，跳过这些符号（否则与 IR 模块双重定义）。TLS 全局仍由 goa 拥有（IR 端对 TLS 只声明 extern）。
- **COFF 链接**：`src/goa/coff.go` + `coffmerge.go` 已验证端到端（单对象 → PE → 运行，退出码正确）。段名 `/N` 长名机制、重定位 `ADDR64`/`ADDR32NB`/`wide`/`virtual` 已就位。
- **变参已支持**：`va_start`/`va_end`/`va_arg` 降到 LLVM intrinsic；`define` 签名带 `, ...)`。`llvmEligible` 不再排除变参。
- **IR 正确性要点已沉淀**（见 `MEMORY.md` 的「LLVM 后端」）：运算符名、`call` 实参带类型、比较结果 `i1`、数组参数 decay、字符串初始化拷值 vs 取地址。

---

## 3. 已知缺口（按优先级）

### P0 — 让 `-fllvm` 在 Windows 上产出可运行二进制
1. **IR 宽度不一致**：个别 goclib 函数 IR 生成报类型错误（如 `i32` 用在 `i64` 运算），集中在整型常量/窄类型提升/指针运算的 IR 发射。需逐函数修 `llvmmod.go`/`llvmstmt.go`/`llivrop.go` 的宽度推导。**这是当前阻断 e2e 的主因。**
2. **链接期符号归属**：代码已让 `genWith` 跳过 IR 拥有的非 TLS 全局与 goclib 函数；待 `-fllvm` 真能编出 IR 后，验证生成的 PE **无重复符号**、运行结果正确。TLS 全局的归属需回归（入口桩可能引用）。
3. **3 个 `-fllvm` e2e 用例失败**（`llvmbackend_test.go`）：全局变量、嵌套循环排序、三元 + 逻辑运算符。逐一定位是 IR 发射 bug 还是宽度 bug。

### P1 — 变参与运行期正确性
4. **变参运行期**：eligible 已放行，但 x86-64 寄存器保存区（va_list 布局）需真实程序验证（`printf`/`%d` 等）。补充变参运行期 e2e 用例。
5. **浮点初值 / 常量**：`_BitInt` 常量的 `constInit` 已显式放弃（走 goa），其余浮点/字符串初值需回归。

### P2 — 平台与语言覆盖
6. **Linux ELF 对象**：`compileIR` 对 `linux` 直接报错。需让 LLVM 产出 ELF 对象（`-mtriple=x86_64-unknown-linux-gnu` + ELF 格式），再由 goa 的 ELF 链接路径拼装。这是 `-fllvm` 跨平台的关键。
7. **位域（bit-field）**：`llvmEligible` 仍排除含位域访问的函数（按名过近似）。路线：在 IR 端把位域读写降为「读-改-写 + 掩码/移位」，复用 goc 已有的位域布局；或整结构体按字节数组建模。
8. **`_BitInt`**：LLVM **原生支持任意宽度整数类型**（`i<N>`），这是比 goa 自研大整数更优的落点。路线：把 `KBitInt` 映射到 LLVM 任意宽度整数，运算走 LLVM 整数指令；超大宽度（>128）再决定是否回落软件实现。
9. **内联汇编（`AsmStmt`）**：goclib 默认无内联汇编（仅 `-rtdiag` 变体有）。路线：含 asm 的函数整函数回落默认后端（已具备），或未来对简单 asm 做 IR 内联。

### P3 — 打磨
10. **优化级别映射**：`compileIR` 把 `opt` 映射到 `LLVMOptLevel`（0/1/3）。确认 `-O2`/`-O3` 真走 Aggressive，且 `-O0` 仍可用（调试）。
11. **诊断信息**：`genLLVMAll` 的错误文案已去掉「variadic」；统一其余文案，使其指向「build without -fllvm」的回退说明。
12. **体积对照**：`-fllvm` 产物体积 vs goa 默认后端（goclib 函数粒度裁剪在 LLVM 端不生效），量化 LLVM 优化的收益/代价。

---

## 4. 分阶段计划

| 阶段 | 目标 | 退出标准 |
|---|---|---|
| **M1 打通 Windows** | 修 IR 宽度 bug，让 gocregress 代表程序 `-fllvm` 编出可运行 PE | 一个中等规模 C 程序 `-fllvm` 退出码/输出与默认后端一致 |
| **M2 关闭 e2e 红灯** | 修 3 个失败用例（全局/排序/三元+逻辑） | `llvmbackend_test.go` 全绿（或明确标注为已知限制） |
| **M3 变参硬化** | 变参运行期正确 + 新增 e2e | `printf` 变参族用例通过 |
| **M4 Linux ELF** | LLVM 产出 ELF 并链接 | Linux 目标 `-fllvm` 编出可运行 `.elf` |
| **M5 位域 + _BitInt** | 两种构造降到 IR | 含位域/`_BitInt` 的程序可用 `-fllvm` 编 |
| **M6 双后端共存决策** | 基准对比，决定是否把 `-fllvm` 设为默认 | 给出「默认后端」结论与依据 |

---

## 5. 测试与护栏

- **默认后端护栏**：`gocregress pass=475` 必须保持全绿；任何 `-fllvm` 改动不得影响非 `-fllvm` 路径（`skipFuncs=nil` 时 `genWith` 行为与改前逐字节一致）。
- **LLVM e2e**：`tools/gocregress` 增加 LLVM 腿（复用 6 腿框架，触发 `goc -fllvm`）；`llvmbackend_test.go` 为单文件 e2e，缺 `libLLVM.dll` 时自跳过。
- **单元层**：`llvmir_test.go` 仍用 `genLLVM`（用户函数 → IR，全局声明 extern）做文本级断言；生产路径是 `genLLVMAll`，二者都要有测试覆盖。
- **门禁**：提交前本地复现 CI（`gofmt` 显式列 `src tools/elfcheck tools/msgboxcheck`；三模块 `go vet`；`go test`）。LLVM 端「慢慢修」，但每修一处都要补一个最小复现用例。

---

## 6. 关键文件索引

| 文件 | 职责 |
|---|---|
| `src/llvmall.go` | `genLLVMAll`：全程序 IR + 全局初值折叠 + 符号所有权 `claimed` |
| `src/llvmcompile.go` | `genLLVMProgram` / `compileIR`：驱动 LLVM、IR→COFF 对象 |
| `src/llvmmod.go` | IR 模块骨架、`llirType`、extern/intrinsic 登记 |
| `src/llvmfn.go` `src/llvmstmt.go` `src/llivrexpr.go` `src/llivrop.go` `src/llivrcall.go` | 函数/语句/表达式/运算/调用 的 IR 发射 |
| `src/llvmbackend.go` | `llvmEligible` / `usesUnsupportedLLVM`：资格判定（排除 asm/位域/_BitInt） |
| `src/llvmbuild.go` | `genLLVM`（单元测用，旧式用户函数发射器） |
| `src/main.go` | `-fllvm` 解析、`genWith(skipFuncs=claimed)` 调用 |
| `src/codegen.go` | `genWith`：入口桩 + `skipFuncs` 跳过 IR 拥有的符号 |
| `src/goa/llvm.go` | 纯 Go libLLVM 绑定 |
| `src/goa/coff.go` `coffmerge.go` | COFF 解析 + 并入 goa 镜像 |
