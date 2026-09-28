# goc + goa —— 不依赖 gcc 也不依赖 libc 的迷你 C 工具链

两个纯 Go 程序，同一份 C 源码可以出两个平台的可执行文件：

```
  foo.c ──[goc]──> foo.asm ──[goa]──> foo.exe    Windows PE32+，只导入 kernel32
              └─[goc -target linux]─[goa -f elf]─> foo    Linux ELF64，只用 syscall
```

```bash
bash build.sh       # 一条命令：goc + goa + 两个测试工具（见下面目录结构）

./bin/goc.exe src/examples/hello.c                   # 编译并运行（Windows）
./bin/goc.exe -c src/examples/hello.c                # 只编译
./bin/goc.exe -S src/examples/hello.c                # 只输出汇编（hello.asm）
./bin/goc.exe -c -o bin/goc-out src/examples/hello.c # 产物集中到 bin/goc-out/，不污染源码树
./bin/goc.exe -c -target linux src/examples/hello.c  # 出 Linux ELF64（无后缀）
```

Windows 产物只导入 **Windows 系统 DLL**（`win32.def` 列出的 kernel32/user32/gdi32  
导出，按程序实际调用取子集）；Linux 产物是静态 ELF，一条动态链接都没有，只用  
syscall（`write` / `read` / `brk` / `exit_group`）。两头都没有 msvcrt / glibc，  
也没有 gcc。

## 目录结构

```
.
├── src/                                                # 编译器源码（go 模块 goc）
│   ├── lexer.go  parser.go  ast.go  codegen.go  main.go
│   ├── goa/                                            # goa：汇编器（独立 go 模块）
│   │   ├── asm.go  pe.go  elf.go  main.go              #   Intel 语法子集 -> PE32+ / ELF64
│   │   ├── examples/  expected/  run_tests.sh          #   goa 的用例与 golden
│   │   └── README.md
│   ├── goclib/                                         # 自带的 C 库（见下「goclib」一节）
│   │   ├── goclib.c                                    #   唯一实现：纯 C，goc 启动时按目标平台自行编译
│   │   ├── goclib.h + 内置标准头 stddef/stdarg/stdio/stdlib/string.h
│   │   └── README.md                                   #   机制说明
│   └── examples/*.c  expected/*.txt                    # goc 的用例与 golden
├── tools/                                              # 验证工具（独立 go 模块）
│   ├── elfcheck  msgboxcheck                           #   解释 ELF / 驱动 GUI 断言
│   └── peun.py  ucrun.py                               #   PE / ucrt 逆向辅助脚本
├── bin/                                                # 构建产物（goc / goa / elfcheck / msgboxcheck / goc-out）
├── build.sh  run_tests.sh  run_tests_linux.sh          # 构建 / 测试（Win 解释 / Linux 原生）
└── .github/workflows/ci.yml                            # CI：Linux 原生端到端 + Windows 端到端
```

`src/goa/` 是独立的 go 模块（自己的 `go.mod`），可以单独拿出来用：给一份 `.asm`，  
直接出 exe，不需要 goc。

## Linux 目标

`-target linux` 会让 goc 换一套东西：

- **调用约定**从 Win64（rcx/rdx/r8/r9 + 32 字节 shadow space）切成 SysV  
  （rdi/rsi/rdx/rcx/r8/r9，无 shadow space）。
- **goclib 编译走 goclib.c 的 Linux 分支**（`#elif defined(__linux__)` 那段）—— 同名  
  同语义的另一套实现：`__goclib_write` 走 `write` syscall，堆分配用 `brk` 做 bump  
  allocator（`__goclib_heap_free` 是空操作，进程退出时一起还），`exit` 转调  
  `exit_group`(231)。
- 参数上限相应从「4 个寄存器 + 栈」变成「6 个寄存器 + 栈」。

有个坑值得一提：Linux 下 goa 给每个 extern 生成的 syscall 桩**就叫 extern 的名字**，  
所以库若自带 `exit`，就不能再 import 同名 extern 桩（符号冲突 + 无限递归）。现在的  
做法：入口桩的 `call exit` 通过 need 闭包拉取 C 版 `exit` 函数体，且仅当 C 库**不**  
提供 `exit` 时才注入该 extern。

Windows 上没法 exec ELF，所以本机测试用 `tools/elfcheck` 加载并解释执行  
（校验 ELF 头/程序头，然后真的解释指令、模拟 write/exit/brk）。同一份 golden  
文件：Linux 后端的输出必须和 Windows 逐字节一致。

真正的内核验证交给 CI：`.github/workflows/ci.yml` 的 Ubuntu job 会用  
`run_tests_linux.sh` **直接执行**所有 Linux ELF 目标（不走解释器），真实内核 +  
真实 SSE2，这才是 double 支持最硬的证明。

## goclib：自带的 C 库

`printf` 不是编译器里的一段魔法字符串，而是一个真正的库。整套库是**纯 C**  
（`src/goclib/goclib.c` + 伞头 `goclib.h`），平台差异封在文件顶部的  
`#if defined(_WIN32) / #elif defined(__linux__)` 里（Windows 走 kernel32、Linux 走  
syscall）——跟普通 C 库用 `#ifdef` 隔离平台相关代码是一个思路：跨平台的部分只写  
一遍，只把 OS 相关的部分封进 `#ifdef`。

goc 在启动时把 goclib 当普通 C 程序编译**两次**（每个目标一次），函数体经常规  
代码生成器按需发射：程序**实际调用到**的函数（及其传递闭包）才会进产物，只用  
`putchar` 的程序不会背上 `printf` 的 512 字节输出缓冲。库全局变量（如 `rand_state`）  
同样按引用打标后发射。

| 原语（碰 OS 的唯一层面） | Windows（kernel32 extern 直调） | Linux（goa syscall 桩） |
| --- | --- | --- |
| `__goclib_write(buf,len)` | `GetStdHandle`+`WriteFile` | `write`(fd=1) |
| `__goclib_exit(code)` | `ExitProcess` | `exit_group`(231) |
| `__goclib_heap_alloc(size)` | `GetProcessHeap`+`HeapAlloc` | `brk` bump allocator（16B 对齐） |
| `__goclib_heap_free(p)` | `HeapFree` | 空操作（进程退出一起还） |
| `__goclib_read(buf,len)` | `GetStdHandle`+`ReadFile` | `read`(fd=0) |

Windows 侧原语只建立在 kernel32 之上，所以**依赖表里依然没有 msvcrt**；Linux 侧  
只依赖 syscall。Win 侧 extern 的 DLL 归属由 `goclib/win32.def`（embed 进二进制）  
回答——改了它必须重建 goc。

已知限制：`printf` 单次调用超过 512 字节会截断；`sprintf` 跟真货一样不做边界  
检查（缓冲区归调用方管）；格式化支持 `%d %i %u %lu %llu %ld %x %s %c %f %p %%`  
与宽度/精度（`%02x`、`%.6f`、`%10.2f` 等）；`%f` 按**四舍五入到偶数**输出指定位数的  
小数；`%p` 输出 `0x` + 16 位十六进制地址。

## 支持的语言子集

- 类型：`int` 与 `double`（都是 8 字节栈槽），函数参数最多 16 个（前 4 个走  
  寄存器，其余压栈）
- `if` / `else`、`while` / `for` / `do-while`、`switch` / `case` / `default`  
  （含 fall-through 与中途的 `default`）、`break` / `continue`、`goto` 与标号、`return`、块作用域
- 运算符：`+ - * / %`、`< > <= >= == !=`、`&& ||`、`& | ^ << >>`、`!`、  
  一元 `-`、三元 `?:`，以及 `+= -= *= /= %= &= |= <<= >>=` 复合赋值；操作数含  
  `double` 时 `+ - * /` 与比较自动提升，结果类型随操作数
- 字符串字面量、`printf` 调用（`%d %s %c %x %f %%`）

## double 支持

`double` 走 SSE2 标量指令：参数放在 xmm0..xmm3（Windows）或 xmm0..xmm7（SysV），  
算术用 `addsd` / `subsd` / `mulsd` / `divsd`，整型↔浮点转换用 `cvtsi2sd` /  
`cvttsd2si`（截断），比较用 `ucomisd` 再跟无符号跳转（jb/ja/jbe/jae/je/jne）。  
常量落在 `.data` 的 `dq` 里，RIP 相对寻址读取。

goclib 的 `%f` 先截出整数位，再把小数部分乘 10 逐位压出，最后一位按**四舍五入到偶数**  
进位（必要时向整数位进位）；小数位数由精度指定（默认 6 位，即 `%.6f`，支持 `%.Nf` /  
`%.0f`，精度上限 17 位）。
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

goclib 的变参函数（`printf` / `sprintf`）在入口处把寄存器里的变参和栈上的一起  
收集到 `__goclib_va`，然后才做第一次 `call` —— 否则寄存器里的变参会先被冲掉。  
Windows 下变参从 rdx 起、栈上在 `[rbp+48]`；Linux 下从 rsi 起、栈上在 `[rbp+16]`。

## 测试

```bash
bash run_tests.sh               # goc 端到端：全部示例（Windows 原生 + Linux 用 elfcheck 解释）
bash run_tests_linux.sh         # 真机版：在 Linux 上直接执行 ELF（CI 的 Ubuntu job 也跑它）
cd src/goa && bash run_tests.sh # goa 自己的用例，14/14（11 Windows + 3 Linux）+ 1 个 GUI
```

`src/examples/goclib.c` 把整个库跑一遍，两个平台的输出与同一份 golden 逐字节比对。

## 体积对比

|            | gcc 后端   | goc + goa   |
| ---------- | -------- | ----------- |
| `stress.c` | 72611 字节 | **4096 字节** |

差了约 17 倍 —— gcc 那个把整个 CRT 启动代码和 msvcrt 都链进去了。

## 许可

MIT，见 [LICENSE](LICENSE)。
