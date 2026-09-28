# goa — 纯 Go 的 x86-64 汇编器 + PE / ELF 生成器

不依赖 gcc / nasm / 任何外部工具链：Go 标准库解析 asm，直接输出可执行文件。

- 默认 **Windows PE32+**（导入表 + IAT）
- `-f elf` 输出 **Linux ELF64**（静态、无 libc、无动态链接器）

```bash
go build -trimpath -ldflags="-s -w" -o goa.exe .
./goa.exe examples/hello.asm              # -> examples/hello.exe
./goa.exe examples/hello.asm out.exe      # 指定输出名
./goa.exe -f elf examples/linux/hello.asm # -> examples/linux/hello（Linux 惯例无后缀）
```

它是上层 C 编译器 goc 的后端（`../README.md`），但也能单独用：给一份 `.asm`，直接出
可执行文件。

## Windows 目标

```asm
; hello.asm -- 直接用 kernel32 打印，不经 libc
section .data
msg          db "Hello, world!", 10, 0
bytesWritten dq 0
hStdout      dq 0

section .text
global _start

extern GetStdHandle, kernel32
extern WriteFile,    kernel32
extern ExitProcess,  kernel32

_start:
    and rsp, -16                  ; Win64：入口处手动对齐栈
    sub rsp, 48                   ; 32 字节 shadow space + 余量
    mov rcx, -11                  ; STD_OUTPUT_HANDLE
    call GetStdHandle
    mov [rip+hStdout], rax
    mov rcx, [rip+hStdout]
    lea rdx, [rip+msg]
    mov r8, 14
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0               ; 第 5 个参数（栈）
    call WriteFile
    mov rcx, 0
    call ExitProcess
```

要点：

- 入口符号固定为 `_start`（`global _start`）。
- `extern <func>, <dll>` 声明导入；`call <func>` 会自动走 IAT。
- `subsystem windows` 生成 GUI 程序（不分配控制台），默认是 `subsystem console`。
  `du "文本"` 定义 UTF-16LE 字符串并自动 NUL 结尾，可直接交给 `MessageBoxW` 这类
  Wide-char API —— 用 `MessageBoxA` + UTF-8 字节在中文 Windows（GBK）下会乱码。
- 标签以 `.` 开头（`.loop:` / `.Lrec:`）是**局部标签**，作用域限定在最近一个全局
  标签之内 —— 两个函数可以各有一个 `.Lrec` 而不会互相串（早期版本所有标签都是全局的，
  `fact` 的 `jg .Lrec` 会跳进 `fib` 的 `.Lrec`，这个坑终于填上了）。
- 注释用 `;`。`//` 在 goc 生成的 `__asm {}` 块里也认。

## Linux ELF 目标

```asm
; Linux ELF64 hello world —— 静态、无 libc、无动态链接器
extern write
extern exit

section .data
msg db "Hello, Linux!", 10

section .text
global _start
_start:
    mov rdi, 1              ; fd = stdout（SysV：参数走 rdi/rsi/rdx/rcx/r8/r9）
    lea rsi, [rip+msg]
    mov rdx, 14
    call write

    mov rdi, 0
    call exit
```

要点：

- **没有 DLL 可导入**。`extern <name>` 里的名字必须是 goa 认识的 Linux 系统调用名，
  否则直接报错。goa 为每个 extern 生成一个桩 `mov rax, <号>; syscall; ret`，所以
  `call write` 就是一次 syscall。当前认识这些：
  `read` `write` `open` `close` `lseek` `mmap` `mprotect` `munmap` `brk` `ioctl`
  `writev` `nanosleep` `getpid` `exit` `kill` `exit_group` `gettimeofday`
  `clock_gettime`（表在 `asm.go` 的 `linuxSyscalls`）。
- 也可以直接写 `syscall` 指令（见 `examples/linux/raw.asm`）。
- 参数是 **SysV AMD64** 顺序：`rdi, rsi, rdx, rcx, r8, r9`，没有 shadow space。
- `_start` 不能 `ret` 回来，必须自己 `exit`。
- 镜像布局：单个 `PT_LOAD` 覆盖整个文件，`0x400000` 起 RWX，
  `0x80` 开始是 `.text`（前面是 ELF 头和一个程序头）。

## 支持的指令

整数 / 通用：

| 类别 | 指令 |
| --- | --- |
| 数据定义 | `db` `dq`（支持 `N dup(v)`）、`du`（UTF-16LE 字符串，自动补 NUL） |
| 传送 | `mov`、`movzx` `movsx` `movsxd`（含 `movslq` / `movzbl` 等别名）、`lea`、`push`、`pop`、`xchg` |
| 算术 | `add` `sub` `adc` `sbb` `and` `or` `xor` `cmp` `test` `imul` `mul` `idiv` `div` `inc` `dec` `neg` `not` |
| 移位 / 旋转 | `shl` `sal` `shr` `sar` `rol` `ror` `rcl` `rcr` |
| 位操作 | `bt` `bts` `btr` `btc`、`bswap` |
| 符号扩展 | `cqo`（`cqto`）、`cdq`（`cltd`）、`cdqe`（`cltq`）、`cwde`（`cwtl`） |
| 控制转移 | `call` `ret` / `ret N`（`retn` `retq`）、`jmp`、`jmp short`、**全部 16 个条件跳转**（见下） |
| 条件传送 / 置位 | **全部** `cmov<cc>`、`set<cc>` |
| 循环 | `jrcxz` `jecxz` |
| 杂项 | `nop` `pause` `hlt` `ud2` `int3` `cpuid` `rdtsc` `leave`、`mfence` `lfence` `sfence`、`syscall`（`0F 05`） |

SSE2 标量浮点（`%.1f` 那一套够用了）：

```
movsd movss addsd subsd mulsd divsd sqrtsd xorpd ucomisd
cvtsi2sd cvttsd2si cvtss2sd cvtsd2ss movq
```

条件码不逐个拼写，而是从一张 16 项的表派生出来的：`j<cc>` = `0F 80+cc`、
`set<cc>` = `0F 90+cc /0`、`cmov<cc>` = `0F 40+cc /r`。表名收录了经典同义写法，
所以 `jz`≡`je`、`jc`≡`jb`、`jnae`≡`jb`、`jnle`≡`jg`、`sete`≡`setz`、
`cmovng`≡`cmovle` 等等都可以（`asm.go` 里的 `ccTable` / `ccAlias`）。

## 支持的操作数形式

```
mov rax, 123              ; reg, imm64
mov rax, rbx              ; reg, reg
mov al, bl                ; 8 位寄存器
mov rax, [rip+sym]        ; RIP 相对载入
mov [rip+sym], rax        ; RIP 相对存储
mov [rip+buf], al         ; 8 位 RIP 相对存储
mov byte [rip+buf], 0x30  ; RIP 相对立即数存储（imm8）
mov [rbp-8], rax          ; [base±disp]
mov [rbx+rcx*8], rax      ; [base + index*scale]
mov [rbx+rcx*8+16], rax   ; [base + index*scale + disp]
mov rax, [rbx+rcx*8]      ; 同上，载入
mov qword [rbp-8], 42     ; [base+disp] 立即数存储（imm32 / REX.W 下符号扩展）
mov word [rbp-8], 42      ; 0x66 + imm16
lea r13, [rip+digits]     ; RIP 相对取址
lea rax, [rbx+rcx*8+16]   ; 寄存器取址
```

支持 `byte` / `word` / `dword` / `qword` / `ptr` 大小前缀（会被剥掉，实际宽度由
寄存器或 `db`/`dq` 决定 —— 除了上面那种「内存 + 立即数」，那里必须靠前缀定宽度）。

## 已知限制

- 只生成 PE32+（x86-64 Windows）和 ELF64（x86-64 Linux），不产出 Mach-O / 其他架构。
- SSE 只覆盖部分标量 double 指令：**没有** packed / SIMD 整数（没有 `paddd`、`pmuludq`
  之类），没有 AVX / VEX 编码。
- `mov [mem], imm` 只支持 `[base+disp]` 形式（可以带前缀定宽度）；带 index 寄存器、`db`
  之外的 RIP 相对形式只有 imm8。
- 无宏、无 `%include`、无 `resb` 等 NASM 伪指令。常量用 `name = 123` 定义，不是 `equ`。
- 段只有 `.text` / `.rdata` / `.data`，输出时 `.rdata`、`.data` 和导入表会被
  合并进同一个 `.data`（见下节）。
- PE 一侧没有资源（`.rsrc`）、没有重定位表、没有调试信息。

## 输出体积

`examples/hello.exe` = **1536 字节**。构成：

```
0x000-0x200   PE 头 + 节表（512，FileAlignment 下限）
0x200-0x400   .text（实际 93 字节，补齐到 512）
0x400-0x600   .data（实际 191 字节，含导入表，补齐到 512）
```

两个做法换来的：

1. **`FileAlignment = 0x200` 而不是 `0x1000`**（`SectionAlignment` 仍必须是 `0x1000`，
   那是页大小，改不了）。之前节在磁盘上按 4KB 补齐，一个 93 字节的 `.text` 要占 4096 字节。
2. **`.rdata` / `.data` / 导入表合并成一个 `.data` 节**。节数从 3~4 降到 2，
   头能塞进一个 512 字节块，也少付两次对齐填充。

1536 是「不作弊」的下限了：再往下只能把 `.text` 和 `.data` 合成一个 RWX 节
（会被 DEP 拦、被杀软盯上），或者用 `FileAlignment < 512` 的畸形头（PE 规范不允许，
部分 Windows 版本直接拒绝加载）。做这两个之前是 **16384 字节**。

Linux 侧没有这个问题：`examples/linux/hello` 只有 503 字节。

## 测试

```bash
bash run_tests.sh
```

一次做三件事：

1. 把 `examples/*.asm` 汇编、**运行**，与 `expected/<name>.txt` 逐字节比对。
   两处细节值得学：**退出码单独取**（`$?`），因为只看 stdout 会漏掉「打印了正确前缀
   然后崩」这种失败（`fmath` 在 CI 上就出过：吐出 `4` 然后挂掉）；diff **无条件先跑
   一遍**，这样即使崩了诊断文件也在，报错时有东西可读。崩溃时会 `od -c` 打出原始字节。
2. 把 `examples/linux/*.asm` 汇编成 ELF 并在 QEMU 的 CPU 核心上执行（见下节）。
3. `examples/msgbox.asm` 这个 GUI 程序没有 stdout 可比，走 `../../tools/msgboxcheck`。

当前 **14 项 vs golden（11 个 Windows + 3 个 Linux）+ 1 个 GUI**，全通过。

仓库根的 `run_tests.sh` 会**委托调用本脚本**（而不是自己抄一份例子清单），所以这一套
用例的定义只有一处。两个环境变量：

- `GOC_PYTHON=/path/to/python` —— 指定带 unicorn 绑定的解释器，跑 Linux 腿用
- `GOC_SKIP_MSGBOX=1` —— 跳过 msgbox 用例；CI runner 没有交互式桌面，设了它

另外 `../../tools/elfcheck -structure-only` 会对每个 ELF 做结构断言（headers /
segments / entry / 节表自洽）—— 与指令语义正交的那部分仍然是我们自己把关。

### 本机跑不了 ELF，怎么验证？

Windows 上没法 exec 一个 ELF，所以 Linux 腿交给 **QEMU 的 CPU 核心**：
`../../tools/ucrun.py` 用 Unicorn（把 QEMU 的 TCG 翻译核心做成库）按 `PT_LOAD` 把镜像映射进去（注意 vaddr 要 **向下** 对齐到页），从 entry 真跑，
遇到 `syscall` 时实现 `write`（真的写到 stdout）、`exit_group`、`brk`。

为什么不用手写解释器：解释器最多证明「和我们对 ISA 的理解一致」。真机这一段是有
代价换来的 —— 换成 QEMU 跑的当天就抓到一个手写解释器（对未映射地址一律返回 0）
掩盖的真段错误。现在 `examples/linux/num.asm` 那种输出 `fact(5) = 120` /
`fib(10) = 55` 的用例是有意义的验证：数值算错、递归栈错、syscall 参数错、地址越界
都会立刻暴露。

（仍建议在有 Linux 的机器上真跑一次：CI 的 Ubuntu job 就是这么干的，它覆盖模拟器
之外还有真实内核语义。）

`msgbox.asm` 走 `../../tools/msgboxcheck`：启动 exe、按标题找到对话框（纯 Go syscall
调 `FindWindowW`）、读出实际显示的正文、用 `GetDlgItem(IDYES)` + `BM_CLICK` 点「是」、
确认程序走了 IDYES 分支、再关窗检查退出码。**需要交互式桌面**，锁屏时会失败。

## 写 Windows x64 汇编时必须注意的两件事

这两个坑在调试过程中各花掉一轮：

1. **栈必须 16 字节对齐**，且调用者要为被调用者预留 32 字节 shadow space。
   `call` 指令执行那一刻 `rsp` 必须是 16 的倍数，否则 `WriteFile` 一类
   用了 SSE 指令的 API 会直接崩。
2. **不要用易失寄存器保存跨调用的值**。Win64 下 `rax/rcx/rdx/r8/r9/r10/r11`
   被调用方随意践踏，循环变量要用 `rbx/rbp/r12-r15` 这类非易失寄存器。
   另外 shadow space 那 32 字节属于被调用方，`push` 存的数据如果落在这个
   范围内会被覆盖（见 `examples/loop.asm` 里的注释）。

## Linux 目标下有什么不同

| | Windows x64 | Linux (SysV AMD64) |
| --- | --- | --- |
| 参数寄存器 | `rcx rdx r8 r9` | `rdi rsi rdx rcx r8 r9` |
| shadow space | 调用者预留 32 字节 | 无 |
| 栈对齐 | `call` 时 16 字节 | 同为 16 字节（但 `syscall` 本身不要求） |
| 非易失寄存器 | `rbx rbp rdi rsi r12-r15` | `rbx rbp r12-r15`（**rdi/rsi 是易失的**） |
| `syscall` 额外破坏 | — | `rcx`（返回地址）和 `r11` |

最后一条容易中招：`call write` 之后 `rcx` 就没了，所以循环计数别放 `rcx`。
（注意 Linux 的真内核传第 4 个参数用 `r10` 而不是 `rcx`，goa 的 extern 桩做的是
`mov rax,<号>; syscall`，`rcx` 里的第 4 个参数会被 syscall 指令自己冲掉 —— 想传
4 个参数就直接用 `r10`，别指望桩替你搬。）
