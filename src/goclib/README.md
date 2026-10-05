# goclib —— goc 自带的 C 库

goc 不链接任何系统 libc（无 msvcrt / glibc，无 gcc）。它需要的 `printf`、`malloc`、
`strlen`、`isspace` 等都由这里提供——**全部是纯 C 实现**，按域拆成多个 .c 文件，由
goc 在启动时按目标平台自行编译，函数体经常规代码生成器（genFunc）按需发射：程序真正
调用到什么才链接什么，hello world 不为 malloc 买单。

```
goclib/
├── goclib.h       伞头：拉入全部标准头 + 六个 __goclib_* 平台原语原型
├── os.c           平台原语实现（Win kernel32 extern / Linux syscall）
├── stdio.c        printf 家族（%d %s %f %e %g …）+ 数组/标量薄打印
├── file.c         FILE 层：fopen/fread/fwrite/fseek/ftell/remove/rename/…
├── stdlib.c       malloc/free/calloc/realloc/atoi/strtol/qsort/bsearch/exit
├── string.c       strlen/strcpy/strcmp/strstr/strtok/memset/memcpy/memmove/…
├── ctype.c        isalnum/isalpha/isdigit/isspace/… /tolower/toupper
├── math.c         44 个数学函数（含 erf/tgamma/lgamma/expm1/log1p/fma）
├── time.c         time/clock/gmtime/localtime/mktime/strftime/asctime/ctime
├── errno.c        errno 位置 + strerror
├── assert.c       assert 失败处理
├── ctype.h / stddef.h / stdarg.h / stdio.h / stdlib.h / string.h /
│   math.h / time.h / errno.h / assert.h / limits.h / float.h   内置标准头
├── windows.h / windef.h / winbase.h / wingdi.h / winuser.h / dirent.h / stat.h
│   内置 Win32/目录/文件属性头；每个 extern API 在自己的原型里写明归属 DLL
│   （如 `extern BOOL CloseHandle(HANDLE), kernel32;`），无需独立归属表
└── README.md      本文件
```

## 1. 工作机制（codegen.go 的 buildClibC / genClibFuncs）

- `init()` 里对两个目标各跑一次 `buildClibC(linux bool)`：伞头 goclib.h 作为独立 TU
  先行编译（头内如有实现也会被收集），随后 `goclib/*.c` 按名序编译（ctype.c → os.c →
  stdio.c → stdlib.c → string.c），同名定义**后者胜出**（.c 定义覆盖头定义），重复不会
  到达链接器。
- 平台选择用 `PreprocessTarget` 注入的宏：`_WIN32/_WIN64` 或 `__linux__/__linux`，
  os.c 顶部 `#if defined(_WIN32) / #elif defined(__linux__)` 只留对应分支。
- 所有 `goclib/*.h`（含 ctype.h 等标准头）经 `go:embed goclib/*` 嵌入二进制，新头文件
  自动生效、无需改 embed 列表。标准头被 cpp 层按需注入到用户 TU。
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
| threads.h | 18 | 线程 `thrd_create` `thrd_equal` `thrd_current` `thrd_detach` `thrd_join` `thrd_exit` `thrd_sleep` `thrd_yield`；条件变量 `cnd_init` `cnd_destroy` `cnd_signal` `cnd_broadcast` `cnd_wait`；线程本地存储 `tss_create` `tss_get` `tss_set` `tss_delete`；`call_once`。**`mtx_*`（6 个）尚未实现** —— `cnd_wait` 内部会调用 `mtx_unlock`/`mtx_lock`，所以用到 `cnd_wait` 的程序要等互斥锁落地才能链接 |

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

## 5. 已知缺口

- **goc 目前造不出 -0.0。** `-0.0` 字面量和 `-x`（x 是 0.0）都得到 +0.0：一元负号
  发的是 `0.0 - x`，而 IEEE 下 `0.0 - 0.0` 是 +0.0；常量池又按值索引，Go 的 map
  里 -0.0 与 0.0 相等。库侧已经绕开——`signbit`/`copysign` 直接读写符号位，
  printf 用 `signbit` 判定负号而不是 `x < 0`——所以 `copysign(0.0, -1.0)` 既能造出
  负零也能打成 `-0`。但源码里直接写 `-0.0` 依然拿不到负零，mathlib2.c 因此用
  copysign 构造它。真正的修法在编译器：一元负号改发符号位翻转，常量池按位模式
  索引。

## 6. 历史注记

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
