# goclib —— goc 自带的 C 库

goc 不链接任何系统 libc（无 msvcrt / glibc，无 gcc）。它需要的 `printf`、`malloc`、
`strlen`、`isspace` 等都由这里提供——**全部是纯 C 实现**，按域拆成多个 .c 文件，由
goc 在启动时按目标平台自行编译，函数体经常规代码生成器（genFunc）按需发射：程序真正
调用到什么才链接什么，hello world 不为 malloc 买单。

```
goclib/（19 个 .c）：os.c 平台原语 / stdio.c printf 家族 / file.c FILE 层 /
stdlib.c 分配器 / string.c 字符串 / ctype.c 字符分类 / math.c 44 个数学函数 /
time.c 时间 / dir.c 目录 / errno.c / assert.c / rt.c 运行时追踪 / mtx.c 互斥锁 /
threads.c C11 线程 / bitint.c 大整数 / uchar.c / wchar.c / stdbit.c / args.c

goclib.h    伞头：拉入全部标准头 + 六个 __goclib_* 平台原语原型
syscall.h   Linux 系统调用的双宿主路线（见第 7 节）
sys/stat.h  转接头，让 #include <sys/stat.h> 在外部编译器下也能解析
```

## 1. 工作机制（codegen.go 的 buildClibC / genClibFuncs）

- `init()` 里对两个目标各跑一次 `buildClibC(linux bool)`：伞头 goclib.h 作为独立 TU
  先行编译（头内如有实现也会被收集），随后 `goclib/*.c` 按名序编译（ctype.c → os.c →
  stdio.c → stdlib.c → string.c），同名定义**后者胜出**（.c 定义覆盖头定义），重复不会
  到达链接器。
- 平台选择用 `PreprocessTarget` 注入的宏：`_WIN32/_WIN64` 或 `__linux__/__linux`，
  os.c 顶部 `#if defined(_WIN32) / #elif defined(__linux__)` 只留对应分支。
- 所有 `goclib/*.h`（含 ctype.h 等标准头）经 `go:embed goclib/*` 嵌入二进制，新头文件
  自动生效、无需改 embed 列表。标准头被 cpp 层按需注入到用户 TU。注意 `goclib/*` 只
  匹配一层，所以 `goclib/sys/` 下的文件不进 embed——goc 解析 `<sys/stat.h>` 走的是
  basename 匹配（命中 `goclib/stat.h`），那条路径完全不受影响。
- Gen 按 `c.need` 闭包发射 C 库函数（genClibFuncs 不动点：发射一个函数会标记它内部
  调用的更多函数），库全局变量（如 `rand_state`）只在被引用时发射。

## 2. 六个 `__goclib_*` 平台原语（碰 OS 的唯一层面，在 os.c）

| 原语 | Windows（kernel32 extern 直调） | Linux（goa syscall 桩） |
|---|---|---|
| `__goclib_write(buf,len)` | `GetStdHandle`+`WriteFile` | `write`(fd=1) |
| `__goclib_read(buf,len)` | `GetStdHandle`+`ReadFile` | `read`(fd=0) |
| `__goclib_exit(code)` | `ExitProcess` | `exit_group`(231) |
| `__goclib_heap_alloc(size)` | `GetProcessHeap`+`HeapAlloc` | `brk` bump 分配器（16B 对齐） |
| `__goclib_heap_realloc(p,size)` | `HeapReAlloc` | 新分配 + 拷贝（bump 分配器不能原地长） |
| `__goclib_heap_free(p)` | `HeapFree` | 空操作（进程退出一起还） |

## 3. 函数清单（200 个，全部按需链接）

| 头文件 | 数量 | 内容 |
|---|---|---|
| stdio.h | 49 | `scanf` `vscanf` `vfscanf` `vsscanf` `sscanf` `fscanf` `printf` `fprintf` `sprintf` `snprintf` `vprintf` `vfprintf` `vsnprintf` `puts` `putchar` `getchar` `perror`；FILE 层 `fopen` `freopen` `fclose` `fread` `fwrite` `fgetc` `fputc` `fgets` `fputs` `fflush` `ftell` `fseek` `rewind` `feof` `ferror` `clearerr` `ungetc` `remove` `rename` `tmpfile` `setvbuf` `setbuf` `tmpnam`；薄打印 `int_print`/`long_print`/`str_print` 与 6 个 `*_array_print` |
| math.h | 50 | 舍入/余数 `fabs` `floor` `ceil` `trunc` `round` `fmod` `modf` `rint` `nearbyint` `remainder` `fma`；幂/对数 `sqrt` `cbrt` `pow` `exp` `exp2` `expm1` `log` `log2` `log10` `log1p`；三角/双曲 `sin` `cos` `tan` `asin` `acos` `atan` `atan2` `sinh` `cosh` `tanh`；分解 `frexp` `ldexp` `hypot` `fmin` `fmax` `ilogb` `logb`；特殊 `erf` `erfc` `tgamma` `lgamma`；符号 `signbit` `copysign` `fdim` `nan`；扩展 `nextafter` `nexttoward` `scalbn` `scalbln` `remquo` `fpclassify` |
| stdlib.h | 29 | `malloc` `free` `calloc` `realloc` `atoi` `atol` `atoll` `atof` `abs` `labs` `llabs` `strtol` `strtoul` `strtod` `div` `ldiv` `lldiv` `qsort` `bsearch` `getenv` `atexit` `at_quick_exit` `quick_exit` `abort` `rand` `srand` `exit` `aligned_alloc` `system` |
| string.h | 24 | `strlen` `strcpy` `strncpy` `strcmp` `strncmp` `strcat` `strncat` `strchr` `strrchr` `strstr` `strspn` `strcspn` `strpbrk` `strtok` `memset` `memcpy` `memmove` `memcmp` `memchr` `strdup` `strnlen` `stpcpy` `strndup` `memrchr` |
| ctype.h | 13 | `isalnum` `isalpha` `iscntrl` `isdigit` `isgraph` `islower` `isprint` `ispunct` `isspace` `isupper` `isxdigit` `tolower` `toupper` |
| time.h | 16 | `time` `clock` `difftime` `gmtime` `localtime` `mktime` `strftime` `asctime` `ctime` `gmtime_r` `localtime_r` `asctime_r` `ctime_r` `timespec_get` `clock_gettime` `tzset` |
| errno.h | 1 | `strerror`（外加 `errno` 本身） |
| threads.h | 24 | 线程 `thrd_create` `thrd_equal` `thrd_current` `thrd_detach` `thrd_join` `thrd_exit` `thrd_sleep` `thrd_yield`；互斥锁 `mtx_init` `mtx_destroy` `mtx_lock` `mtx_trylock` `mtx_unlock` `mtx_timedlock`；条件变量 `cnd_init` `cnd_destroy` `cnd_signal` `cnd_broadcast` `cnd_wait`；线程本地存储 `tss_create` `tss_get` `tss_set` `tss_delete`；`call_once` |

`<threads.h>` 是双平台实现（见 `threads.c`）：Windows 走 CreateThread +
CRITICAL_SECTION + CONDITION_VARIABLE + Tls*，Linux 走 `clone`(56) + mmap 栈 +
`futex`(202) + `gettid`(186)。Linux 侧的三个已知限制都写在 `threads.c` 的头注释里：
detached 线程的栈不回收、`_Thread_local` 在所有线程间共享（clone 没带 CLONE_SETTLS）、
堆分配器不是线程安全的。

`float` 不是独立精度：goc 把 float 当"有效 double"处理，所以没有 `fabsf`/`powf`
那一族，也没有 `long double`。用到这些名字会在 codegen 报 unknown function，
这比给出一个悄悄算错的结果好。

### 3.1 printf 的浮点：%e / %g 与舍入

`%f` `F` `e` `E` `g` `G` 共用同一个十进制转换器（`__goclib_double_to_buf`），
数组打印也复用它。`%g` 的 precision 是**有效数字**（C 的规则），不是小数位数：
指数落在 `[-4, precision)` 用定点形式，越界用科学计数法，两种都去掉尾零。
指数至少两位、需要时才三位（`1e+05` / `1e+300`）。

十进制指数由 `log10` 估一个初值，再用真实值归一化回来——`log10` 只要差不超过
1 就够了。1e-308 以下 `10^k` 会下溢，所以幂被夹住、归一化循环把差额走回来，
`5e-324` 才能打成 `4.940656e-324` 而不是挂掉或算错 16 个数量级。

舍入是 half-to-even（IEEE 默认），判据是"小数点后是否还剩非零"，不是"下一位
是不是 5"——后者会把 `0.125001` 打成 `0.12`。精确 tie（2.5、0.125）按偶邻取整，
mingw 的 CRT 用 half-away-from-zero 给 3/0.13，两者都符合标准，goc 取前者。



## 4. 符号约定

- C 库函数 label 即函数名（`printf:`、`exit:`）。Linux 上库自带 `exit`（转调
  `__goclib_exit`），入口桩 `call exit` 走 need 闭包拉 C 版函数体，**不再** import
  同名 extern 桩——避免了 C 函数与 goa syscall 桩的符号冲突。
- Win 侧原语调 kernel32：每个 extern API 的归属 DLL 直接写在同名头文件的原型里
  （如 `winbase.h` 的 `extern BOOL CloseHandle(HANDLE), kernel32;`）。编译期由
  `dllOf` 收集，无需 win32.def 中心表。

## 5. goclib 也是一个通用 C 库

goclib 不依赖 goc 特有的语法或语义。用 gcc / clang 编同一份源码得到同一个库，实测
clang 22.1.8（Windows 目标）与 gcc 15.2.0（Linux 目标）各 19/19 通过、零实质警告，
打成 `libgoclib.a` 后与 musl 链接运行正常。

### 5.1 `__goc__` 是宿主判据

`__goc__` 是 goc 预定义的宏（值 1），gcc/clang 根本没有它——真实编译器各有自己的
`__GNUC__` / `__clang__`，都不定义 `__goc__`。所以头文件按它二分：

| | goc | gcc / clang |
|---|---|---|
| `stdarg.h` | 变参操作交给 codegen 内建（Linux 下连 `va_copy` 宏都不定义，名字直接落到 codegen） | `__builtin_va_list` + `__builtin_va_*` |
| `windows.h` 家族 | 需要（`extern BOOL CloseHandle(HANDLE), kernel32;` 是 goc 专有的 DLL 内联注解语法） | 不需要（Win32 调用全在 `#if defined(_WIN32)` 块内，被预处理器丢弃） |
| `stdatomic.h` | codegen 内建 `__goc_atomic_*` | `__atomic_*` 内建，降到同样的 LOCK XADD / CMPXCHG |
| `threads.c` / `mtx.c` 的自旋与 clone | 手写 `__asm` xchg / clone 汇编 | `atomic_exchange` / `syscall(56,…)` |
| `wchar_t` | Win64 是 2 字节无符号（对齐 UTF-16 `WCHAR`） | x86-64 SysV 是 4 字节有符号（对齐 glibc/musl） |

`__goc__` 必须真的登记在 `p.macros` 里（`common/preprocess.go`）。只作为宏展开期的
特判是不够的：头文件是用 `#ifdef __goc__` 问的，那条路查的是宏表。

### 5.2 用 gcc / clang 编 goclib

`-nostdinc` 是这套验证的核心：屏蔽系统头之后，goclib 自带的头就是唯一头来源，
证明它不依赖宿主 libc 的任何头。

```sh
# Linux（alpine 实测：gcc 15.2.0 + musl 1.2.6）
CC=gcc
CFLAGS="-std=c2x -nostdinc -Igoclib -O2 -Wall"
mkdir -p obj
for f in goclib/*.c; do $CC $CFLAGS -c "$f" -o "obj/$(basename "$f" .c).o"; done
ar rcs libgoclib.a obj/*.o
```

```sh
# Windows 目标（实测：clang 22.1.8）
CC="clang -target x86_64-linux-gnu"
CFLAGS="-nostdinc -Igoclib -O2 -Wall -Wextra"
# 其余同上
```

链接时**用静态库而不是散 `.o`**：goclib 定义了 `printf`、`fwrite`、`malloc`、
`strlen` 等约 390 个 musl 也有的名字，而 `ld` 只为满足仍未定义的符号才从 archive
里提取成员，于是 goclib 的版本优先、musl 的同名符号不会被拉入。散 `.o` 全部无条件
包含，每一个重复符号都是链接错误。

```sh
$CC -o hello hello.o libgoclib.a -lm
./hello
nm hello | grep -E ' [Tt] (printf|fwrite|malloc|strlen|fputs)$'   # 确认跑的是 goclib
```

### 5.3 `syscall.h`：同一份调用，两条到达路径

`syscall.h` 是 Linux-only 的（整体包在 `#if defined(__linux__)` 里——在 Windows TU 里
声明 `write(2)` 不是无用，而是会让 codegen 在 goclib 函数表和 DLL import 里都找不到
它，然后整份构建挂在 `unknown function "write": not in goclib`）。

goa 把 `linuxSyscalls` 表里的名字变成 `mov rax,N; syscall; ret` 三指令桩。表里大量
`__goclib_*` 别名（`__goclib_rename`、`__goclib_stat`、`__goclib_futex`），原因是
**goclib 自己就定义了同名 C 函数**：`extern rename` 会解析到自己的定义，无限递归。
外部编译器没有这张表，别名无人定义，链接失败——所以这个头把每个别名映射到宿主真正
有的路线：

| 调用 | goc | gcc / clang |
|---|---|---|
| `read` `write` `open` `close` `lseek` `mmap` `munmap` `nanosleep` `unlink` `getpid` `gettid` `sched_yield` `vfork` `execve` `wait4` | goa syscall 桩 | libc 同名函数 |
| `brk` | goa 桩 | `syscall(12, …)` |
| `clone` `futex` `exit_thread` `clock_gettime` | goa 桩 | `syscall(…)` |
| `stat` `fstat` `access` `rename` `mkdir` `rmdir` `getcwd` `chmod` | goa 桩 | `syscall(…)` |
| `getdents64` | goa 桩 | `syscall(217, …)` |
| `exit_group` | goa 桩 | `_exit()` |

三条不能走 libc 的理由，都是实测出来的，不是风格选择：

1. **`brk` 不是签名不同，是根本不工作。** musl 1.2.6 下 `brk(0)` 走编译器内建返回
   `(void*)-1`——bump 分配器把这读成"内核拒绝了"，第一次 malloc 就失败；而同一进程里
   `syscall(SYS_brk, 0)` 返回真实的 break（实测 `0x5b9eae9c2000`）。
2. **`stat` 那一族不能映射到同名 libc 函数。** `dir.c` 自己就定义了
   `int stat(const char *, struct stat *)`，名字解析到的正是它，于是
   `#define __goclib_stat … stat(…)` 把包装函数变成无限递归（gcc 报
   `-Winfinite-recursion`，8 处）。这正是 goa 的表带 `__goclib_` 前缀的原因。走系统调用
   反而更忠实：`stat.h` 的 `struct stat` 就是内核 x86-64 布局，`stat(2)` 直接填它。
3. **`getdents64` 在 libc 里根本不存在。** musl 和 glibc 都把它藏起来了，按名字声明能
   编过，链接时是 `undefined reference`。

### 5.4 为了在宿主编译器下正确，`memset`/`memcpy`/`memmove` 加了 `volatile`

`string.c` 里那三个函数是教科书式的字节循环，而在宿主编译器下这正是问题：`-O2` 下
gcc 认出这个模式，判断它等价于库函数，于是发出 **`call memset` 写在 `memset` 里面**。
实测 gcc 15.2 + musl 的反汇编：

```
memset:  test %rdx,%rdx / je .L0 / sub $0x8,%rsp / call memset / add $0x8,%rsp / ret
```

这是无界递归，musl 自己的 `calloc` 第一次分配就会踩到（栈上是
`opendir → calloc → memset → memset → …`）。goc 下从未暴露，因为 codegen 逐字发射
那个循环。

修法是给目标指针加 `volatile`：它保证存储按写的顺序发生（正是 C23 对
`memset_explicit` 的措辞），同时让内建匹配器认不出这个模式。已实测验证——同一份源码
不加 `volatile` 编出自调用，加上之后循环存活，**不需要额外的编译选项**。

### 5.5 验证脚本

`tools/portability/`（含用法说明；用例源码在 `tests/portability_cases/`）：

| 脚本 | 跑在哪 | 作用 |
|---|---|---|
| `build.sh` | alpine | gcc `-nostdinc` 编全部 19 个 `.c`，并归纳出实质警告 |
| `link_run.sh` | alpine | 打 `libgoclib.a`，链接 `hello.c`，`nm` 确认跑的是 goclib 而非 musl |
| `sc.c` + `sc_run.sh` | alpine | 专测 5.3 表格里走 `syscall(2)` 的 8 个包装函数 + `getdents64`，21 项断言 |
| `win_regress.sh` | Windows | goc / gocl 双后端的 Windows PE 回归 |
| `lin_build.sh` + `lin_run.sh` | Windows / alpine | gocl `-target linux` 的 ELF 回归 |
| `brk2.c` | alpine | 探针：实测 `brk(0)` 走编译器内建返回 -1，而 `syscall(12,0)` 返回真实 break |

`brk2.c` 是那份"brk 不工作"结论的原始证据。`sc.c` 存在是因为 5.3 那张表里的映射
错了不会有编译错误，只会有运行期的递归或 undefined reference——需要一份只走这条
路线、逐项断言的测试。

Linux 用例分两段是有原因的：`gocl.exe` 是 Windows 程序，给它 `/mnt/d/...` 路径会报
"the system cannot find the path specified"（那是 Linux 路径，它按 Windows 文件系统
解析），所以编译必须在 Windows 侧做，只有运行在 alpine 里。调用 `write(2)` 的那批用例
（`pb` `pe` `pf` `pg` `vt4`）属于 Linux 组而非 Windows 组——`write` 是 Linux 系统调用，
在 PE 目标上没有任何意义，把它们列进 Windows 组只会得到三个与 goclib 无关的
`unknown function "write": not in goclib`。

## 6. 已知缺口

- **goc 目前造不出 -0.0。** `-0.0` 字面量和 `-x`（x 是 0.0）都得到 +0.0：一元负号
  发的是 `0.0 - x`，而 IEEE 下 `0.0 - 0.0` 是 +0.0；常量池又按值索引，Go 的 map
  里 -0.0 与 0.0 相等。库侧已经绕开——`signbit`/`copysign` 直接读写符号位，
  printf 用 `signbit` 判定负号而不是 `x < 0`——所以 `copysign(0.0, -1.0)` 既能造出
  负零也能打成 `-0`。但源码里直接写 `-0.0` 依然拿不到负零，mathlib2.c 因此用
  copysign 构造它。真正的修法在编译器：一元负号改发符号位翻转，常量池按位模式
  索引。

## 7. 历史注记

- 阶段 4-13 期间后端是条件编译的手写汇编 `goclib.asm`（`; @func`/`; @deps`/`; @extern`
  块 + `selectPlatform` 选分支）。阶段 14 goc 的 C 子集补齐后，goclib.c 转正、goclib.asm
  整体退役（#117 五原语 C 化 + #118 移除 asm 机器）；曾需要的 `goclibAliasLinux`（exit →
  `__goclib_exit`）随 importSet 条件注入一并删除。
- 阶段 14 收尾（#131-#133）把单文件 goclib.c 按域拆成 os.c/stdio.c/stdlib.c/string.c/
  ctype.c，并新增 ctype.h 与 ctype.c（13 个字符分类/转换函数）、string.h 补全
  strstr/strrchr/strtok 等搜索类函数；goclib_test.c 端到端验证改为 include 伞头 + 各
  .c 文件。
- 曾修复 goa 汇编器 `stripComment` 的引号 bug：该函数用 `strings.Index(ln, ";")` 截行、
  不感知引号，字符串字面量含 `;`（如 strtok 分隔符 `",;."`）时 `.data` 段字节被错误截断
  （LC0 整条丢失、后续字面量串联错位），运行期 strchr/strtok 行为全错。已改为引号感知 +
  转义感知的状态机（`\"` 正确跳过），并新增 goa 单元测试 `lit_test.go` 锁定。
