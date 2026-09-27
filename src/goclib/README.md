# goclib —— goc 自带的 C 库

goc 不链接任何系统 libc（无 msvcrt / glibc，无 gcc）。它需要的 `printf`、`malloc`、
`strlen` 等都由这里提供。目录里有两个互补的部分：

```
goclib/
├── goclib.asm    汇编后端（当前真正在跑的），一份源、双平台条件编译
├── goclib.h     跨平台 C 声明（goc 暂不能编译，stage5 启用）
├── goclib.c     跨平台 C 实现（同上）
└── README.md   本文件
```

## 1. `goclib.asm` —— 当前后端（汇编）

整库写在一个文件里，用条件编译把 Windows 和 Linux 两套实现合并：

```asm
#if defined(_WIN64)
  ; ---- Windows 分支：kernel32 WriteFile / GetProcessHeap / ExitProcess ----
  ; printf sprintf puts putchar getchar strlen strcpy strcmp strcat strchr
  ; memset memcpy memmove memcmp strncmp malloc free calloc atoi abs strtol
  ; rand srand exit  + 内部 __goclib_write __goclib_vfmt __goclib_exit
  ;                 __goclib_heap_alloc __goclib_heap_free __goclib_read
#else
  ; ---- Linux 分支：write / brk / exit / read syscall，SysV 调用约定 ----
  ; 同名同语义
#endif
```

goc 在加载 goclib 时调用 `selectPlatform()`（`codegen.go`），按目标平台挑出对应分支、
丢掉另一分支，**两份原文件的内容会被原样保留**（所以双平台行为和拆成两个目录时
逐字节一致）。`go:embed goclib/*.asm` 把这一份文件嵌进 goc 二进制。

加函数 / 加平台相关代码：往对应分支里丢一个 `; @func` 块；双平台都有的算法也要
在两条分支里各写一份（这正是普通编译器用 `#ifdef` 包内联汇编的做法）。

### 真正无法跨平台、只能留在汇编里的东西

只有「碰 OS」的五个 `__goclib_*` 原语必须分平台写：

| 原语 | Windows | Linux |
|---|---|---|
| `__goclib_write(buf,len)` | `GetStdHandle`+`WriteFile` | `write`(fd=1) syscall |
| `__goclib_exit(code)` | `ExitProcess` | `exit` syscall |
| `__goclib_heap_alloc(size)` | `GetProcessHeap`+`HeapAlloc` | `brk` bump 分配 |
| `__goclib_heap_free(p)` | `HeapFree` | 空操作（进程退出一起还） |
| `__goclib_read(buf,len)` | `ReadFile` | `read`(fd=0) syscall |

**除此之外的一切算法**（格式化、字符串/内存操作、strtol、rand、atoi…）本来也是
汇编，但理论上都能用 C 写——只是 goc 现在的 C 子集还编不了（见第 2 节）。

## 2. `goclib.h` / `goclib.c` —— 跨平台 C 版（休眠中）

这是同一批函数的**纯 C 实现**，只写一遍、两个平台共用，算法全部用标准 C 表达，
平台差异只在 `#include` 进来的五个 `__goclib_*` 原语上。

它**现在不参与编译**——goc 的 C 子集（stage 4 及之前）还缺：

- `char` / 真正的指针类型（只能把指针塞进 `int`）；
- 字节级元素访问（`Index` 硬编码 8 字节步长 + quad load）；
- 全局 / `static` 变量（顶层非函数声明被丢弃）；
- `for` / `break` / `continue`；
- 变参函数（`printf` 的 `...`）。

所以字符串/内存类函数、带状态的 `rand`/`srand`、以及变参 `printf` 暂时只能靠汇编。

## 3. 迁移到 C 版（stage 5）

等 goc 补齐上面那些特性后，把后端从 `goclib.asm` 换成 `goclib.c` 的步骤：

1. goc 编译 `goclib/goclib.c`，把它和用户程序编进同一个翻译单元（让 `printf` 等成为
   本地函数）。
2. 把五个 `__goclib_*` 原语从 `goclib.asm` 里抽出来，在双平台分支上各自保留并暴露
   （`goclib.asm` 其余的公开函数全部删掉）。
3. 删除 `goclib.asm` 里除 `__goclib_*` 以外的所有函数——它们现在由 `goclib.c` 提供。
4. `goclib.c` 里对 `__goclib_*` 的调用会像现在一样通过 `c.need` 解析并拉进对应原语。

迁移后汇编层只剩五个平台原语，`goclib.asm` 退化为「OS 胶水」，跨平台算法统一由
C 维护。
