# goc / gocl 编译 Nim 生成的 C — 可行性 & 基准

## 结论速览

- **goc（原生 goa 后端）**：✅ 能编译并**正确运行**全部 5 个 Nim 内核，校验和与 gcc 逐位一致。
- **gocl（LLVM 后端）**：✅ 能编译并正确运行**全部 5/5** 个内核，校验和与 gcc 逐位一致（此前 fib 失败，根因是 gocl 的线程局部存储（TLS）支持缺失，已修；与递归无关）。
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
| fib     | `fib      317811`        | `fib      317811`        | `fib      317811`        | ✅ |
| intloop | `intloop  37499987500000` | `intloop  37499987500000` | `intloop  37499987500000` | ✅ |
| strbuild| `strbuild 20000`          | `strbuild 20000`          | `strbuild 20000`          | ✅ |
| float   | `float    1.10701e+06`    | `float    1.10701e+06`    | `float    1.10701e+06`    | ✅ |

## 计时（秒，10 次取最优，输出重定向到 /dev/null 以隔离计算量；输出已单独验证正确）

| 内核 | goc best / avg | gcc best / avg | gocl best / avg |
|------|----------------|----------------|-----------------|
| sieve   | 0.173 / 0.185 | 0.167 / 0.180 | 0.167 / 0.177 |
| fib     | 0.174 / 0.182 | 0.167 / 0.176 | 0.167 / 0.177 |
| intloop | 0.177 / 0.187 | 0.160 / 0.182 | 0.168 / 0.183 |
| strbuild| 0.168 / 0.178 | 0.163 / 0.176 | 0.170 / 0.175 |
| float   | 0.183 / 0.204 | 0.176 / 0.194 | 0.174 / 0.184 |

**解读**：这些内核的计算量小，Wall-clock 主要由 Nim 运行时启动 + 收尾（分配器、线程、atexit）主导，所以三家都在 0.16–0.20s 的噪声带内，差异在 ~5–10%。gcc 有 `-O2` 优化优势，goc 走的是 ~`-O0` 等价代码生成但正确；gocl 与 goc 量级相当。基准的**真正价值是正确性验证**（校验和逐位一致），而不是这几十毫秒的绝对速度。

## gocl 线程局部存储（TLS）缺陷（已修复）

最初看到「gocl 跑 Nim 的 fib 内核无输出」，表象像递归 bug，但实测定位后是 **gocl 的 `_Thread_local`/`__thread` 支持完全缺失**：

- 纯 C 递归（32/64 位 `int`、`long long`、尾递归、goto 早返）在 gocl 下**全部正确**——所以不是递归的问题。
- 复现的最小触发器是「在递归里读线程局部变量」：`_Thread_local int g_x = 5; ... return g_x;` 在 gocl 下读出来是**垃圾值**（`1073754112`），非递归读也一样。Nim 每个递归调用后都 `if (NIM_UNLIKELY(*nimErr_))` 读一个 TLS 错误标志，gocl 把这次读生成成垃圾，于是检查误判为真、提前返回，结果塌成 0。

**根因**：gocl 把 TLS 全局当普通 `extern` 全局，LLVM 生成的 `load @G_x` 直接读静态模板地址，绕过了 Windows `gs:0x58`/Linux `fs` 的 per-thread 机制；而且 `linkData` 根本没给链接器填 `Data.TLSVars`，所以 `.tls` 段、TLS 目录、`G_goc_tls_index` 都没产出。

**修复**（完全复用原生后端的 TLS 设施）：
- `src/gocl/translate.go`：新增 `ComputeTLSLayout`，按原生 `tlsPlace` 规则（8 字节对齐 + `link.TLSAlignedSize` 槽宽）给每个 TLS 全局分配 `.tls` 偏移，并填入 `irMod.tlsOffsets`；两个全局循环不再为 TLS 声明 IR 符号。
- `src/gocl/driver.go`：`linkData` 用同一 `ComputeTLSLayout` 填 `Data.TLSVars`，保证 IR 访问偏移与链接器 `.tls` 布局一致。
- `src/gocl/expression.go` / `src/gocl/call.go`：访问 TLS 全局时不再 `load @G_x`，而是经运行时 helper `__goc_tls_slot(off)` 拿到 per-thread 地址（Win 走 `gs:0x58`+index，Linux 走 `__tls_start`+off）。
- `src/common/link/emit.go`：当 `len(d.TLSVars) > 0` 时把 `__goc_tls_slot` 汇编进入口 stub；`.tls` 段与 `G_goc_tls_index`、TLS 目录由既有逻辑产出。
- 验证：`readtls`（非递归读）、`fibtls`（递归+TLS 读）、`tlstest`（多类型+写入回读）、`tlsaddr`（取地址写回）、以及原始 Nim `k_fib` 在 gocl 下**全部正确**；goc（原生）对应用例无回归。

## 本轮改动

- `src/common/link/global.go`：`foldConstInit` 新增 `CastExpr` / 三元 `CondExpr` / 一元 `!` / `_Bool`/`_BitInt` 截断的处理。**修复了 `((NU)(1) << 62)` 在静态初始化器里被折叠成 0**——这是 Nim 浮点格式化段错误的根因，goc 与 gocl 同时受益。
- `src/common/link/emit.go` + `src/gocl/*`：**新增 gocl 的线程局部存储支持**（见上），修复 Nim 生成的 C 在 gocl 下因 TLS 读取错误而崩溃/静默失败的缺陷。
- 临时调试环境变量（`GOC_DUMP_IR` / `GOC_NO_OPT`）已移除。
