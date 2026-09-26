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
├── clib_win/                                         # C 库，Windows 版（只靠 kernel32）
├── clib_linux/                                       # C 库，Linux 版（只靠 syscall）
├── examples/*.c   expected/*.txt                     # c0 的用例与 golden
├── build.sh  run_tests.sh                            # 构建 / 测试
└── .github/workflows/ci.yml                          # CI：Linux 编译检查 + Windows 端到端
```

`asm/` 是独立的 go 模块（自己的 `go.mod`），可以单独拿出来用：给一份 `.asm`，
直接出 exe，不需要 c0。

## Linux 目标

`-target linux` 会让 c0 换一套东西：

- **调用约定**从 Win64（rcx/rdx/r8/r9 + 32 字节 shadow space）切成 SysV
  （rdi/rsi/rdx/rcx/r8/r9，无 shadow space）。
- **clib 从 `clib_win/` 换成 `clib_linux/`** —— 同名同语义的另一套实现：`__clib_write` 走
  `write` syscall，`malloc` 用 `brk` 做 bump 分配（`free` 是空操作，进程退出时
  一起还），`exit` 走 `exit` syscall。
- 参数上限相应从「4 个寄存器 + 栈」变成「6 个寄存器 + 栈」。

有个坑值得一提：Linux 下 a0 给每个 extern 生成的 syscall 桩**就叫 extern 的名字**，
所以 clib 里的 `exit` 函数必须改名 `__clib_exit`（否则会覆盖桩的符号并无限递归），
由 c0 在生成调用时做一次别名映射（`clibAliasLinux`）。

Windows 上没法 exec ELF，所以测试用 `asm/tools/elfcheck` 加载并解释执行
（校验 ELF 头/程序头，然后真的解释指令、模拟 write/exit/brk）。同一份 golden
文件：Linux 后端的输出必须和 Windows 逐字节一致。

**仍建议在有 Linux 的机器上真跑一次** —— 解释器能验证指令语义，替代不了真实内核。

## clib：自带的 C 库

`printf` 不是编译器里的一段魔法字符串，而是一个真正的库 —— 两套平行的汇编
模块，同名同语义、各写一份：

| 文件 | 提供 |
|---|---|
| `clib_win/stdio.asm` | `printf` `sprintf` `puts` `putchar`（内部 `__clib_vfmt` `__clib_write`） |
| `clib_win/string.asm` | `strlen` `strcpy` `strcmp` `strcat` `memset` `memcpy` |
| `clib_win/stdlib.asm` | `malloc` `free` `atoi` `abs` `exit` |
| `clib_linux/*.asm` | 同样三个文件、同样这批函数名，只是换成 syscall 实现 |

Windows 版全部只建立在 kernel32 之上：`malloc`/`free` 走 `GetProcessHeap` +
`HeapAlloc`/`HeapFree`，输出走 `GetStdHandle` + `WriteFile`，所以**依赖表里依然
没有 msvcrt**。`clib_linux/` 是同名同语义的另一套，只依赖 syscall。

`clib_win/` 和 `clib_linux/` 下的 `.asm` 在编译 c0 时用 `go:embed` 嵌进二进制。
c0 只把程序**实际调用到**的
函数（及其依赖）拼进生成的汇编里，数据也一样按函数打标记 —— 只用 `putchar`
的程序不会背上 `printf` 那 512 字节的输出缓冲：

```bash
$ printf 'int main() { putchar(65); putchar(10); return 0; }\n' > min.c
$ ./c0.exe min.c
compiled min.exe (1536 bytes)     # printf/malloc/strlen 一个都没进
```

库文件的结构靠注释标记，加函数就是往 `clib_win/`（必要时同时 `clib_linux/`）
里丢一个块：

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

已知限制：`printf` 单次调用超过 512 字节会截断；`sprintf` 跟真货一样不做边界
检查（缓冲区归调用方管）；格式化只认 `%d %s %c %x %%`（不支持宽度/精度/`%f`）。

## 支持的语言子集

- 类型只有 `int`（8 字节栈槽），函数参数最多 8 个（前 4 个走寄存器，其余压栈）
- `if` / `else` / `while` / `return`、块作用域
- 运算符：`+ - * / %`、`< > <= >= == !=`、`&& ||`、`!`、一元 `-`
- 字符串字面量、`printf` 调用

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
bash run_tests.sh           # c0 端到端 16/16（8 个 Windows + 8 个 Linux）
cd asm && bash run_tests.sh # a0 自己的用例，13/13（10 Windows + 3 Linux）+ 1 个 GUI
```

`examples/clib.c` 把整个库跑一遍，两个平台的输出与同一份 golden 逐字节比对。

## 体积对比

| | gcc 后端 | c0 + a0 |
|---|---|---|
| `stress.c` | 72611 字节 | **4096 字节** |

差了约 17 倍 —— gcc 那个把整个 CRT 启动代码和 msvcrt 都链进去了。

## 许可

MIT，见 [LICENSE](LICENSE)。
