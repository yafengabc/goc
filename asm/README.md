# a0 — 纯 Go 的 x86-64 汇编器 + PE / ELF 生成器

不依赖 gcc / nasm / 任何外部工具链：Go 标准库解析 asm，直接输出可执行文件。

- 默认 **Windows PE32+**（导入表 + IAT）
- `-f elf` 输出 **Linux ELF64**（静态、无 libc、无动态链接器）

```bash
go build -trimpath -ldflags="-s -w" -o a0.exe .
./a0.exe examples/hello.asm              # -> examples/hello.exe
./a0.exe examples/hello.asm out.exe      # 指定输出名
./a0.exe -f elf examples/linux/hello.asm # -> examples/linux/hello（Linux 惯例无后缀）
```

## Linux ELF 目标

```asm
extern write          ; SYS_write，a0 生成 mov rax,1; syscall; ret 桩
extern exit           ; SYS_exit

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

- **没有 DLL 可导入**。`extern <name>` 里的名字必须是 a0 认识的 Linux 系统调用名
  （`write` `read` `open` `close` `brk` `mmap` `exit` `exit_group` `getpid`
  `nanosleep` `writev` …），否则直接报错。a0 为每个 extern 生成一个桩
  `mov rax, <号>; syscall; ret`，所以 `call write` 就是一次 syscall。
- 也可以直接写 `syscall` 指令（见 `examples/linux/raw.asm`）。
- 参数是 **SysV AMD64** 顺序：`rdi, rsi, rdx, rcx, r8, r9`，没有 shadow space。
- `_start` 不能 `ret` 回来，必须自己 `exit`。
- 镜像布局：单个 `PT_LOAD` 覆盖整个文件，`0x400000` 起 RWX，
  `0x80` 开始是 `.text`（前面是 ELF 头和一个程序头）。

标签以 `.` 开头（`.loop:` / `.Lrec:`）是**局部标签**，作用域限定在最近一个全局
标签之内 —— 两个函数可以各有一个 `.Lrec` 而不会互相串（早期版本所有标签都是全局的，
`fact` 的 `jg .Lrec` 会跳进 `fib` 的 `.Lrec`，这个坑终于填上了）。

## 源文件语法

```asm
section .data
msg          db "Hello, world!", 10, 0
buf          db 32 dup(0)
count        dq 0

section .text
global _start

extern GetStdHandle, kernel32
extern WriteFile,    kernel32
extern ExitProcess,  kernel32

_start:
    and rsp, -16          ; Win64: 入口处手动对齐栈
    sub rsp, 48           ; 32 字节 shadow space + 16 字节余量
    mov rcx, -11
    call GetStdHandle
    mov [rip+hStdout], rax
    ...
    mov rcx, 0
    call ExitProcess
```

- 入口符号固定为 `_start`（`global _start`）。
- `extern <func>, <dll>` 声明导入；调用 `call <func>` 会自动走 IAT。
- `subsystem windows` 生成 GUI 程序（不分配控制台），默认是 `subsystem console`。
  `du "文本"` 定义 UTF-16LE 字符串并自动 NUL 结尾，可直接交给 `MessageBoxW` 这类
  Wide-char API —— 用 `MessageBoxA` + UTF-8 字节在中文 Windows（GBK）下会乱码。
- 标签以 `.` 开头的（`.loop:`）是局部标签，`.Lxxx` 亦可。
- 注释用 `;`。

## 支持的指令

| 类别 | 指令 |
|---|---|
| 数据 | `db`、`dq`（支持 `N dup(v)`）、`du`（UTF-16LE 字符串，自动补 NUL） |
| 传送 | `mov`、`lea`、`push`、`pop` |
| 算术 | `add` `sub` `and` `or` `xor` `cmp` `imul` `idiv` `div` `inc` `dec` `neg` |
| 控制 | `call` `ret` `jmp` `je` `jne` `jl` `jge` `jle` `jg` `jb` `jae` `jbe` `ja` `js` `jns` `jo` `jno` |
| 其他 | `nop`、`int3`、`syscall`（`0F 05`，Linux 目标） |

## 支持的操作数形式

```
mov rax, 123              ; reg, imm64
mov rax, rbx              ; reg, reg
mov al, bl                ; 8 位寄存器
mov rax, [rip+sym]        ; RIP 相对载入
mov [rip+sym], rax        ; RIP 相对存储
mov [rip+buf], al         ; 8 位 RIP 相对存储
mov byte [rip+buf], 0x30  ; 立即数存储（仅 imm8）
mov [rbp-8], rax          ; [base±disp]
mov [rbx+rcx*8], rax      ; [base + index*scale]
mov [rbx+rcx*8+16], rax   ; [base + index*scale + disp]
mov rax, [rbx+rcx*8]      ; 同上，载入
lea r13, [rip+digits]     ; RIP 相对取址
lea rax, [rbx+rcx*8+16]   ; 寄存器取址
```

支持 `byte` / `word` / `dword` / `qword` / `ptr` 大小前缀（会被剥掉，
实际宽度由寄存器或 `db`/`dq` 决定）。

## 已知限制

- 只生成 PE32+（x86-64 Windows）和 ELF64（x86-64 Linux），不产出 Mach-O / 其他架构。
- 立即数存储仅支持 imm8（`mov [mem], imm32` 只有 `[base+disp]` 形式支持 imm32）。
- 无宏、无 `%include`、无 `resb` 等 NASM 伪指令。
- 段只有 `.text` / `.rdata` / `.data`，输出时 `.rdata`、`.data` 和导入表会被
  合并进同一个 `.data`（见下节）。

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

## 测试

```bash
bash run_tests.sh
```

会把 `examples/*.asm` 全部汇编、运行，并与 `expected/*.txt` 逐字节比对；
再把 `examples/linux/*.asm` 汇编成 ELF 并交给 `tools/elfcheck` 跑一遍。
当前 **13/13** 通过（10 个 Windows + 3 个 Linux）。

### 本机跑不了 ELF，怎么验证？

Windows 上没法 exec 一个 ELF，所以 `tools/elfcheck` 做两件事：

1. **结构检查**：按内核加载器的视角逐字段校验（magic / class / 类型 / 机器 /
   entry 是否落在可执行 `PT_LOAD` 内 / `p_vaddr ≡ p_offset (mod p_align)` /
   节表和 `.shstrtab` 是否自洽）。
2. **指令级执行**：把镜像映射到内存，从 entry 开始解释执行，遇到 `syscall` 时
   实现 `write`（真的写到 stdout）、`exit`、`brk`（一个 1 MiB 的堆）。
   不支持的 opcode 会**大声报错**而不是猜。

所以 `examples/linux/num.asm` 那种输出 `fact(5) = 120` / `fib(10) = 55` 的用例
是有意义的验证：数值算错、递归栈错、syscall 参数错都会立刻暴露。
（仍建议在有 Linux 的机器上真跑一次，解释器不能替代真实内核。）

`msgbox.asm` 是 GUI 程序，没有 stdout 可比，所以走 `tools/msgboxcheck`：启动 exe、
按标题找到对话框（纯 Go syscall 调 `FindWindowW`）、读出实际显示的正文、用
`GetDlgItem(IDYES)` + `BM_CLICK` 点「是」、确认程序走了 IDYES 分支、再关窗检查退出码。
**需要交互式桌面**，锁屏时会失败。

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
|---|---|---|
| 参数寄存器 | `rcx rdx r8 r9` | `rdi rsi rdx rcx r8 r9` |
| shadow space | 调用者预留 32 字节 | 无 |
| 栈对齐 | `call` 时 16 字节 | 同为 16 字节（但 `syscall` 本身不要求） |
| 非易失寄存器 | `rbx rbp rdi rsi r12-r15` | `rbx rbp r12-r15`（**rdi/rsi 是易失的**） |
| `syscall` 额外破坏 | — | `rcx`（返回地址）和 `r11` |

最后一条容易中招：`call write` 之后 `rcx` 就没了，所以循环计数别放 `rcx`。
