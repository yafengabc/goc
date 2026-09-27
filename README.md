# c0 + a0 —— 不依赖 gcc 也不依赖 libc 的迷你 C 工具链

两个纯 Go 程序，同一份 C 源码可以出两个平台的可执行文件：

```
  foo.c ──[c0]──> foo.asm ──[a0]──> foo.exe    Windows PE32+，只导入 kernel32
              └─[c0 -target linux]─[a0 -f elf]─> foo    Linux ELF64，只用 syscall
```

```bash
bash build.sh       # 一条命令：c0 + a0 + 两个测试工具（见下面目录结构）

./c0.exe examples/hello.c                  # 编译并运行（Windows）
./c0.exe -c examples/hello.c               # 只编译
./c0.exe -S examples/hello.c               # 只输出汇编（hello.asm）
./c0.exe -c -target linux examples/hello.c # 出 Linux ELF64（无后缀）
```

Windows 产物**只导入 kernel32**（`ExitProcess` / `GetStdHandle` / `WriteFile`）；
Linux 产物是静态 ELF，一条动态链接都没有，只发 `write` / `exit` / `brk` 三个
syscall。两头都没有 msvcrt / glibc，也没有 gcc。

## 目录结构

```
.
├── lexer.go  parser.go  ast.go  codegen.go  main.go  # c0：迷你 C 编译器
├── asm/                                              # a0：汇编器（独立 go 模块）
│   ├── asm.go  pe.go  elf.go  main.go                #   Intel 语法子集 -> PE32+ / ELF64
│   ├── examples/  expected/  run_tests.sh            #   a0 的用例与 golden
│   └── tools/elfcheck  tools/msgboxcheck             #   验证工具（解释 ELF / 驱动 GUI）
├── clib/                                              # 自带的 C 库（见下「clib」一节）
│   ├── clib.asm                                       #   汇编后端：一份源，#if defined(_WIN64)/#else 双平台
│   ├── c0lib.h  c0lib.c                               #   跨平台 C 实现（c0 暂不能编译，stage5 启用）
│   └── README.md                                      #   后端切换与迁移说明
├── examples/*.c   expected/*.txt                     # c0 的用例与 golden
├── build.sh  run_tests.sh  run_tests_linux.sh          # 构建 / 测试（Win 解释 / Linux 原生）
└── .github/workflows/ci.yml                            # CI：Linux 原生端到端 + Windows 端到端
```

`asm/` 是独立的 go 模块（自己的 `go.mod`），可以单独拿出来用：给一份 `.asm`，
直接出 exe，不需要 c0。

## Linux 目标

`-target linux` 会让 c0 换一套东西：

- **调用约定**从 Win64（rcx/rdx/r8/r9 + 32 字节 shadow space）切成 SysV
  （rdi/rsi/rdx/rcx/r8/r9，无 shadow space）。
- **clib 走 `clib/clib.asm` 的 Linux 分支**（`#else` 那段）—— 同名同语义的另一套
  实现：`__clib_write` 走 `write` syscall，`malloc` 用 `brk` 做 bump 分配
  （`free` 是空操作，进程退出时一起还），`exit` 走 `exit` syscall。
- 参数上限相应从「4 个寄存器 + 栈」变成「6 个寄存器 + 栈」。

有个坑值得一提：Linux 下 a0 给每个 extern 生成的 syscall 桩**就叫 extern 的名字**，
所以 clib 里的 `exit` 函数必须改名 `__clib_exit`（否则会覆盖桩的符号并无限递归），
由 c0 在生成调用时做一次别名映射（`clibAliasLinux`）。

Windows 上没法 exec ELF，所以本机测试用 `asm/tools/elfcheck` 加载并解释执行
（校验 ELF 头/程序头，然后真的解释指令、模拟 write/exit/brk）。同一份 golden
文件：Linux 后端的输出必须和 Windows 逐字节一致。

真正的内核验证交给 CI：`.github/workflows/ci.yml` 的 Ubuntu job 会用
`run_tests_linux.sh` **直接执行**所有 Linux ELF 目标（不走解释器），真实内核 +
真实 SSE2，这才是 double 支持最硬的证明。

## clib：自带的 C 库

`printf` 不是编译器里的一段魔法字符串，而是一个真正的库。整套库写在**一份**
条件编译的汇编源 `clib/clib.asm` 里，用 `#if defined(_WIN64) / #else` 把两套实现
（Windows 走 kernel32、Linux 走 syscall）合并到同一个文件；c0 在加载时按目标平台
挑出对应分支（见 `selectPlatform`）。这跟普通编译器用 `#ifdef` 隔离平台相关汇编
是一个思路 —— 跨平台的部分只写一遍，只把 OS 相关的部分封进 `#ifdef`。

| 分支 | 提供 |
|---|---|
| `#if defined(_WIN64)` | `printf` `sprintf` `puts` `putchar` `getchar` `strlen` `strcpy` `strcmp` `strcat` `strchr` `memset` `memcpy` `memmove` `memcmp` `strncmp` `malloc` `free` `calloc` `atoi` `abs` `strtol` `rand` `srand` `exit`，以及内部 `__clib_write` `__clib_vfmt` `__clib_exit` `__clib_heap_alloc` `__clib_heap_free` `__clib_read` |
| `#else`（Linux） | 同名同语义，改为 syscall 实现 |

Windows 分支全部只建立在 kernel32 之上：`malloc`/`free` 走 `GetProcessHeap` +
`HeapAlloc`/`HeapFree`，输出走 `GetStdHandle` + `WriteFile`，所以**依赖表里依然
没有 msvcrt**。Linux 分支只依赖 syscall。

`clib/clib.asm` 在编译 c0 时用 `go:embed` 嵌进二进制。c0 只把程序**实际调用到**的
函数（及其依赖）拼进生成的汇编里，数据也一样按函数打标记 —— 只用 `putchar`
的程序不会背上 `printf` 那 512 字节的输出缓冲。库文件的结构靠注释标记：

```asm
; @func strlen          <- 函数名
section .text
strlen:
    ...
; @end

; @data putchar         <- 这段静态数据只在 putchar 被链接时带上
section .data
__clib_ch db 0
; @end
```

`; @deps __clib_write strlen` 声明依赖，`; @extern WriteFile` 声明需要的导入，
两者都会被自动展开。

**跨平台 C 版本**：`clib/c0lib.c` + `clib/c0lib.h` 是同一批函数的纯 C 实现，只写
一遍、两个平台共用。它现在是**休眠源码**——c0 的 C 子集还缺 `char`/指针/全局变量/
`for`/变参，暂时编不了；一旦 stage5 补齐这些特性，`clib/clib.asm` 就会被它取代，
汇编层只剩五个 `__clib_*` 平台原语（I/O、堆、退出、读输入）。迁移步骤见
`clib/README.md`。

已知限制：`printf` 单次调用超过 512 字节会截断；`sprintf` 跟真货一样不做边界
检查（缓冲区归调用方管）；格式化只认 `%d %s %c %x %f %%`（不支持宽度/精度；
`%f` 固定 6 位小数，相当于 C 的 `%.6f`）。

## 支持的语言子集

- 类型：`int` 与 `double`（都是 8 字节栈槽），函数参数最多 8 个（前 4 个走
  寄存器，其余压栈）
- `if` / `else` / `while` / `return`、块作用域
- 运算符：`+ - * / %`、`< > <= >= == !=`、`&& ||`、`!`、一元 `-`；操作数含
  `double` 时 `+ - * /` 与比较自动提升，结果类型随操作数
- 字符串字面量、`printf` 调用（`%d %s %c %x %f %%`）

## double 支持

`double` 走 SSE2 标量指令：参数放在 xmm0..xmm3（Windows）或 xmm0..xmm7（SysV），
算术用 `addsd` / `subsd` / `mulsd` / `divsd`，整型↔浮点转换用 `cvtsi2sd` /
`cvttsd2si`（截断），比较用 `ucomisd` 再跟无符号跳转（jb/ja/jbe/jae/je/jne）。
常量落在 `.data` 的 `dq` 里，RIP 相对寻址读取。

clib 的 `%f` 用「取整 + 小数部分循环乘 10」输出固定 6 位小数：`cvttsd2si` 截出
整数位，`cvtsi2sd` 转回去 `subsd` 减掉，剩下的小数部分循环 6 次乘 10 逐位压出；
`%f` 的变参槽位传的是 8 字节 IEEE-754 位模式（`movq rax, xmm0`）。

Windows 上没法直接执行 SSE2 验证编码，所以 elfcheck 解释器补了 F2/66 前缀解析和
这套指令的解释执行 —— Linux 目标的 fp 用例与 Windows 输出逐字节一致。CI 的
Ubuntu job 还会把 `fp` 的 Linux ELF **直接跑在真实内核上**再比一次，覆盖
解释器验证不到的地方（真实 syscall、栈对齐、16 字节 xorpd 等）。

## 调用约定

默认遵循 Windows x64 ABI：整数参数走 RCX/RDX/R8/R9，第 5 个起放 `[rsp+32]`；
调用者预留 32 字节 shadow space，每个 `call` 处 RSP 保持 16 字节对齐。压栈参数
时多申请的空间会向上取整到 16，对齐才不会被破坏。二元表达式的左操作数溢出到
rbp 相对栈槽（而不是 `push`/`pop`），也是同一个原因。

`-target linux` 时走 SysV AMD64：参数走 RDI/RSI/RDX/RCX/R8/R9，第 7 个起放
`[rsp]`（没有 shadow space）。栈帧里因此少算 32 字节。

clib 的变参函数（`printf` / `sprintf`）在入口处把寄存器里的变参和栈上的一起
收集到 `__clib_va`，然后才做第一次 `call` —— 否则寄存器里的变参会先被冲掉。
Windows 下变参从 rdx 起、栈上在 `[rbp+48]`；Linux 下从 rsi 起、栈上在 `[rbp+16]`。

## 测试

```bash
bash run_tests.sh           # c0 端到端 20/20（10 个 Windows + 10 个 Linux，后者用 elfcheck 解释）
bash run_tests_linux.sh     # 真机版：在 Linux 上直接执行 ELF（CI 的 Ubuntu job 也跑它）
cd asm && bash run_tests.sh # a0 自己的用例，14/14（11 Windows + 3 Linux）+ 1 个 GUI
```

`examples/clib.c` 把整个库跑一遍，两个平台的输出与同一份 golden 逐字节比对。

## 体积对比

| | gcc 后端 | c0 + a0 |
|---|---|---|
| `stress.c` | 72611 字节 | **4096 字节** |

差了约 17 倍 —— gcc 那个把整个 CRT 启动代码和 msvcrt 都链进去了。

## 许可

MIT，见 [LICENSE](LICENSE)。
