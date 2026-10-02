# Group A2 — 存储类 / 聚合 / _BitInt / 探针

> 对拍引擎：`tests\c23\_tools\check_case.ps1`，gcc = `D:\msys\ucrt64\bin\gcc.exe -std=c2x -Wall -Wextra` (16.2.0 MSYS2 UCRT64)。
> goc = `D:\projects\goc\bin\goc.exe`（2026-10-02 版本）。语义判定由本负责人定稿。

| 文件 | 特性 | 子用例数 | 判定 | goc 证据（报错原文/关键输出） | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|---|
| c23_thread_local.c | thread_local/_Thread_local | 7 | PASS | 无 stderr；SUMMARY 7/7 exit 0，逐行与 gcc 一致 | gcc 7/7，输出逐行一致 | **能放心用**（文件作用域 + 函数内 static thread_local）；见摘要局限 |
| c23_bitint.c | _BitInt(N) 大整数 | 15 | PARTIAL | 无 stderr（能跑但值错）：case2 `s8=200`（应 -56）；case3 `sum=300 prod=400`（应 44,144）；case5 `underflow=18446744073709551615`（应 4294967295）。SUMMARY goc=12/15 vs gcc=15/15 | gcc 15/15；>64 位全部逐位一致（含 2^128-1、跨宽度符号扩展、256 位平方） | **>64 位能放心用，≤64 位要避开**；另须避开"与字面量比较" |
| c23_noarg_func.c | 无参 f()==f(void) | 5 | PASS | 无 stderr；SUMMARY 5/5 exit 0，逐行一致 | gcc 5/5，输出逐行一致 | **能放心用** |
| c23_anon_struct.c | 匿名 struct/union 成员 | 7 | PARTIAL | 无 stderr（能跑但值错）：case3 双层嵌套匿名 `a=11`（gcc `a=10`）。SUMMARY goc=6/7 vs gcc=7/7。首版另报：`cannot brace-initialise bit-field member "lo"` | gcc 7/7；单层匿名全部一致 | **单层匿名能放心用，双层嵌套匿名要避开**；位域勿花括号初始化 |
| c23_atomic_probe.c | _Atomic / <stdatomic.h> | 1 | UNSUPPORTED | stderr 原文：`<...>: note: skipping unavailable system header <stdatomic.h>`；`parse error: line 15: expected ";", got "int"`（line 15 = `_Atomic int ax = 0;`）。exit 1 | gcc 干净编译运行 `ax=1 a=6` | **要避开**：原子类型与 stdatomic.h 均不可用 |

## 组内发现摘要

### 1. _BitInt 打印方案（重要，供其他组复用）
- **gcc 16.2.0 的 printf 不支持 `%wN` 宽度修饰符**：实测 `%w8d` 被当字面量打印成 `8d`、`%wu16u` 直接破坏格式串。故本套测试不用 `%wN`。
- 本套采用：**≤64 位 _BitInt 先 `(long long)`/`(unsigned long long)` 强转，用 `%lld`/`%llu` 打印；>64 位用自实现 `/10` 十进制打印机**（`while (v > (T)0) { buf[i++] = '0'+(int)(v%ten); v=v/ten; }`）。该打印机在两侧除法一致，输出逐位对拍通过。

### 2. goc _BitInt 的两个真实坑（写标准库务必避开）
- **坑 A — 与无类型整数字面量比较会让编译器 panic（整个文件编译失败）**：`while (v > 0)`、`if (x == 0)`、`x >= 10` 这类"_BitInt 与裸字面量比较"在 goc 触发 Go 运行时 panic（`main.bigWordsOf ... codegen.go:1043`，nil 解引用，exit 2）。gcc 完全正常。**缓解写法：全部显式 `(T)0`/`(T)10`，或与另一个同宽 _BitInt 变量比较**（如 `v > (U128)0`、`x == expected_var`）。算术 `v + 1`、`v / 10`、`v % 10` 与字面量混用**不**触发 panic，正常。
- **坑 B — ≤64 位宽度不做掩码（核心缺口）**：goc 对 `_BitInt(N)`（N≤64，单机器字）在赋值/强转/运算后**不按 N 位回绕**，直接保留 64 位全精度；>64 位（走 goclib 大整数运行时）回绕正确。实测：`(signed _BitInt(8))200` 给 200（应 -56）、`(unsigned _BitInt(8))200+100` 给 300（应 44）、`(unsigned _BitInt(32))0 - 1` 给 2^64-1（应 2^32-1）。**结论：_BitInt 仅适合做 >64 位大整数（128/256/1024）；不要把它当"恰好 N 位"的窄整型用——那等价于普通 int/uint64 行为。**
- **sizeof 两侧不一致（仅小宽度）**：goc 恒按 64 位字对齐（N≤64 一律 sizeof=8），gcc 按 1/2/4/8/16 字节打包。N≥33 起两侧一致（64→8、65→16、128→16、256→32、1024→128）。测试文件 sizeof 只打 N≥33 的宽度。

### 3. thread_local 的实测补充
- 文件作用域 `thread_local`/`_Thread_local` 与函数内 `static thread_local` 全部与 gcc 一致，确证 roadmap #118-#123 的 goa 原生 TLS。
- **单线程局限（本组明确标注）**：全部测试均在单进程单线程内运行，**无法验证"每线程独立存储"**——这是线程独立性的根本局限，需待 #130 `<threads.h>` 线程创建落地后另组验证。
- 顺带发现两个 goc 健壮性 bug（未纳入主测试文件，因 gcc 拒绝该构造 / 会让 goc 崩溃）：
  - **块作用域非 static 的 `thread_local int x;` 会让 goc panic**（`main.(*Type).IsArray ... codegen.go:4724`，nil 解引用）；gcc 正确报 `error: function-scope 'x' implicitly auto and declared '_Thread_local'`。合法用法只需记住"块内必须 `static thread_local`"。
  - goc 容忍**非常量 TLS 初始化器**（`static thread_local int n = runtime_fn();`）但**静默置零**（gcc 报 `initializer element is not constant`）；合法 C 下 TLS 初始化必须是常量表达式。

### 4. 匿名成员的实测补充
- 单层匿名 struct/union（扁平访问、union 重叠、箭头、sizeof、单层 designated/位置初始化）全部与 gcc 一致。
- **双层嵌套匿名（匿名内再匿名、且中间夹一个具名字段）字段解析错位**：`struct { int outer; struct { int a; struct { int b,c; }; }; }` 用 `{9,10,11,12}` 或 designated 初始化，goc 读 `a` 恒为 11（gcc 为 10）——疑似 goc 对夹着具名字段的两级匿名做字段/偏移消解时把 `a` 与内层 `b` 重叠。**建议标准库只用一层匿名嵌套。**
- **goc 不支持位域成员的花括号初始化**：`struct { unsigned lo:4; ... } t = {0x5, ...}` 报 `cannot brace-initialise bit-field member "lo"`；gcc 接受（仅 -Wmissing-braces 警告）。测试中改为逐字段赋值。位域+_BitInt 交互归 D2 组，本组未涉及。

### 5. _Atomic 探针结论
- roadmap #129 声称"语法接受 + lock 前缀"，**实测并不成立**：goc 连 `_Atomic int x;` 关键字都**不解析**（`parse error: line 15: expected ";", got "int"`），并非"接受后发 lock 前缀"。
- `<stdatomic.h>` 按设计缺失：goc 打 `note: skipping unavailable system header <stdatomic.h>`，随后 `atomic_int` 未定义报错。
- **整体记为 UNSUPPORTED**：写标准库时原子量/原子操作完全不可用，需自行绕开。

### 交叉点
- _BitInt 与位域的交互归 D2 组 `c23_bitfield.c`，本组未覆盖。
- 无参函数的"带参调用歧义"归 E 组 `c23_noarg_args.c`，本组严格不带参调用。
- 组内多个 goc panic（块作用域 TLS、_BitInt 字面量比较）均为 codegen 期 nil 解引用，属编译器健壮性缺陷而非运行期问题；如后续修复，对应子用例应可转 PASS。
