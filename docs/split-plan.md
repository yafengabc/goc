# goc 拆分方案：goc / gocl / goclib

> 状态：**待实施**（2026-10-05 设计定稿）
> 决策依据：本文件所有数字均为源码实测（`wc -l` / 符号引用扫描），非估算
> 一句话目标：**把「goc（自研后端实验）」「gocl（LLVM 后端产品）」「goclib（共享 C 库）」
> 拆成单仓三 module，前端一份、两后端各一份、库一份。**

---

## 0. 结论先行

| 决策 | 结论 |
| --- | --- |
| 仓库形态 | **单仓三 module**，不是三个独立仓库 |
| 前端共享 | 抽出 `frontend/` package，5807 行只维护一份 |
| gocl 的 goa 后端 | **裁成窄路径**（约 8000 行变死代码后删除），不是整块删 |
| goclib | 由两个编译器共同依赖，形态待第三步定 |
| 最大工作量 | **不在后端，在把前端从 `package main` 的隐式耦合里剥出来**（第三阶段） |

---

## 1. 现状实测

### 1.1 代码分布

| 部分 | 文件 | 行数 | 归属 |
| --- | --- | --- | --- |
| **前端** | `parser.go` `check.go` `ast.go` `types.go` `lexer.go` | **5807** | 两后端 100% 共享 |
| goa 后端 | `codegen.go` | 10869 | goc 专属 |
| goa 优化管线 | `opt.go` `liveness.go` `loopana.go` | 3085 | goc 专属 |
| goa 辅助 | `print.go` `ufcs.go` `ptrcap.go` | 982 | goc 专属 |
| **LLVM 后端** | `llvm*.go`（不含测试） | **3400** | gocl 专属 |
| goa 汇编器 | `src/goa/` | 7791 | 两后端共享 |
| goclib | `src/goclib/*.c *.h` | 11254（C） | 共享库 |
| 回归工具 | `tools/` | 2044 | 共享 |
| 三个 module | `src/` `src/goa/` `tools/` | — | — |

### 1.2 关键发现：前端已经是干净的

扫描前端五个文件对后端符号的引用：

```
check.go: funcTypeOf
（其余四个文件：零引用）
```

- `dllOf` / `externLinux` / `clibCWin` / `clibCLinux` / `goclibCFS` **全部在 codegen.go**，
  前端一个都不碰。
- 前端唯一的跨界符号是 `funcTypeOf`（定义在 `check.go:1054`），本就是前端自己的东西。
- **AST 遍历工具的归属**（实测 `grep -ln "^func <name>"`）：
  `walkStmts` 在 `check.go`（前端），而 `walkExpr` / `stmtExprs` / `firstExprOf`
  三个都在 `llvmbackend.go` —— 后两者是给 LLVM 后端用的，前者 `check.go` 也在用。
  **三个都要搬进 frontend。**
- `typUnsupported` 定义在 `codegen.go`，实测**前端五个文件一个都不用它**
  （`grep -l` 结果为空），且**零处引用后端状态**。它是纯类型判定、被 LLVM 后端用了
  12 次 —— 属于**后端间的共享工具**，两个后端各持一份或放共享层，不必进 frontend。

> 这意味着**第三阶段（剥前端）的风险远低于预期**。原本担心的「前端和 codegen 共享包级变量」
> 在源码层面并不成立：真正阻碍独立的是 `package main` 这个名字和文件物理位置，不是依赖方向。

### 1.3 LLVM 后端对 codegen.go 的依赖面

`llvm*.go` 引用 codegen.go 符号的次数：

| 符号 | 次数 | 归属建议 |
| --- | --- | --- |
| `walkExpr` | 24 | frontend（在 llvmbackend.go，需搬） |
| `typUnsupported` | 12 | 共享工具（纯类型判定；前端不用，两后端共用） |
| `stmtExprs` | 6 | frontend（在 llvmbackend.go，需搬） |
| `walkStmts` | 5 | frontend（已在 check.go） |
| `funcTypeOf` | 3 | frontend（已在 check.go） |
| `clibCStore` | 2 | 共享库层（goclib 加载器） |

**结论：llvm*.go 对 codegen.go 的依赖只有 `clibCStore` 一处真依赖**，其余全部是
「定义位置不对」而非「逻辑耦合」。这是个好消息 —— LLVM 后端可以近乎整体平移。

---

## 2. 目标结构

```
goc/                              ← 仓库根（你的实验分支）
│
├── frontend/                     module: goc/frontend
│     parser.go  check.go  ast.go  types.go  lexer.go
│     walk.go（walkExpr / stmtExprs / firstExprOf，从 llvmbackend.go 搬）
│     ≈ 5900 行，两个编译器共同依赖
│
├── backendutil/                  两后端共享的纯类型工具
│     typunsupported.go（typUnsupported，从 codegen.go 搬）
│     很小；也可以先各持一份，等第三次调用出现再抽出来
│
├── goa/                          module: goc/goa          （已是独立 module）
│     汇编器 + PE/ELF 链接 + COFF 解析合并
│     7791 行，两后端共享
│
├── goclib/                       C 库（形态待定，见阶段四）
│     11254 行 C
│
├── cmd/
│   ├── goc/                      编译器 A：frontend + goa 后端
│   │     codegen.go  opt.go  liveness.go  print.go  ufcs.go
│   │     ptrcap.go  loopana.go  stub.go（从 codegen.go 拆出）
│   │     目标：自有 codegen 的实验场
│   │
│   └── gocl/                     编译器 B：frontend + LLVM 后端
│         llvm*.go（3400 行）
│         link.go（从 codegen.go 拆出，343 行 genWith 的窄版）
│         目标：LLVM 生成 ELF/COFF，goa 只做链接
│
└── tools/                        module: goc/tools        （已是独立 module）
      gocregress 等回归工具
```

### 2.1 依赖方向（单向，无环）

```
        ┌──────────┐
        │ frontend │  parser/check/ast/types/lexer
        └────┬─────┘
             │  被依赖，无反向边
     ┌───────┴────────┐
     │                │
┌────▼─────┐   ┌──────▼──────┐
│cmd/goc   │   │ cmd/gocl    │
│(goa后端) │   │(LLVM后端)   │
└────┬─────┘   └──────┬──────┘
     │                │
     └───────┬────────┘
             │
      ┌──────▼──────┐
      │    goa      │  汇编 + PE/ELF 链接
      └──────┬──────┘
             │
      ┌──────▼──────┐
      │   goclib    │  共享 C 库
      └─────────────┘
```

**唯一允许的耦合方向是向下的。** 任何后端 → 另一后端的引用都视为架构缺陷。

---

## 3. 关键事实：gocl 的 goa 后端必须「裁」而非「删」

### 3.1 为什么不能整块删

实测 `codegen.go:2829`（`genWith` 内）：

```go
for _, f := range prog.Funcs {
    if skipFuncs[f.Name] {
        continue
    }
    if err := c.genFunc(f); err != nil { return "", err }
}
```

`-fllvm` 模式下 `skipFuncs` 覆盖了**全部用户函数和 goclib 函数** ——
所有 C 代码由 LLVM 生成。但 `genWith` 本身有 **343 行**，且以下职责**在 codegen.go 里，
不在 goa 里**：

1. 符号表注册（`c.globals` / `c.globalTyp` / `c.funcs`）
2. 调用图构建（`genFunc` 前的可达性分析）
3. **TLS 全局布局**（`c.tlsPlace` / `c.tlsVars` / `c.tlsList`）
4. **入口桩合成**（main / WinMain / wWinMain，非 C，按平台 ABI 建栈）
5. **`___chkstk_ms` 注入**（`coffmerge.go:122` 的 COFF 路径，另有原生路径一份）
6. 全局布局与段分配

**这六项都是 LLVM 模式必需的。** 直接删 codegen.go 会让 gocl 无法启动。

### 3.2 裁剪范围

`genWith` 在 `-fllvm` 下**不执行一行**表达式生成。可以安全删除的部分：

| 类别 | 内容 | 行数估算 |
| --- | --- | --- |
| 表达式生成 | `genExprT1`(600) `genExprT` `genBig*`(700+) `genTruth` `genCompoundAssign` … | ~4000 |
| 优化管线 | `opt.go` 全文件（`-fllvm` 用 LLVM `PassBuilder`） | 1797 |
| 活跃性分析 | `liveness.go` 全文件 | 1042 |
| 循环分析 | `loopana.go` | 246 |
| 容量分析 | `ptrcap.go` | 432 |
| 打印特化（旧） | `print.go`（已被 `printfspec.go` 取代） | 264 |
| UFCS | `ufcs.go` | 286 |
| **保留** | 上述六项职责 + 入口桩 + TLS + chkstk | **~800** |

> **风险评估：低。** 这些代码在 `-fllvm` 下是死代码，删除不改变行为。
> 但必须**分两步**：先让 gocl 跑通（第三阶段），确认行为一致后再删（第五阶段）。
> 一步到位会失去「删错了」的反馈来源。

### 3.3 需要下沉到 goa 的部分

更彻底的做法是把入口桩合成、COFF/ELF 链接、TLS 布局、`chkstk` 注入下沉到 `goa` 包，
`link.go` 只剩编排。**本方案不选这条** —— 它要动 goa 的公共 API，收益（少 500 行）
远小于风险（goa 是两个后端共用的核心）。

---

## 4. 实施阶段

### 阶段一：脚手架（无行为变化）
1. 建 `cmd/goc/` 和 `cmd/gocl/` 目录
2. `src/*.go` 整体移入 `cmd/goc/`，`package main` → 保持 main（编译器入口）
3. 更新 `build.sh`、`run_tests.sh`、`run_tests_linux.sh`
4. 验证：两个编译器二进制行为与拆分前**逐字节一致**

**验收**：`gocregress pass=487 fail=0`，`goc -fllvm` 全部 e2e 通过。

### 阶段二：抽出 frontend
1. 建 `frontend/`，`module goc/frontend`
2. 移入 `parser.go check.go ast.go types.go lexer.go`（5807 行）
3. 从 `llvmbackend.go` 搬 `walkExpr` / `stmtExprs` / `firstExprOf` → `frontend/walk.go`
4. `typUnsupported` 放共享层（两后端共用，不进 frontend）
5. `package goc` → `package frontend`，导出面按需扩大
6. **两个编译器都改为 import `goc/frontend`**

**验收**：回归全绿；`grep` 确认 frontend 不 import 任何后端包。

### 阶段三：cmd/gocl 能跑
1. `llvm*.go` 移入 `cmd/gocl/`
2. 从 `codegen.go` 拆出 `link.go`：保留阶段 3.1 列的六项职责
3. cmd/gocl 删掉 codegen.go（暂不删 opt/liveness 等，留着）
4. `cmd/gocl` 默认走 LLVM，`-fllvm` 变成默认而非开关

**验收**：`-fllvm` 全部测试通过，gocregress 的 -fllvm 腿全绿，**输出与拆分前逐字节一致**。

### 阶段四：goclib 形态
单独决策，不阻塞前三阶段。候选：
- (a) 保持 `//go:embed`，两个编译器各自 embed 同一份源码（最简）
- (b) 独立 module `goc/goclib`，导出 embed.FS，两个编译器依赖它
- (c) 独立仓库

> 推荐 (b)：`//go:embed` 天然适合做成一个包，导出 `goclib.Source embed.FS`。
> 改动量约 20 行（`headers.go` + `buildClibC` 的路径来源），收益是 goclib 有了独立版本号
> 和独立测试。记忆里记着一条相关教训：**新增 `goclib/*.h` 后必须 `touch src/headers.go`**
> （embed 不刷新），做成 module 后这个坑要显式处理掉。

### 阶段五：删死代码
1. 删 `opt.go` `liveness.go` `loopana.go` `ptrcap.go` `print.go` `ufcs.go`（约 4000 行）
2. 裁剪 codegen.go 的表达式生成部分
3. 清理 `skipFuncs`（gocl 不再需要 —— 它就是 LLVM 模式的全集）

**验收**：goc 后端全部测试通过（这是唯一还在跑这些代码的地方）。

---

## 5. 风险与对策

| 风险 | 概率 | 影响 | 对策 |
| --- | --- | --- | --- |
| 剥前端时漏掉隐式依赖 | 中 | 编译失败，可立即发现 | 阶段二结束时 `grep` 断言 frontend 不 import 后端包 |
| 删死代码时误删仍在跑的 | 低 | goc 后端行为变化 | 阶段五晚于阶段三；gocregress 覆盖 487 用例 |
| goclib 拆出后 embed 失效 | 中 | 编译期报错 | 阶段四单独验证双编译器均能构建 |
| 两 module 循环依赖 | 低 | 无法编译 | 依赖方向单向，阶段二结束时用 `go list -deps` 验证 |
| 实验分支激进改动影响 gocl | **高** | gocl 被迫跟进 | **单仓三 module 已缓解**：gocl 只依赖 frontend，实验改动集中在 cmd/goc |

---

## 5b. 阶段二已完成（2026-10-05）

实际落地与原计划有三处偏差，都是实测逼出来的：

**1. 前端是 6357 行而非 5807。** 多出的两个文件（`print.go` 264 行、`ufcs.go` 286 行）
一开始被当成后端留在原地，搬移时才发现 `check.go` 依赖它们的三个方法
（`checker.arrayMethodHint` / `rewritePrint` / `tryUFCS`）。它们是 checker 的方法、
纯类型辅助，没有一行代码生成 —— 按**语义归属**而不是文件位置判断，才是对的分法。

**2. 后端需要 88 个前端符号，不是 68 个。** 「首字母大写」漏掉了 `const` 块里的
`Kind` 族（`KInt`/`KBool`/`KPtr`…11 个）与 `CType` 族（`TInt`/`TDouble`…），
以及 token kind（`TEOF`/`TIdent`…）。收集规则要扫 const/var 块内部。

**3. 三个符号必须排除在转换之外**：`structs` / `typedefs` / `keywords` 在后端常被
用作**结构体字段名**。加前缀会把 `structs map[string]bool` 变成
`frontend.Structs map[...]` —— 不报错，直接不解析。这类冲突靠正则无法判定，
只能靠编译反馈逐个排除。

**关于「一个目录只能有一个 main」的实测**：`goc.go` / `goa.go` / `gocl.go` 放同一目录
会得到 `main redeclared in this block`，**一个 exe 都出不来**。所以 `gocl` 必须是
独立目录（与 `src/goc` 同级），这是语言规则不是设计取舍。

### 已达成的结构

```
src/frontend/     module goc/frontend      6357 行，**零依赖**（连 goa 都不 import）
src/goc/          module goc                package compiler（可 import）
  cmd/goc/main.go                           薄入口，os.Exit(compiler.Main(...))
src/              module goc/selfcontained   main.go embed goclib/ → 6.4M 单文件
src/goa/          module goa                package goa（可 import）+ cmd/goa
```

`src/main.go` 这个自包含入口是**阶段三 gocl 的工作模板**：它证明了
「package compiler + SetLibrary(embed) + 薄 main」这条路径可行，
gocl 要做的只是把代码生成换成 LLVM、去掉自研 codegen。

### 库加载必须惰性（真 bug，值得单列）

`SetLibrary` 最初写成「换源 + 把 `libLoad` 置空」，结果**库永远不编译**，
症状是 `codegen error: unknown function "ExitProcess"`。

根因是 Go 的初始化顺序：**被导入包的 `init()` 先于导入者执行**。若库在
`package compiler` 的 `init()` 里编译，入口包的注入一定来得太晚。
正确做法是 `sync.Once` 惰性加载 + `Main()` 开头 `ensureLib()` ——
`ensureLib` 还必须放在 `Main` 最前面，因为预处理器解析 `#include <stdio.h>`
时就要读库，晚了就是 cpp.go 里的空指针而不是一条诊断。

## 6. 建议的第一步

**做阶段一（脚手架）+ 阶段二（抽 frontend），跑通后再谈 gocl。**

理由：阶段一二是纯搬移，风险低、收益确定（前端单点维护），且完成后
「gocl 删 codegen」才有一个能立刻验证的目标。而阶段三之后每一步都涉及行为验证，
需要更充分的回归基线。

---

## 附：本文档的数据来源

```sh
# 代码分布
ls -1 src/*.go | grep -v _test | while read f; do wc -l < "$f"; done
for d in src src/goa tools; do find $d -name "*.go" -not -name "*_test.go" | xargs wc -l; done
wc -l src/goclib/*.c src/goclib/*.h

# 前端对后端的依赖（结果：仅 check.go: funcTypeOf）
for f in parser check ast types lexer; do
  grep -oE "\b(buildClibC|clibCWin|clibCLinux|newCGFor|dllOf|funcTypeOf|Gen)\b" $f
done

# LLVM 后端对 codegen 的依赖
grep -ohE "\b(walkExpr|typUnsupported|stmtExprs|walkStmts|funcTypeOf|clibCStore)\b" src/llvm*.go | sort | uniq -c
```
