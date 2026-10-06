# goc / gocl 编译 Nim 生成的 C — 可行性 & 基准

## 结论速览

- **goc（原生 goa 后端）**：✅ 能编译并**正确运行**全部 5 个 Nim 内核，校验和与 gcc 逐位一致。
- **gocl（LLVM 后端）**：✅ 能编译并正确运行 **4/5** 个内核；**递归过程**（fib）编译通过但运行错误——已知的 gocl 后端缺陷（见下）。
- 本轮修复的关键阻塞：静态初始化器里的强制类型转换常量（`((NU)(1) << 62)`，即 Nim 的 `NIM_STRLIT_FLAG`）被算成 0，导致**任何 Nim 浮点→字符串转换在两个后端都段错误**。修掉后浮点路径通了。

## 怎么生成的

```bash
# Nim 每个 .nim → 多个 .c（Nim C 后端）
nim c --compileOnly --nimcache:.nc_sieve -d:danger k_sieve.nim
# 用各编译器链接（需要 Nim 标准库头）
goc  -I "D:\Program Files\nim-2.2.12\lib" .nc_sieve/*.c -o k_sieve_goc.exe
gocl -I "D:\Program Files\nim-2.2.12\lib" .nc_sieve/*.c -o k_sieve_gocl.exe
gcc  -O2 -I "D:\Program Files\nim-2.2.12\lib" .nc_sieve/*.c -o k_sieve_gcc.exe
```

5 个内核覆盖编译器必须正确的东西：整数循环、递归、堆分配、Seq/字符串增长（分配器 + memcpy）、浮点。
每个内核打印一个确定性校验和——编译器的错误编译会表现为**不同的数字**，而不是单纯变慢。

> 完整 `bench.nim`（含 `std/strutils`）目前**不能**直接编：它拉进 Nim 运行时的 `resize`/`eqdestroy` 等函数，goclib 尚未实现（与编译器后端无关，是运行时覆盖面的事）。所以基准用 5 个自包含内核。

## 正确性（与 gcc 对照）

| 内核 | goc 输出 | gcc 输出 | gocl 输出 | 一致 |
|------|----------|----------|-----------|------|
| sieve   | `sieve    25997`        | `sieve    25997`        | `sieve    25997`        | ✅ |
| fib     | `fib      317811`        | `fib      317811`        | （空 — 递归 bug，见下）  | — |
| intloop | `intloop  37499987500000` | `intloop  37499987500000` | `intloop  37499987500000` | ✅ |
| strbuild| `strbuild 20000`          | `strbuild 20000`          | `strbuild 20000`          | ✅ |
| float   | `float    1.10701e+06`    | `float    1.10701e+06`    | `float    1.10701e+06`    | ✅ |

## 计时（秒，10 次取最优，输出重定向到 /dev/null 以隔离计算量；输出已单独验证正确）

| 内核 | goc best / avg | gcc best / avg | gocl best / avg |
|------|----------------|----------------|-----------------|
| sieve   | 0.173 / 0.185 | 0.167 / 0.180 | 0.167 / 0.177 |
| fib     | 0.174 / 0.182 | 0.167 / 0.176 | —（递归 bug）   |
| intloop | 0.177 / 0.187 | 0.160 / 0.182 | 0.168 / 0.183 |
| strbuild| 0.168 / 0.178 | 0.163 / 0.176 | 0.170 / 0.175 |
| float   | 0.183 / 0.204 | 0.176 / 0.194 | 0.174 / 0.184 |

**解读**：这些内核的计算量小，Wall-clock 主要由 Nim 运行时启动 + 收尾（分配器、线程、atexit）主导，所以三家都在 0.16–0.20s 的噪声带内，差异在 ~5–10%。gcc 有 `-O2` 优化优势，goc 走的是 ~`-O0` 等价代码生成但正确；gocl 与 goc 量级相当。基准的**真正价值是正确性验证**（校验和逐位一致），而不是这几十毫秒的绝对速度。

## gocl 递归缺陷（已知，待修）

`k_fib.nim`（5 行递归 `fib`）在 gocl 下：
- **无优化管线**：运行时**无限递归到 n=-1 后栈溢出段错误**（gdb 确认）。
- **-O1（默认）**：LLVM 把本应两次顺序递归调用的线性结构**误优化成一个单调用循环**，程序静默返回错误/无输出。

生成的 IR 本身是正确的线性递归（无回边），所以这是 gocl→LLVM 代码生成/ABI 层面的问题，不是上游 LLVM 的通用 bug。复现最小用例：`bench/nim/k_fib.nim`。

## 本轮改动

- `src/common/link/global.go`：`foldConstInit` 新增 `CastExpr` / 三元 `CondExpr` / 一元 `!` / `_Bool`/`_BitInt` 截断的处理。**修复了 `((NU)(1) << 62)` 在静态初始化器里被折叠成 0**——这是 Nim 浮点格式化段错误的根因，goc 与 gocl 同时受益。
- 临时调试环境变量（`GOC_DUMP_IR` / `GOC_NO_OPT`）已移除。
