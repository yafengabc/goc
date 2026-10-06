# tools/portability —— goclib 通用化与双后端回归

`goclib` 不依赖 goc 专有的语法或语义：同一份源码能被 gcc / clang 编，也能被 goc /
gocl 编，产出的库行为一致。这批脚本是那件事的证据，以及 `gocl -target linux`
这条路的回归。

`../..` 是仓库根，所以脚本从任意 checkout 运行都行，不需要改路径。

## 用 goc / gocl 编（Windows + Linux）

```sh
# Windows PE：goc 与 gocl 双后端，同一批期望值
sh tools/portability/win_regress.sh

# Linux ELF：编译在 Windows 侧（gocl.exe 是 Windows 程序），运行在 alpine 里
sh tools/portability/lin_build.sh
wsl -d alpine -- sh /mnt/d/Projects/goc/tools/portability/lin_run.sh
```

仓库不在 `/mnt/d` 时，`lin_run.sh` 的 `DST` 可以覆盖：

```sh
wsl -d alpine -- sh -c 'DST=/mnt/c/code/goc/tmp/portability-out sh /mnt/c/code/goc/tools/portability/lin_run.sh'
```

**为什么 Linux 分两段。** `gocl.exe` 是 Windows 程序，给它 `/mnt/d/...` 路径会报
"the system cannot find the path specified"——那是 Linux 路径，它按 Windows 文件系统
解析。所以编译必须在 Windows 侧做，只有运行在 alpine 里。

**为什么 `write(2)` 那批用例属于 Linux 组。** `pb` `pe` `pf` `pg` `vt4` 的源码直接
调用 `write(1, …)`，那是 Linux 系统调用，在 PE 目标上没有任何意义。把它们列进
Windows 组只会得到三个与 goclib 无关的 `unknown function "write": not in goclib`——
被测程序在要一个该目标上不存在的系统调用。

## 用 gcc / clang 编（宿主机交叉验证）

在 alpine 里：

```sh
wsl -d alpine -- sh -c '
  mkdir -p /tmp/gl && cd /tmp/gl &&
  cp -r /mnt/d/Projects/goc/src/goclib . &&
  cp /mnt/d/Projects/goc/tools/portability/*.sh /mnt/d/Projects/goc/tools/portability/*.c . &&
  sh build.sh && sh link_run.sh && sh sc_run.sh'
```

`build.sh` 用 `-nostdinc`，屏蔽系统头之后 goclib 自带的头就是唯一头来源——这是
"goclib 不依赖宿主 libc 的任何头"这句话的证明方式。

`link_run.sh` 打成 `libgoclib.a` 再链接，而不是散 `.o`：goclib 定义了 `printf`、
`fwrite`、`malloc`、`strlen` 等约 390 个 musl 也有的名字，而 `ld` 只为满足仍未定义
的符号才从 archive 提取成员，于是 goclib 的版本优先、musl 的同名符号不会被拉入。
散 `.o` 全部无条件包含，每一个重复符号都是链接错误。脚本末尾的 `nm` 确认
`printf`/`fwrite`/`fputs`/`malloc`/`strlen` 全部来自 `libgoclib.a`。

## 脚本

| 脚本 | 跑在哪 | 作用 |
|---|---|---|
| `win_regress.sh` | Windows | goc 与 gocl 双后端的 PE 回归，当前 12/12 |
| `lin_build.sh` | Windows | `gocl -target linux` 编译 16 个 ELF 用例 |
| `lin_run.sh` | alpine | 运行上面编译出的 ELF，比对期望输出，当前 16/16 |
| `build.sh` | alpine | `gcc -nostdinc` 编全部 20 个 `.c`，归纳实质警告 |
| `link_run.sh` | alpine | 打 `libgoclib.a`，链接 `hello.c`，`nm` 确认跑的是 goclib |
| `sc.c` + `sc_run.sh` | alpine | 专测走 `syscall(2)` 的 8 个包装函数 + `getdents64`，21 项断言 |
| `tcp.c` / `tcp2.c` | 两个平台 | 环回 TCP 与 UDP 往返、非阻塞、超时、错误码翻译；自己报告结果（退出码 0 且末行 `OK`），因为输出里有内核分配的端口号 |
| `hello.c` | alpine | 只用 goclib 公共 API 的程序，被 `link_run.sh` 链接 |
| `brk2.c` | alpine | 探针：`brk(0)` 走编译器内建返回 -1，而 `syscall(12,0)` 返回真实 break |

## `sc.c` 为什么存在

`syscall.h` 把 `stat`、`fstat`、`access`、`rename`、`mkdir`、`rmdir`、`getcwd`、
`chmod` 这 8 个别名映射到 `syscall(2)` 而不是 libc 的同名函数。这是个不能想当然的
决定：goclib 自己的 `dir.c` 就定义了 `int stat(const char *, struct stat *)`，
名字解析到的正是它，于是映射到 libc 名字会把包装函数变成无限递归（gcc 报
`-Winfinite-recursion`）。`getdents64` 更直接——musl 和 glibc 都不导出它。

映射错了不会有编译错误，只会有运行期的递归或 undefined reference，所以需要一份
只走这条路线、逐项断言的测试。`readdir drops . and ..` 一项也是：Linux 上 `.` 和
`..` 是真实记录，`dir.c` 明确把它们丢掉，否则同一份代码在两个平台会列出差两项的
结果。

## 用例源码

`../../tests/portability_cases/`。分成三组：

- **两平台都能表达的**（`win1` `p2` `p5` `p7` `p1` `p6` `p8` `p9`）—— 两个后端
  都跑，期望值相同。这正是要验证的东西：goclib 在两边必须是一个意思。
- **调 `write(2)` 的**（`pb` `pe` `pf` `pg` `vt4`）—— Linux 专属。
- **`mini_linux.c` / `stdio_linux.c`** —— 前者是裸 `write(2)`，用来把 ELF 后端与
  syscall 桩隔离出来；后者走完整 stdio 路径（缓冲 stdout 需在退出时 flush、
  格式化、`.data` 全局）。
- **socket 组**（`tcp` `tcp2`）—— 两个平台都跑。它们是这里唯一触到网络栈的用例，
  因此也是唯一能抓到"选项编号或 `fd_set` 布局对目标而言是错的"这类问题的用例。
  `tcp.c` 走 bind/listen/connect/accept/send/recv 与 `select`；`tcp2.c` 走 UDP 的
  `sendto`/`recvfrom`、非阻塞、超时，以及错误码翻译——最后一部分是重点：程序写的
  是 `if (errno == ECONNREFUSED)`，而 Linux 内核答 111、Winsock 答 10061，这行代码
  本身看不出翻译有没有生效。
