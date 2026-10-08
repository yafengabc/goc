# goc + goa —— 不依赖 gcc 也不依赖 libc 的迷你 C 工具链

两个纯 Go 程序，同一份 C 源码可以出两个平台的可执行文件：

```
  foo.c ──[goc]──> foo.asm ──[goa]──> foo.exe    Windows PE32+，只导入 kernel32
              └─[goc -target linux]─[goa -f elf]─> foo    Linux ELF64，只用 syscall
```

`goc -c` 可以生成中间文件（.OBJ .O）；`gocld`（已编进 `goc.exe`）
是独立的链接程序：

```
  foo.c ──[goc -c]──> foo.o ──[gocld]──> foo.exe
                        (COFF / ELF64)     └─ .o 也可以来自别处，按需混入 .c
```

而gocl则是goc+llvm后端，能利用强大的llvm生成更紧凑，性能更强的程序，并且能生成多个平台的程序（linux）
goc+goa则只能生成x86-64的程序。

另外我自己裁剪了一个14M的libllvm.dll,跟gocl.exe放到同意目录即可使用。

```bash
bash build.sh       # 一条命令：goc + goa + 两个测试工具（见下面目录结构）

./bin/goc.exe src/examples/hello.c                   # 编译并链接（Windows）
./bin/goc.exe -c src/examples/hello.c                # 只编译：出 hello.o
./bin/goc.exe -S src/examples/hello.c                # 只输出汇编（hello.asm）
./bin/goc.exe -c -o bin/goc-out src/examples/hello.c # 产物集中到 bin/goc-out/，不污染源码树
./bin/goc.exe -c -target linux src/examples/hello.c  # 出 Linux ELF64 对象（无后缀）
./bin/goc.exe a.c b.c -o app.exe                     # 多个 .c 编成一个可执行

# 分开的两步，和 gcc 一样
./bin/goc.exe -c a.c && ./bin/goc.exe -c b.c         # a.o  b.o
./bin/goc.exe a.o b.o -o app.exe                     # 链接
./bin/goc.exe -c a.c && ./bin/goc.exe a.o b.c -o app.exe   # 也可以混着来
```

多个 `.c` 各自是一个**独立的翻译单元**（宏、typedef、struct 标签互不干扰）。
一次编译多个 `.c` 时它们各自产出目标文件、随后一起链接；也可以先`-c` 再单独链接，
两种方式下 `static` 的符号都只在本文件可见，两个文件都定义的同名**外部**符号
才是重复定义错误。

`static` 符号写进目标文件时会带上对象自己的名字（`unit.o`里的 `scale` 记作
`unitscale`）并标记为内部链接（COFF 的 `IMAGE_SYM_CLASS_STATIC` / ELF 的
`STB_LOCAL`）。改名是必须的：重定位按**符号名**解析引用，两个单元各有一份同名
`static` 函数时若不改名，要么被报成重复定义，要么一个对象的调用落到另一个对象的
函数上——后者更坏，程序能链接、能运行，只是算出错误的答案。

汇编器 goa 已编译进
goc 二进制，不需要旁边放 `goa.exe`（独立的 `bin/goa.exe` 仍然保留，供手写汇编使用）。

链接阶段处理四件事：**重定位**（RIP 相对位移、`_BitInt` 的 sret 隐藏指针、
64 位绝对地址）、**符号合并**（跨单元的未定义符号由提供它的那一份定义）、
**goclib 副本去重**（goc 没有库阶段，每个用 `printf` 的单元都自带一份完整实现，
链接时保留一份、其余丢弃——它们的静态状态本就是单元私有的）、
**资源节**（`.rsrc` 的树形结构按 key 合并，多个 `.o` 里的图标合成一棵
RT_ICON）。目前不做死代码消除，被丢弃副本的字节仍留在 `.text` 里。

`.rsrc` 不是可以照搬的一段数据，而是一棵 `IMAGE_RESOURCE_DIRECTORY` 树，
每个叶子用 `OffsetToData` 记录自己的**文件偏移**。这带来一个循环依赖：叶子的
偏移要等节在文件里的位置定了才知道，而那个位置要等所有节的尺寸加总完才知道，
尺寸又来自树本身。打破它的办法是承认**尺寸与位置无关**——于是先 `size()` 后
`emit()`，两趟走完。多个对象各带一份资源时按 key 合并：同 key 同内容是常态
（两份 `.rc` 编出来的同一份资源），保留一份；同 key 不同内容是重复定义，报错；
一边是目录一边是叶子则无解，也报错。短一层的树（payload 直接挂在类型下）
会被补成 `类型/1/0x0409`——Windows 查 `RT_MANIFEST` 正是按这个路径。资源由
`windres` 等工具生成 `.o` 后交给 gocld；goc 本身不读 `.rc`。

Windows 产物只导入 **Windows 系统 DLL**（kernel32/user32/gdi32
导出，按程序实际调用取子集；每个 API 的归属 DLL 写在同名头文件的 `extern ... , dll` 原型里，例如
`winbase.h` 里的 `extern BOOL CloseHandle(HANDLE), kernel32;`）；这份归属关系由
`.o` 自己带出去（一个 `.goc_dll:` 静态符号），所以隔了对象文件链接也一样准，
不需要链接器猜。Linux 产物是静态 ELF，一条动态链接都没有，只用
syscall（`write` / `read` / `brk` / `exit_group`）。两头都没有 msvcrt / glibc，
也没有 gcc。

## 下载与发布

GitHub Release 由 `v*` tag 触发，产出版本化 zip（如
`goc-v0.0.1-windows-x86_64.zip` 与 `goc-v0.0.1-linux-x86_64.zip`）。

zip 里是开箱即用的工具链目录。C 标准库源码（`goclib/`）已经 **embed 进二进制**
（`src/libembed.go`），所以 **`goc.exe` 单文件就能工作**——拷到空目录、清空 PATH
也能编译运行，不需要旁边有 `goclib/`，也不再读 `GOCLIB_PATH` 环境变量。

实测（2026-10-07）：`goc.exe` 4.9 MB，只导入 `kernel32.dll`；编一个用到
`math.h` / `string.h` / `stdlib.h` 的程序，脱离仓库目录、PATH 只留系统目录，
输出正确。

`gocl` 多一个文件：它依赖 `libLLVM.dll`（约 14 MB，放在 exe 旁边）。两个文件
一起拷走即可，同样不需要 `goclib/`。

zip 里那份独立 `goa` 是给手写汇编用的（`src/goa` 是独立 go 模块），goc 编译 C
不再需要它——汇编器已编译进 `goc` 二进制。

Windows zip 含 `goc.exe` / `cc.exe` / `goa.exe` / `goclib/`，Linux zip 含
`goc` / `goa` / `goclib/`，均附 `README.md` / `LICENSE`，另有一份
`SHA256SUMS.txt` 校验所有资产。

## 目录结构

```
.
├── src/                                            # 自包含入口（go 模块 goc/selfcontained）
│   ├── goc.go                                         #   -tags goc   → bin/goc.exe
│   ├── gocl.go                                        #   -tags gocl  → bin/gocl.exe
│   └── libembed.go                                   #   内嵌的 goclib/（两个 tag 共用）
├── src/frontend/                                   # C 前端（go 模块 goc/frontend，零依赖）
│   ├── lexer.go  parser.go  ast.go  types.go          #   词法 / 语法 / AST / 类型
│   ├── check.go  print.go  ufcs.go                     #   语义检查、print/数组重写、UFCS
│   └── go.mod
├── src/common/                                     # 两后端共享（go 模块 goc/common）
│   ├── preprocess.go                                     #   C 预处理器
│   ├── translate.go                                      #   多翻译单元的读取/解析/合并
│   ├── library.go  source.go                            #   goclib 的定位与编译
│   ├── printfspec.go                                     #   printf 调用点特化
│   └── link/                                             #   入口桩与全局数据发射
├── src/gocld/                                       # 链接器（go 模块 gocld）
│   ├── image.go                                          #   中间表示：段/符号/重定位
│   ├── api.go                                            #   入口：NewImage / IngestCOFF / Build
│   ├── coff.go  coffmerge.go                            #   COFF 解析与对象合并
│   ├── pe.go  elf.go                                    #   PE32+ / ELF64 镜像
│   └── fixup.go                                         #   重定位应用
├── src/gocl/                                        # LLVM 后端编译器（go 模块 gocl）
│   ├── translate.go  function.go  statement.go          #   前端 AST → LLVM IR
│   ├── expression.go  operator.go  call.go  types.go
│   ├── module.go  compile.go  backend.go  util.go
│   └── driver.go                                       #   驱动（flags / 构建流程）
├── src/goc/                                        # 编译器主体（go 模块 goc）
│   ├── codegen.go  cpp.go  multi.go  opt.go            #   自研 x86-64 代码生成
│   └── llvm*.go                                        #   LLVM 后端（-fllvm）
├── src/goclib/                                     # 自带的 C 库（embed 进两个编译器）
│   ├── os.c                                            #   6 个平台原语，唯一碰 OS 的文件
│   ├── stdio.c  stdlib.c  string.c  ctype.c  math.c    #   21 个 .c / 10.3k 行 / 484 个函数
│   │   time.c  file.c  dir.c  threads.c  socket.c ...
│   ├── goclib.h                                        #   伞头
│   ├── stddef.h  stdarg.h  stdio.h  stdlib.h           #   26 个内置标准头（可被 #include）
│   │   string.h  ctype.h  math.h  time.h  stdatomic.h
│   │   threads.h  stdbit.h  stdckdint.h  uchar.h  wchar.h ...
│   ├── windows.h  windef.h  winbase.h  wingdi.h  winuser.h
│   └── README.md                                       #   库的实现机制
├── src/goa/                                            # goa：汇编器（独立 go 模块）
│   ├── asm.go  pe.go  elf.go  main.go                  #   Intel 语法子集 -> PE32+ / ELF64
│   ├── examples/  expected/  run_tests.sh              #   用例与 golden
│   └── README.md                                       #   汇编器自己的文档
├── src/examples/  src/expected/                      # goc 回归套件的数据（gocregress 的输入）
│   └── examples/multi/                                 #   多文件链接用例
├── tools/                                              # 验证工具（go 模块 tools）
│   ├── elfcheck                                        #   ELF 校验 + 解释执行（--structure-only 只校验结构）
│   ├── msgboxcheck                                     #   驱动 GUI 对话框并断言
│   └── ucrun.py  peun.py                               #   ELF / PE 的 Unicorn(QEMU) 运行器
├── bin/                                                # 产物（goc / goc-standalone / goa / ...）
├── build.sh  run_tests.sh  run_tests_linux.sh          # 构建 / 测试（本机 / Linux 真内核）
└── .github/workflows/ci.yml                            # CI：Linux 原生端到端 + Windows 端到端
```

`src/goa/` 是独立的 go 模块（自己的 `go.mod`），可以单独拿出来用：给一份 `.asm`，
直接出 exe，不需要 goc。八个 Go 模块：`src/frontend`（前端，零依赖）、`src/common`（预处理器 + C 库 + 链接）、`src/goc`（自研 x86-64 后端）、`src/gocld`（链接器）、`src/gocl`（LLVM 后端）、`src`（自包含入口）、`src/goa`（汇编器）、`tools`（验证），
在 `src/goc/` 里跑 `go test ./...` 是看不到 goa 的单测的。

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

## 多架构（gocl）

`gocl`（LLVM 后端）有 `-arch`，可选六种目标：

```
gocl -arch aarch64 -target linux hi.c -o hi.elf
```

| `-arch` | 产物 | `e_machine` | 2026-10-08 实测 |
| --- | --- | --- | --- |
| `x86_64` | PE32+ 与 ELF64 | 0x3e | ✅ 默认，完整可用 |
| `aarch64` | ELF64 | 0xb7 | ✅ |
| `arm` | ELF32，hard-float（armhf） | 0x28 | ✅ |
| `armel` | ELF32，soft-float | 0x28 | ✅ |
| `riscv64` | ELF64 | 0xf3 | ✅ |
| `riscv32` | ELF32 | 0xf3 | ✅ |

上表的"✅"是**产物**层面的：每个目标都编译链接出结构合法的 ELF（用 ELF 头的
`e_type`/`e_machine`/`e_ident` 核对过）。**只有 x86_64 在真机上执行过**
（WSL 的 alpine，直接 `exec`）；其余五种本机没有 qemu 用户态模拟，所以"跑起来
对不对"这一层是 CI 的事，不是这张表能证明的。

`arm` 与 `armel` 是两个目标而不是一个目标的两种选项：armhf 的 `double` 运算是
VFP 指令，armel 没有 FPU，每一次 `double` 加/减/乘/除/比较都变成对
`softfloat.c` 里 `__adddf3` 一类函数的**调用**。两者对同一个浮点参数用不同的
寄存器传，编给其中一个的二进制在另一个上不是"慢一点"，而是取到错的值。

多架构要求 `libLLVM.dll` 里含对应后端。判断方法很直接——`objdump -p` 看它导出
哪些 `LLVMInitialize*TargetInfo`：

```bash
objdump -p bin/libLLVM.dll | grep -o "LLVMInitialize[A-Za-z0-9]*TargetInfo" | sort -u
```

仓库当前带的那份导出 `X86` / `AArch64` / `ARM` / `RISCV` 四个。缺哪个后端，
`-arch` 就会报 `libLLVM does not export LLVMInitialize…TargetInfo`——这是产物
边界，不是代码边界；换一份含该后端的 `libLLVM.dll` 即可，不用改 gocl 任何代码。

两点现状值得记：

- **没有除法指令的目标是软实现**：LLVM 会把 64 位除法降到 `__udivdi3` /
  `__divdi3` 一类 libcall（RISC-V 基线 ISA 根本没有除法，ARM 的 64 位商需要
  128 位中间值）。这些由 `goclib/intops.c` 提供，用的是移位-减法，所以它不调用
  任何除法——一个用 `/` 实现 `/` 的版本会绕回自己直到栈耗尽。带上 64 位除法的
  程序在 arm / riscv 上因此能直接编过；`double` 的运算在 armel 上走
  `softfloat.c`，是同一套思路的另一半。
- **`elfcheck --structure-only` 只认 x86-64**（硬编码 `want 0x3e`），拿它校验
  aarch64 产物会误报。产物本身是合法的 ARM64 ELF。

还有一个坑值得单独写，因为它只在浮点上现身、而在整数上完全隐形：**x86-64 Linux
的入口必须先把栈对齐到 16 字节**。内核进入 `_start` 时 `rsp` 已经是 16 对齐的，
而 LLVM 写的 prologue 是给"被 `call` 进来的函数"用的——`call` 会压入返回地址，
于是它假设进来时 `rsp` 是 8 模 16，并再用一条 `pushq` 把它凑回 16。这一次
`pushq` 就把真实栈推到 8 模 16 上，`_start` 之后每一次调用都把偏移八字节的栈
交给被调者。整数路径没有指令在乎，直到某个被调者的 prologue 用 `movaps` 把 SSE
寄存器倒进 save area——`movaps` 对未对齐地址直接 #GP，于是
`printf("%f\n", 1.5)` 在打印任何东西之前就 SIGSEGV。修法是给 ELF 入口换一段
`andq $-16, %rsp` 的汇编 stub（`__goc_entry`），让 `_start` 看到 LLVM 为它编译
时所假设的栈形。这个坑只在 x86-64 上成立：`call` 压返回地址是它独有的，
AArch64 / ARM / RISC-V 进函数时栈指针就是调用者留下的那个。

`goc`（自研后端）只有 x86-64，不接受 `-arch`。

## goclib：自带的 C 库

整套库是**纯 C**，按标准头拆成 21 个 `.c`（约 10351 行），全部 platform-specific
的东西收在 `os.c` 里。当前导出 **484 个函数**（含内部实现符号；想知道某个函数
到底有没有，让编译器去调一个不存在的名字，它会把整个可用清单打进错误信息）。

| 文件 | 内容 |
| --- | --- |
| `os.c` | 平台原语（唯一碰 OS 的文件，见下表） |
| `stdio.c` | `printf` 全家、`scanf` 全家、`FILE *` 文件 I/O |
| `stdlib.c` | `malloc`/`free`/`calloc`/`realloc`/`atoi`/`strtol`/`rand`/`srand`/`exit`/`qsort`/`bsearch`/`div` 等 |
| `string.c` | `strlen`/`strcpy`/`strncpy`/`strcmp`/`strncmp`/`strcat`/`strncat`/`strchr`/`strrchr`/`strstr`/`strspn`/`strcspn`/`strpbrk`/`strtok`/`strdup`/`strndup`/`memset`/`memcpy`/`memmove`/`memcmp`/`memchr`/`memrchr`/`memccpy` 等 |
| `ctype.c` | `isalpha` `isdigit` `isalnum` `isspace` `isupper` `islower` `isxdigit` `ispunct` `isprint` `isgraph` `iscntrl` `tolower` `toupper` |
| `math.c` | C89–C23 数学函数（见下） |
| `time.c` | `time`/`clock`/`timespec_get`/`localtime`/`gmtime`/`mktime`/`strftime`/`difftime` 等 |
| `file.c` `dir.c` | 文件与目录（`fopen`/`fread`/`fwrite`/`fseek`/`opendir`/`readdir`/`stat`/`mkdir`/`rmdir`） |
| `wchar.c` `uchar.c` | 宽字符与 Unicode 转换 |
| `stdbit.c` `bitint.c` | C23 `<stdbit.h>`、`_BitInt(N)` 大整数运行时 |
| `socket.c` `mtx.c` `threads.c` `signal.c` `errno.c` `assert.c` `args.c` `rt.c` | socket、互斥量、线程、信号、errno、assert 等 |

平台差异封在每个文件顶部的 `#if defined(_WIN32) / #elif defined(__linux__)` 里
（Windows 走 kernel32、Linux 走 syscall）—— 跟普通 C 库用 `#ifdef` 隔离平台相关
代码是一个思路：跨平台的部分只写一遍，只把 OS 相关的部分封进 `#ifdef`。
goc 启动时会注入 `_WIN32`/`_WIN64` 或 `__linux__`/`__linux`，所以库源码自己不需要
在命令行上被告知目标平台。

只有 `os.c` 里的 6 个原语碰操作系统：

| 原语 | Windows（kernel32 extern 直调） | Linux（goa syscall 桩） |
| --- | --- | --- |
| `__goclib_write(buf,len)` | `GetStdHandle`+`WriteFile` | `write`(fd=1) |
| `__goclib_read(buf,len)` | `GetStdHandle`+`ReadFile` | `read`(fd=0) |
| `__goclib_exit(code)` | `ExitProcess` | `exit_group`(231) |
| `__goclib_heap_alloc(size)` | `GetProcessHeap`+`HeapAlloc` | `brk` bump allocator（16B 对齐） |
| `__goclib_heap_free(p)` | `HeapFree` | 空操作（进程退出一起还） |
| `__goclib_heap_realloc(p,size)` | `HeapReAlloc` | 分配新块 + 拷贝 |

Windows 侧原语只建立在 kernel32 之上，所以**依赖表里依然没有 msvcrt**；Linux 侧
只依赖 syscall。Win 侧 extern 的归属 DLL 写在内置头文件的原型里（如 `winbase.h` 的
`extern ... , kernel32;`），由 `dllOf` 表在编译期收集，无需单独的中心表。

goc 在启动时把整个库当普通 C 程序编译**两次**（每个目标一次），函数体经常规代码
生成器按需发射：程序**实际调用到**的函数（及其传递闭包）才会进产物，只用
`putchar` 的程序不会背上 `printf` 的 512 字节输出缓冲。库全局变量（如 `rand_state`）
同样按引用打标后发射。

printf/scanf 与默认 C 库的一致性（2026-10-08 起，以 ucrt gcc -std=c11 为对照基准，
`tests/portability_cases/printfmt.c` 逐字节钉住）：

- **printf** 支持 `%d %i %u %o %x %X %c %s %f %e %g %a %p %n %%`（含大小写浮点
  拼写与 `%ls`/`%lc`），旗标 `- + 0 # 空格`，宽度/精度含 `*`（负宽=左对齐、负精度=
  省略）。整数精度（`%.5d`、`%.0d`+0 打空）、`%#x` 仅非零值加前缀、`%#o` 提升精度
  强制前导 0、符号与前缀计入宽度（`%#08x` 恰 8 字符）、`%g` 舍入进位后重判风格
  （`9.999999e5`→`1e+06`）等语义与 C99 一致。
- **scanf**（`scanf`/`sscanf`/`fscanf`/`vscanf` 全家）支持 `%d %i %u %o %x %X %c %s
  %[ %f %e %g %a %p %n %%`，长度修饰符 `hh h l z j t`（`hh` 真写 1 字节）。scanset
  支持 `^` 取反、`a-z` 区间、`]` 字面成员，并按 C99 自动补终止 NUL。EOF 语义
  （未赋值且输入耗尽返回 -1，匹配失败返回 0）、浮点 field 按 token 收集
  （`"1e+x"` 整体失败且 field 已消费、`"1.5-3"` 是两项）、十六进制浮点均已对齐。
- 与 ucrt 的两处**刻意差异**：NaN 不打符号位（ucrt 打 `-nan(ind)`，而符号位在
  硬件 indefinite 与优化器折叠之间不可稳定观测）；`%n` 按标准写入（ucrt 安全
  加固拒绝）。LP64（Linux）平台上 `long` 为 64 位属平台语义，非库差异。
- 单次调用超过 512 字节会截断；`sprintf` 不做边界检查（缓冲区归调用方管）。
- 文件 I/O 已实现：`FILE *`、`fopen`/`fopen_s`/`_wfopen`/`freopen`/`tmpfile`、
  `fread`/`fwrite`/`fgets`/`fputs`/`fgetc`/`fputc`/`ungetc`、
  `fseek`/`ftell`/`rewind`/`fflush`/`fclose`、`feof`/`ferror`/`clearerr`、
  `remove`/`rename`/`perror`。
- `math.h` 已实现（含 `sqrt` `pow` `exp` `log` `sin` `cos` `tan` `asin` `acos`
  `atan` `atan2` `sinh` `cosh` `tanh` `hypot` `cbrt` `expm1` `log1p` `log2` `log10`
  `fmod` `fabs` `floor` `ceil` `round` `trunc` `fma` `copysign` `nan`
  `fpclassify` `signbit` `isnan` 等，以及 C23 的 `fmaximum_num`/`fminimum_num`
  族）。**缺 `long double` 取整族**：`lround`/`llround` 等没有。
- `time.h` 已实现：`time`/`clock`/`timespec_get`/`localtime`/`gmtime`/`mktime`/
  `strftime`/`difftime`/`asctime`/`ctime`。
- `dirent.h` 已实现：`opendir`/`readdir`/`closedir`/`rewinddir`/`stat`/`mkdir`/`rmdir`。
- `signal.h`、`threads.h`（`thrd_`/`mtx_`/`tss_`/`call_once`）、`uchar.h`、
  C23 `stdbit.h` 均已实现。
- socket 层可用 `<socket.h>` 的 POSIX 拼写（Windows 走 `ws2_32`，Linux 走 syscall）。
  `winsock2.h` 是 `socket.c` 的内部实现，**不要直接 include**。

## 支持的语言子集

- **标量类型**：`char` `short` `int` `long` `long long` 及其 `signed`/`unsigned`
  组合、`float`、`double`、`_Bool`、`void`；`long double` 落到 `double`
- **聚合类型**：`struct`（嵌套、按值传参、按值返回、成员为数组或结构体）、`union`、
  `enum`、多维数组、指针、函数指针、`typedef`
- **位域**：MSVC 布局规则（跨存储单元分配、`:0` 强制开新单元、无名位域做填充），
  读写走读-改-写
- **`_Generic`**（C11 类型分派，含字面量后缀定型：已实现，无消息无）
- **`_Alignas`/`_Alignof`**（已实现）
- **`_Thread_local`**：**goa 原生 TLS**（TLS 目录 + `gs:[0x58]`），不是软模拟
- **`_Atomic`**（C11）：标量原子，`++`/`--` 发 `lock xadd`，其余走 `lock cmpxchg` 重试循环
- **`_BitInt(N)`**：任意位宽整数，goclib 大整数运行时（schoolbook + Karatsuba 乘、Knuth D 除、十进制转换）
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
- **复合字面量**（C99，块作用域）：`&(struct P){1,2}` 可用
- **数组指定初始化器**（C99）：`int a[5] = {[2]=7, [4]=9}`
- **C23 实用子集**：`bool`/`true`/`false`、`typeof`、`nullptr`、`constexpr`、
  `_Static_assert`、`enum E : int`、`u8` 前缀、二进制字面量 `0b`、数字分隔符 `'`
  、`#embed`、`__has_include`、`__VA_OPT__`、`#elifdef`/`#elifndef`/`#warning`、
  `[[...]]` 属性、`stdckdint.h`、`stdbit.h`
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

- **VLA**（变长数组）：`int a[n]` 一律解析拒绝
- 链接器只做重定位解析与符号合并，没有真正的死代码消除（被去重的
  库函数副本字节仍留在 `.text` 里）；也没有静态库归档（`.a`——只吃裸 `.o`，
  `ar` 出来的报 `machine 0x3c21 is not AMD64`）
- 资源只做到 PE 的 `.rsrc`：能从 `windres` 之类的 `.o` 读入、合并、写进 exe。
  不解析 `.rc` 源文件，不生成资源，也不做 ELF 侧（ELF 根本没有资源节）
- `%.20f` 起（超过 double 的 17 位有效数字）的十进制展开是近似值；精确展开要等
  `long double`（fp128）的十进制转换机器
- `math.h` 缺 `long double` 取整族（`lround`/`llround`）；`round`/`trunc` 有
- `winsock2.h` 是 `socket.c` 的内部实现，用户应 include `<socket.h>`（POSIX 拼写）

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
cd goa && bash run_tests.sh           # 只跑汇编器自己的用例
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
5. **委托 `goa/run_tests.sh`**：11 个 Windows 例子 + 3 个 Linux 例子 + 1 个 GUI
   （`msgboxcheck` 真的去点对话框的「是」）
6. **三个 Go module 各自跑单测**：goc、goa、tools 各跑 `go test ./...`

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
`goa/examples/hello.exe` 是 **1536 字节**（做法见 `goa/README.md` 的「输出
体积」一节）。

## 许可

MIT，见 [LICENSE](LICENSE)。
