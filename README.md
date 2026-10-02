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

Windows 产物只导入 **Windows 系统 DLL**（kernel32/user32/gdi32
导出，按程序实际调用取子集；每个 API 的归属 DLL 写在同名头文件的 `extern ... , dll` 原型里，例如
`winbase.h` 里的 `extern BOOL CloseHandle(HANDLE), kernel32;`）；Linux 产物是静态 ELF，一条动态链接都没有，只用
syscall（`write` / `read` / `brk` / `exit_group`）。两头都没有 msvcrt / glibc，
也没有 gcc。

## 下载与发布

GitHub Release 由 `v*` tag 触发，产出版本化 zip（如
`goc-v0.0.1-windows-x86_64.zip` 与 `goc-v0.0.1-linux-x86_64.zip`）。zip 里是
**开箱即用**的工具链目录：解压后 `goc.exe` 会去找自己**旁边**的 `goa.exe`
（Linux 同理，找旁边的 `goa`），所以 zip 内的文件名不带版本号——版本在
zip 文件名和 Release 的 tag 上。Windows zip 含 `goc.exe` / `cc.exe` /
`goa.exe`，Linux zip 含 `goc` / `goa`，均附 `README.md` / `LICENSE`，另有一份
`SHA256SUMS.txt` 校验所有资产。

## 目录结构

```
.
├── src/                                                # 编译器源码（go 模块 goc）
│   ├── lexer.go  parser.go  ast.go  types.go  headers.go
│   ├── check.go  codegen.go  cpp.go  main.go
│   ├── goa/                                            # goa：汇编器（独立 go 模块）
│   │   ├── asm.go  pe.go  elf.go  main.go              #   Intel 语法子集 -> PE32+ / ELF64
│   │   ├── examples/  expected/  run_tests.sh          #   goa 的用例与 golden
│   │   └── README.md                                   #   汇编器自己的文档
│   ├── goclib/                                         # 自带的 C 库（见下「goclib」一节）
│   │   ├── os.c                                        #   5 个平台原语，唯一碰 OS 的文件
│   │   ├── stdio.c  stdlib.c  string.c  ctype.c        #   45 个库函数
│   │   ├── goclib.h                                    #   伞头
│   │   ├── stddef.h  stdarg.h  stdio.h  stdlib.h       #   内置标准头（可被 #include）
│   │   │   string.h  ctype.h
│   │   ├── windows.h  windef.h  winbase.h  wingdi.h  winuser.h
│   │   └── README.md                                   #   库的实现机制
│   └── examples/*.c  expected/*.txt                    # goc 的用例与 golden
├── tools/                                              # 验证工具（独立 go 模块）
│   ├── elfcheck                                        #   ELF 结构校验（不再解释执行）
│   ├── msgboxcheck                                     #   驱动 GUI 对话框并断言
│   └── ucrun.py  peun.py                               #   ELF / PE 的 Unicorn(QEMU) 运行器
├── bin/                                                # 构建产物（goc / goa / elfcheck / msgboxcheck / goc-out）
├── build.sh  run_tests.sh  run_tests_linux.sh          # 构建 / 测试（本机 / Linux 真内核）
└── .github/workflows/ci.yml                            # CI：Linux 原生端到端 + Windows 端到端
```

`src/goa/` 是独立的 go 模块（自己的 `go.mod`），可以单独拿出来用：给一份 `.asm`，
直接出 exe，不需要 goc。同理 `src/`、`src/goa/`、`tools/` 是**三个** Go 模块，
在 `src/` 里跑 `go test ./...` 是看不到 goa 的单测的。

## Linux 目标

`-target linux` 会让 goc 换一套东西：

- **调用约定**从 Win64（rcx/rdx/r8/r9 + 32 字节 shadow space）切成 SysV
  （rdi/rsi/rdx/rcx/r8/r9，无 shadow space）。
- **goclib 按 `__linux__` 分支编译**（`#if defined(_WIN32) / #elif defined(__linux__)`）：
  `__goclib_write` 走 `write` syscall，堆分配用 `brk` 做 bump allocator
  （`__goclib_heap_free` 是空操作，进程退出时一起还），`exit` 转调 `exit_group`(231)。
- 参数上限相应从「4 个寄存器 + 栈」变成「6 个寄存器 + 栈」。

有个坑值得一提：Linux 下 goa 给每个 extern 生成的 syscall 桩**就叫 extern 的名字**，
所以库若自带 `exit`，就不能再 import 同名 extern 桩（符号冲突 + 无限递归）。现在的
做法：入口桩的 `call exit` 通过 need 闭包拉取 C 版 `exit` 函数体，且仅当 C 库**不**
提供 `exit` 时才注入该 extern。

Windows 上没法 exec ELF，所以本机这一腿交给 **QEMU 的 CPU 核心**：`tools/ucrun.py`
用 Unicorn（QEMU 的 TCG 翻译核心做成库）把 ELF 映射进去真跑 — 真指令语义、真
标志位、真 SSE2、真地址越界报错。这不是可选的锦上添花：历史上 `phase1.c` 有一段
把栈指针塞进 `int` 的未定义行为，手写解释器对未映射地址一律返回 0，于是「输出
逐字节正确、退出码 0」地掩盖了它；换 Unicorn 跑第一次就段错误在 `0xffffffffffe78`
（Linux 栈地址超过 2³¹，32 位截断后符号扩展成了负地址），这才修掉。

所以职责是这样分的：

- `tools/elfcheck` 用 `-structure-only` 只做**结构**校验（magic / class / 类型 / 机器 /
  entry 是否落在可执行段内 / `p_vaddr ≡ p_offset (mod p_align)` / 节表与 `.shstrtab`
  是否自洽）。这一层与指令语义正交，并且是我们自己的断言。
- **`tools/ucrun.py`（Unicorn/QEMU）裁定程序输出是否正确。** 解释器就算跑对了，
  证明的也只是「和我们对 ISA 的理解一致」。

真正的内核验证交给 CI：`.github/workflows/ci.yml` 的 Ubuntu job 会用
`run_tests_linux.sh` **直接执行**所有 Linux ELF 目标（不走任何模拟），真实内核 +
真实 SSE2 + 真实栈随机化，这才是双目标最硬的证明。

## goclib：自带的 C 库

`printf` 不是编译器里的一段魔法字符串，而是一个真正的库。整套库是**纯 C**，
按标准头拆成四个文件，全部 platform-specific 的东西收在第五个文件里：

| 文件 | 内容 | 个数 |
| --- | --- | --- |
| `os.c` | 平台原语：`__goclib_write` / `_read` / `_exit` / `_heap_alloc` / `_heap_free` | 5 |
| `stdio.c` | `printf` `sprintf` `puts` `putchar` `getchar` | 5 |
| `stdlib.c` | `malloc` `free` `calloc` `atoi` `abs` `strtol` `rand` `srand` `exit` | 9 |
| `string.c` | `strlen` `strcpy` `strncpy` `strcmp` `strncmp` `strcat` `strncat` `strchr` `strrchr` `strstr` `strspn` `strcspn` `strpbrk` `strtok` `memset` `memcpy` `memmove` `memcmp` | 18 |
| `ctype.c` | `isalpha` `isdigit` `isalnum` `isspace` `isupper` `islower` `isxdigit` `ispunct` `isprint` `isgraph` `iscntrl` `tolower` `toupper` | 13 |

平台差异封在每个文件顶部的 `#if defined(_WIN32) / #elif defined(__linux__)` 里
（Windows 走 kernel32、Linux 走 syscall）—— 跟普通 C 库用 `#ifdef` 隔离平台相关
代码是一个思路：跨平台的部分只写一遍，只把 OS 相关的部分封进 `#ifdef`。
goc 启动时会注入 `_WIN32`/`_WIN64` 或 `__linux__`/`__linux`，所以库源码自己不需要
在命令行上被告知目标平台。

只有 `os.c` 里的 5 个原语碰操作系统：

| 原语 | Windows（kernel32 extern 直调） | Linux（goa syscall 桩） |
| --- | --- | --- |
| `__goclib_write(buf,len)` | `GetStdHandle`+`WriteFile` | `write`(fd=1) |
| `__goclib_exit(code)` | `ExitProcess` | `exit_group`(231) |
| `__goclib_heap_alloc(size)` | `GetProcessHeap`+`HeapAlloc` | `brk` bump allocator（16B 对齐） |
| `__goclib_heap_free(p)` | `HeapFree` | 空操作（进程退出一起还） |
| `__goclib_read(buf,len)` | `GetStdHandle`+`ReadFile` | `read`(fd=0) |

Windows 侧原语只建立在 kernel32 之上，所以**依赖表里依然没有 msvcrt**；Linux 侧
只依赖 syscall。Win 侧 extern 的归属 DLL 写在内置头文件的原型里（如 `winbase.h` 的
`extern ... , kernel32;`），由 `dllOf` 表在编译期收集，无需单独的中心表。

goc 在启动时把整个库当普通 C 程序编译**两次**（每个目标一次），函数体经常规代码
生成器按需发射：程序**实际调用到**的函数（及其传递闭包）才会进产物，只用
`putchar` 的程序不会背上 `printf` 的 512 字节输出缓冲。库全局变量（如 `rand_state`）
同样按引用打标后发射。

printf 的已知边界：

- 支持 `%d %i %u %o %x %X %s %c %f %g %p %%`；长度修饰符 `l h L z j t` 被解析（所有变参
  槽位都是 8 字节，所以解析掉即等价）。
- **宽度一概忽略**：`%5d` 打 `42`、`%02x` 打 `7`，不会补空格或前导零 —— 这是与标准 C
  明确的差异（代码里有意为之，见 `stdio.c` 里 vfmt 的注释）。
- 精度只有 `%f` 与 `%g` 认：`%.6f` 默认 6 位，`%.0f` 到 `%.17f` 都行；`%g` 用同一套
  定点转换后去掉小数部分的尾零（`2.500000`→`2.5`、`1.000000`→`1`）。注意这是
  **简化版 `%g`**：C 的 `%g` 按有效数字计数并会切换科学计数法，这里按小数位计数
  且没有指数形式。
- 单次调用超过 512 字节会截断；`sprintf` 跟真货一样不做边界检查（缓冲区归调用方管）。
- 没有 `%e %a %n`。
- 库里没有 `scanf`、没有文件 I/O、没有 `math.h`、没有 `time.h`。

## 支持的语言子集

- **标量类型**：`char` `short` `int` `long` 及其 `signed`/`unsigned` 组合、`float`、
  `double`、`_Bool`、`void`；`long double` 落到 `double`
- **聚合类型**：`struct`（嵌套、按值传参、按值返回、成员为数组或结构体）、`union`、
  `enum`、多维数组、指针、函数指针、`typedef`
- **位域**：MSVC 布局规则（跨存储单元分配、`:0` 强制开新单元、无名位域做填充），
  读写走读-改-写
- **方法（UFCS）**：`x.f(args)` / `p->f(args)` 在成员 `f` 不存在时按方法解析——定义
  普通函数 `T_f`（首参为 `struct T` 值或 `struct T*` 指针，T 是 x 的 struct 标签）
  即可写成 `x.f(args)`；指针接收者收到 `&x`（`p->f` 直接收 `p`），值接收者收到 `x`
  （`p->f` 收 `*p`）。成员查找永远优先（函数指针成员不受影响）。纯编译期重写，
  无 vtable、无运行时元数据
- **print 内建**：`print(expr, ...)` 按实参**静态类型**分派到最薄的发射路径——
  `print()` 只换行，单参 `char*`/字符串字面量走 `str_print`、单参 `int`/`char`/`_Bool`
  走 `int_print`、单参 `long` 走 `long_print`，其余才回退 `printf` 拼格式串；带换行、
  返回输出字符数，用户声明的 `print` 函数优先。数组打印 `print(a)` → `[1, 2, 3]`
  与自定义 `T_print` / `T_array_print` 方法见下面「print 内建」一节
- **初始化**：`{}` 初始化列表（嵌套、指定初始化器 `.field =`、数组长度推断）、
  字符数组用字符串字面量初始化、`char *p = "str"`（含全局）
- **存储类与限定符**：`static`（局部持久化，且只初始化一次）、`extern`、`typedef`；
  `const` / `volatile` / `restrict` 接受为限定符，`register` / `auto` 接受为 no-op
  （编译器不做优化，含义上无事可做）
- **控制流**：`if`/`else`、`while`、`for`、`do-while`、`switch`/`case`/`default`
  （含 fall-through 与中途的 `default`）、`break`/`continue`、`goto` 与标号、`return`、
  块作用域、逗号运算符
- **运算符**：`+ - * / %`、`< > <= >= == !=`、`&& ||`、`& | ^ ~ << >>`、`!`、一元
  `- +`、三元 `?:`，以及 `+= -= *= /= %= &= |= ^= <<= >>=` 复合赋值；含操作数的整型
  提升，以及任一操作数为 `double` 时的算术/比较提升
- **`sizeof`**（作用于类型和表达式）
- **变参**：`va_list` / `va_start` / `va_arg` / `va_end`（`stdarg.h`），可以自己写
  printf 风格的函数（`src/examples/variadic.c`）
- **预处理**：`#include`（含自己的头）、`#define`/`#undef`（含函数式宏）、
  `#if`/`#elif`/`#else`/`#endif`、`#ifndef`、`#error`、`#line`（含 GNU 三元组形式）
- **内联汇编**：`__asm { ... }` 块，块内的裸 C 变量名会被绑定成对应的内存操作数 ——
  参数/局部变量 → `[rbp±off]`，全局/static → `[rip+G_x]`；自己写括号的 `[x]` 保持
  原样展开成 `[rbp-8]`（不会套成 `[[rbp-8]]`）

还没到的地方：

- **单编译单元，没有链接器**：不能消费 `.o`，也不能把多个 TU 链到一起
- 没有 `long long`、VLA、复合字面量、`_Generic` 等 C99+ 特性
- 没有数组指定初始化器 `[i] = v`
- 库只有上面那 45 个函数

## print 内建：为体积优化的输出

`print` 不是 `printf` 的别名，而是编译器在 `src/print.go` 里做的**编译期静态分派**
内建。它在编译时看每个实参的静态类型，直接选择最薄的发射路径：

- `print()` —— 只换行
- `print("hello")` / 单参 `char*` —— `str_print`：一个 `write` 调用 + 换行
- `print(42)` / 单参 `int` / `char` / `_Bool` —— `int_print`：整数转文本 + `write`
- `print(42L)` / 单参 `long` —— `long_print`
- 其余（多参、浮点、指针、混搭）—— 回退 `printf` 按格式串发射

三条薄函数（`int_print` / `long_print` / `str_print`）由 goclib 提供，都带换行、
返回输出字符数，语义与 `printf` 的对应格式一致。`print` 是编译期重写，用户自己
声明的 `print` 函数永远优先。

### 数组打印

`print(a)`（裸数组标识符）会把数组打印成 `[1, 2, 3]` 这样的文本：

```c
int a[3] = {11, 12, 13};
long l[2] = {1000000000L, 2000000000L};
double d[3] = {1.5, 2.5, 3.25};
char s[] = "abc";

print(a);   // [11, 12, 13]
print(l);   // [1000000000, 2000000000]
print(d);   // [1.5, 2.5, 3.25]
print(s);   // abc   （char 数组按字符串打印）
```

实现上 goclib 只有一份共享骨架 `__goclib_array_print(a, n, elem_size, conv)`：
按元素字节步进，经函数指针回调把每个元素转成文本；`short/int/long/bool/float/
double` 六个公开函数是薄包装，各自只带一个类型转换器。**新增一种数组类型 =
一个转换器 + 一个包装**。struct/union 数组没有内建格式，编译器会分派用户定义的
`T_array_print(T *a, long n)`（同样带换行）。

### 数组方法

数组也能挂方法：`arr.f(args)` 在成员 `f` 不存在时重写为 `T_array_f(arr, len, args)`，
`len` 是编译期已知的数组长度。比如定义了 `uint_array_add` 后就能写 `uint_arr.add(1)`，
元素拼写是精确的（`uint_array_add` ≠ `int_array_add`）。

## double 支持

`double` 走 SSE2 标量指令：参数放在 xmm0..xmm3（Windows）或 xmm0..xmm7（SysV），
算术用 `addsd` / `subsd` / `mulsd` / `divsd`，整型↔浮点转换用 `cvtsi2sd` /
`cvttsd2si`（截断），比较用 `ucomisd` 再跟无符号跳转（jb/ja/jbe/jae/je/jne）。
常量落在 `.data` 的 `dq` 里，RIP 相对寻址读取。

`float` 内部统一当成「有效宽度等于 double」处理，`cvtsd2ss` 收窄后必须紧跟
`cvtss2sd` 重新拓宽，免得中间结果丢精度。

goclib 的 `%f` 先截出整数位，再把小数部分乘 10 逐位压出，第 prec 位按**四舍五
入到偶数**进位（必要时向整数位进位）。`%f` 的变参槽位传的是 8 字节 IEEE-754 位
模式（`movq rax, xmm0`）。

浮点语义的裁定同样交给 QEMU：Windows 上由 Unicorn 执行 Linux 目标，与 Windows
和 Windows 目标的 golden 逐字节比对；CI 的 Ubuntu job 还会把 `fp` 的 Linux ELF
**直接跑在真实内核上**再比一次，覆盖模拟器验证不到的地方（真实 syscall、栈布局、
`and rsp,-16` 对齐后的 16 字节 `xorpd` 等）。

## 调用约定

默认遵循 Windows x64 ABI：整数参数走 RCX/RDX/R8/R9，第 5 个起放 `[rsp+32]`；
调用者预留 32 字节 shadow space，每个 `call` 处 RSP 保持 16 字节对齐。压栈参数
时多申请的空间会向上取整到 16，对齐才不会被破坏。二元表达式的左操作数溢出到
rbp 相对栈槽（而不是 `push`/`pop`），也是同一个原因。

`-target linux` 时走 SysV AMD64：参数走 RDI/RSI/RDX/RCX/R8/R9，第 7 个起放
`[rsp]`（没有 shadow space）。栈帧里因此少算 32 字节。

结构体按值传递时，实参传的是「值的地址」，被调方把它逐字节拷进自己的帧；返回
结构体走隐藏指针（`argRegs[0]`）。两边相同。寄存器参数都会被溢出到被调方**自己的**
帧里，不依赖调用方的暂存区。

goclib 的变参函数（`printf` / `sprintf`）在入口处把寄存器里的变参和栈上的一起
收集到 `__goclib_va`，然后才做第一次 `call` —— 否则寄存器里的变参会先被冲掉。
Windows 下变参从 rdx 起、栈上在 `[rbp+48]`；Linux 下从 rsi 起、栈上在 `[rbp+16]`。

## 测试

```bash
bash build.sh                       # 构建 goc / goa / elfcheck / msgboxcheck
bash run_tests.sh                   # 本机全量（见下）
bash run_tests_linux.sh             # 在真 Linux 上直接 exec ELF（CI 的 Ubuntu job 跑它）
cd src/goa && bash run_tests.sh     # 只跑汇编器自己的用例
```

`bash run_tests.sh` 一次做八件事，最后一律汇总 `pass=N fail=M`，非零 fail 退出码非 0：

1. 构建 goc / goa / elfcheck
2. 找一个能 `import unicorn` 的 Python（`GOC_PYTHON` 可覆盖；找不到就**大声跳过**
   Linux 腿而不是假装通过）
3. **Windows 腿 ×3**（-O0/-O1/-Os 各 43 项）—— 41 个例子 vs golden 逐字节比对，另有
   `winbox`（MessageBox）和 `winreg`（注册窗口类 + `GetMessage` 消息循环 + WM_PAINT）
   两个 GUI demo 刻意没有 golden，只验证 user32 那些导入能编译链接。-O1/-Os 腿是优化
   轨道的行为护栏：**与 -O0 比对同一份 golden**，任何 pass 改变可观察输出都在这里炸
4. **Linux 腿 ×3**（-O0/-O1/-Os 各 40 项）—— 同一批例子出 ELF，先用
   `elfcheck --structure-only` 校验结构，再由 `tools/ucrun.py` 在 Unicorn（QEMU TCG）
   里执行，与**同一份** golden 比对。依赖 Win32 DLL 的 3 个例子跳过。没有 unicorn
   时这一对腿整段跳过（见下）
5. **委托 `src/goa/run_tests.sh`**：11 个 Windows 例子 + 3 个 Linux 例子 + 1 个 GUI
   （`msgboxcheck` 真的去点对话框的「是」）
6. **三个 Go module 各自跑单测**：src 87 项、src/goa 33 项

本机（Windows 11 + MSYS2 的 Python 带 unicorn 2.1.4）现状 **`pass=253 fail=0`**。

CI 里两个 job 是分工关系，不是重复：

| | Windows runner | Ubuntu runner |
| --- | --- | --- |
| 执行内容 | 126 项 Windows 目标（-O0/-O1/-Os 各 42）+ 委托的 goa 套件 + 三个模块单测 | `run_tests_linux.sh`：ELF 直接跑在**真实内核**上 |
| Linux 目标 | 需要 Python + unicorn，runner 没装，于是**明确跳过**（脚本会打 WARNING，不会静默算通过） | **39 个 goc 例子 + 3 个 goa 例子全跑**，逐字节比对同一份 golden |
| GUI 用例 | `GOC_SKIP_MSGBOX=1` 跳过（需要交互式桌面） | —— |

Ubuntu 那一腿值得多说一句：**它以前从来没真正跑过。** job 的构建步骤里混进了
`tools/msgboxcheck` —— 它是驱动真实 Windows 对话框的程序，只存在于 Windows，于是 Linux
上 `go build` 直接「build constraints exclude all Go files」失败，整个 job 在到达 e2e
之前就结束了。也就是说，过去所有「Linux 目标经过真内核验证」的说法，其实没有任何一次
CI 跑来验证过。修好后第一次跑就是 42/42 全绿，而且产物字节数与本机 Unicorn 下逐个
相同（`fp` 两边都是 14392、`phase1` 都是 4030）——代码生成是确定的，无关宿主。

顺带一个 goc 用法的坑：`-o` 的语义照抄 gcc，**路径不存在时它表示的是输出文件名而不是
目录**。所以测试脚本必须先 `mkdir -p bin/goc-out`；少了这一步，第一个例子会写出一个叫
`bin/goc-out` 的**文件**，后面所有例子都 `Not a directory`（这就是 `run_tests_linux.sh`
当年的实际状况）。

两个环境变量：

- `GOC_PYTHON=/path/to/python` —— 指定带 unicorn 绑定的解释器
- `GOC_SKIP_MSGBOX=1` —— 跳过 GUI 对话框用例；它需要交互式桌面，CI runner 没有，
  Windows job 里设了这个

`src/examples/goclib.c` / `goclib2.c` / `goclib_test.c` 把整个库跑一遍，两个平台的
输出与同一份 golden 逐字节比对。

关于 Linux 腿为什么换成 QEMU，见上面「Linux 目标」那一节 —— 一句话：手写解释器
最多证明 codegen 和自己一致，证明不了程序真的对。

## 优化

`-O` 系列被真正解析（拼法照 gcc 语义映射：`-O`/`-Og`→1、`-O1`→1、`-O2`/`-O3`→3（gcc 的
-O2 本就内联）、`-Os`/`-Oz`→2、`-Ofast`→3，认识不了的照旧忽略）。护栏是硬性的：
**`-O0`（默认）输出与历史管线逐字节相同**，所以同一批 golden 永远成立；`-O1` 起才在指令
流上跑优化 pass（内联 → 常量传播/折叠 → 窥孔 → 死存储消除 → 跨块死存储消除），
`run_tests.sh` 的 -O1/-Os 腿拿**同一份** golden 裁定行为。

`-O1` 现在做五件事，全部跑在全程序指令流（`[]Inst`，标签/内联 asm 边界显式）上：

1. **小函数内联**：无调用（叶子）、无内联 asm、不写 callee-save 寄存器、无栈参数/
   变参的函数成为模板。调用点展开时剥掉 prologue/epilogue（`ret` 序列换成跳到续点的
   `jmp`），形参槽重映射进调用方帧（调用方 `sub rsp` 随之增长），内部标签按调用点加
   后缀；Windows 的 `sub rsp, 32` shadow space 三明治随 call 一起删除。递归、函数指针
   调用、超寄存器参数的函数自动排除。
2. **常量传播 + 折叠**：跟踪全部寄存器与栈槽上的已知值。槽里的常量前向到任何 load
   （`mov rax, [x]` → `mov rax, 37`），目的寄存器已持值时整条 load 消失；双操作数
   整数 ALU（add/sub/and/or/xor/imul/shl/sar/shr）在两个操作数都已知时**求值**——
   指令本身照发（flags 语义不动），只是值进了跟踪器，`add(3,4)` 内联后整条表达式
   折叠成 `mov rax, 7` 的链条。除法、SSE、变参一律不碰。
3. **死存储消除**：同一个槽在无人读取之前被第二次全宽 store 覆盖，前一条直接删——
   这配对吃掉了内联展开后「实参 spill→load 已被前向」留下的纯膨胀。`lea`（取地址）
   和任何间接/带尺寸访存都按「可能读」保守处理，标签/调用/跳转截断窗口。
4. **IR 级窥孔**：`mov <reg>, 0` → `xor <reg32>, <reg32>`（2 字节清零整个寄存器）；
   消除紧邻的冗余访存（存后立即读回同一寄存器的 load、相邻重复 load）。
5. **跨块死存储消除**（CFG + 反向 liveness）：按函数切段、建基本块与边、反向不动点求
   存活集，删掉任何路径都读不到的 store——线性窗口外的跨块覆盖链、call 之后的死
   spill（被调方写不到调用方未逃逸的栈槽）、`ret` 前无读者的 store。间接访存/内联
   asm/形状不明的指令让整段退出；`lea` 取址的槽永久存活；窄访问毒化槽位。

内联 `__asm{}` 一律不碰 —— 其中指令的 flags 语义只有作者知道（-O0 的文本窥孔会改写
用户 asm 里的 `mov eax, 0`，-O1 不会）。pass 只做局部改写、绝不跨标签移动代码；一切
不确定的形状（解析不了的指令、带尺寸/间接的访存、控制流边界）都按「知识失效」处理。
内联是时间换空间：互相调用多的小函数（ctype/struct 系列）-O1 会变大一些，运行输出
仍由同一份 golden 裁定。

`-Os`/`-Oz` 是体积优先档：关掉唯一会变大代码的 pass（内联，例集实测 +17KB），其余清理
pass 照跑——全例集 -Os 比 -O0 还小约 23KB、比 -O1 小约 61KB。`-O2`/`-O3`/`-Ofast` 落在
最激进档（pass 集与 -O1 相同）。路线上的下一层是跨块 copy-prop（liveness 基建直接
复用）、寄存器级分配。

## 体积对比

同一份 Windows PE32+ 源码，gcc 16.2.0（MSYS2 ucrt64，`-O2`，加不加 `-static` 结果
一样）与 goc+goa：

| | gcc -O2 | goc + goa | 差 |
| --- | --- | --- | --- |
| `print("Hello, world!")`（goc 内建） | gcc 没有此内建 | **2048 字节**（-O0 起即是） | — |
| `printf("Hello, world!\n")` | 38989 字节 | 13312 字节（-O1/-Os） | goc 小 65.9% |
| 数组打印（`print(a)` 等四个数组） | 40477 字节¹ | **8192 字节** | goc 小 79.8% |
| `src/examples/stress.c` | 40784 字节 | 15360 字节（-O1） | goc 小 62.3% |

¹ gcc 没有 `print` 内建，用等价的 `printf` 手写实现（同一份数据、同一份输出，
  输出文本与 goc 版逐字一致）。

同一个「Hello, world!」：goc 的产物是 **2048 字节**，gcc 写同样一句话要
**38989 字节** —— 小 **94.7%**（约 1/19），而且 `-O0` 就是这个体积。即使两边都写
`printf`，goc 也只要 13312 字节。数组打印用 `print` 是 8192 字节，gcc 手写等价的
printf 版本要 40477 字节。

差的大头是 CRT 启动代码和整个 printf 家族。goc 侧**按需发射**：程序实际调用到的
函数（及其传递闭包）才进产物，只用 `print` 的程序不会背上 printf 的 512 字节输出
缓冲；`phase1.c` 的 ELF 只有 4030 字节。汇编器自己的产物更小，
`src/goa/examples/hello.exe` 是 **1536 字节**（做法见 `src/goa/README.md` 的「输出
体积」一节）。

## 许可

MIT，见 [LICENSE](LICENSE)。
