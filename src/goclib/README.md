# goclib —— goc 自带的 C 库

goc 不链接任何系统 libc（无 msvcrt / glibc，无 gcc）。它需要的 `printf`、`malloc`、
`strlen`、`isspace` 等都由这里提供——**全部是纯 C 实现**，按域拆成多个 .c 文件，由
goc 在启动时按目标平台自行编译，函数体经常规代码生成器（genFunc）按需发射：程序真正
调用到什么才链接什么，hello world 不为 malloc 买单。

```
goclib/
├── goclib.h       伞头：拉入全部标准头 + 五个 __goclib_* 平台原语原型
├── os.c           平台原语实现（Win kernel32 extern / Linux syscall）
├── stdio.c        printf/sprintf/puts/putchar/getchar
├── stdlib.c       malloc/free/calloc/atoi/abs/strtol/rand/srand/exit
├── string.c       strlen/strcpy/strncpy/strcmp/strncmp/strcat/strncat/strchr/
│                  strrchr/strstr/strspn/strcspn/strpbrk/strtok/memset/memcpy/
│                  memmove/memcmp
├── ctype.c        isalnum/isalpha/iscntrl/isdigit/isgraph/islower/isprint/
│                  ispunct/isspace/isupper/isxdigit/tolower/toupper
├── ctype.h / stddef.h / stdarg.h / stdio.h / stdlib.h / string.h   内置标准头
├── win32.def      Windows extern → DLL 归属表（PE import 的唯一事实源）
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

## 2. 五个 `__goclib_*` 平台原语（碰 OS 的唯一层面，在 os.c）

| 原语 | Windows（kernel32 extern 直调） | Linux（goa syscall 桩） |
|---|---|---|
| `__goclib_write(buf,len)` | `GetStdHandle`+`WriteFile` | `write`(fd=1) |
| `__goclib_exit(code)` | `ExitProcess` | `exit_group`(231) |
| `__goclib_heap_alloc(size)` | `GetProcessHeap`+`HeapAlloc` | `brk` bump 分配器（16B 对齐） |
| `__goclib_heap_free(p)` | `HeapFree` | 空操作（进程退出一起还） |
| `__goclib_read(buf,len)` | `GetStdHandle`+`ReadFile` | `read`(fd=0) |

## 3. 函数清单（45 个，全部按需链接）

| 头文件 | 函数 |
|---|---|
| stdio.h (5) | `printf` `sprintf` `puts` `putchar` `getchar` |
| stdlib.h (9) | `malloc` `free` `calloc` `atoi` `abs` `strtol` `rand` `srand` `exit` |
| string.h (18) | `strlen` `strcpy` `strncpy` `strcmp` `strncmp` `strcat` `strncat` `strchr` `strrchr` `strstr` `strspn` `strcspn` `strpbrk` `strtok` `memset` `memcpy` `memmove` `memcmp` |
| ctype.h (13) | `isalnum` `isalpha` `iscntrl` `isdigit` `isgraph` `islower` `isprint` `ispunct` `isspace` `isupper` `isxdigit` `tolower` `toupper` |

## 4. 符号约定

- C 库函数 label 即函数名（`printf:`、`exit:`）。Linux 上库自带 `exit`（转调
  `__goclib_exit`），入口桩 `call exit` 走 need 闭包拉 C 版函数体，**不再** import
  同名 extern 桩——避免了 C 函数与 goa syscall 桩的符号冲突。
- Win 侧原语调 kernel32：函数名必须同时出现在 win32.def（归属）与 windows.h 家族
  头文件（原型）。改 win32.def 后必须重建 goc（embed 进二进制）。

## 5. 历史注记

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
