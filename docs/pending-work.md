# 未尽工作总表

> 更新：2026-10-09（#48 goc fp128 lowering / 62dc3f8·2714d93 已落，VLA 由远端 ea09539 实现）。
> 本文档按「建议推进顺序」组织，每项标注来源（任务号 / 复现文件 / 相关 commit）。

## P0 — 隐患修复

### 1. ~~goc 原生 `f(x).member` structret bug~~ ✅ 已修复（e3658d7，2026-10-09）

- 根因：标量成员读取后结果缓冲声明未释放；复合赋值回滚 tmpDepth 后语句边界再释放一次 → 槽号变负 → 停车槽别名进活局部变量。
- 修在缓冲真正死亡处（genExprT1 的标量成员加载，releaseCallResultBuffer）；聚合成员/数组衰减路径不提前释放。
- 回归：`tests/portability_cases/structret_bug.c`（字节级 stdout 对比，布局敏感）+ `structret.c`（位置电池）；win_regress 22 项，gocregress 496/0。

## P1 — fp128/long double 可用性收尾

### 2. #49 printf/scanf 的 fp128 十进制转换

- `%Lf/%Le/%Lg`（含 #、g 删尾、精度、宽度等既有格式语义）与 scanf 对应项。
- 核心是 binary128 → 十进制字符串：最长 **4932 位有效数字**（次正规 `LDBL_TRUE_MIN` 场景），不能走 double 路径。
- 参考：goclib `fp128.c`（binary128 软浮点，95563b9）；gcc `__float128` 对照 harness `/tmp/fullcmp.c` 模式（#46 日志）。
- **验收**：随机值 + 边界（最大/最小/次正规/NaN/Inf）与 gcc `printf("%Qa/%.4932f")` 逐位对照；portability case 挂 win_regress。
- **进度**：
  - #49a 变参 fp128 ABI（08d1fee + cdbf48f 位置修复）✅；
  - #49b `src/goclib/fp128dec.c` fmt+parse（0f88e5a）✅ —— libquadmath 差分 408664 case 全绿（`src/goclib/tests/test_fp128dec.c`）；
  - #49c stdio.c 挂接 ✅：printf `%Lf/%Le/%Lg/%La(不接)/%n` 全套 flag/width/prec，>512 字节走 malloc 重试（tf128_fmt 返回负 need，NUL 由调用方补）；scanf `%Lf` 十进制走 tf128_parse、hex 回落 strtod+widen；'L' 修饰符折叠 bug 修复（isL 在折叠前记录）。附带修掉 5 个 goc 前端/codegen bug：负 LD 字面量、聚合 LD 初始化、sprintf 返回值被 resBig 残留毁掉、字面量 LD 变参实参段错误、gocl EnableLongDouble 时序（库构建早于赋值 → 无条件化）。`longdouble_io.c` 挂 win_regress（24/0）+ gocregress 496/0。

### 3. #50 float.h LDBL 常量 + math.h l 后缀函数

- `LDBL_MANT_DIG/LDBL_DIG/LDBL_MIN_EXP/MAX_EXP/MIN_EXP/MAX_10_EXP/MIN_10_EXP/LDBL_MAX/MIN/EPSILON/TRUE_MIN/DECIMAL_DIG`。
- `sqrtl/fablsl/fmodl/modfl/expl/logl/powl/sinl/cosl/tanl/atan2l/floorl/ceill/roundl/fmaxl/fminl/...` 挂 fp128 运行时。
- **验收**：常量与 gcc `__float128` 的 float.h 逐值一致；函数对 gcc 对照 hash。

## P2 — 任务清单核实与关闭（先核实再关，防过时账）

| 任务 | 内容 | 疑似状态 |
|---|---|---|
| #1–5 | gocl IR→ELF 对象链（triple/datalayout、CompileIR、IngestELF、syscall 衔接、WSL 真机） | LLVM 后端已能出 ELF64，**大概率整体过时** |
| #35 | math.h C23 新函数缺口 | 部分可能已完成 |
| #36 | gocl NaN 常量折叠缺陷 | #47 已修 NaN 约定，需核实折叠路径 |
| #37 | LLVM fmax/fmin 家族名字识别绕开 | `fmaximum_num` 私有拼写已规避并登记 module.go，**疑似已完** |
| #7   | gocl Linux 用例测试扩展 | 回归基建已全（gocregress 495 + win_regress 20），可能只剩补用例 |
| #18  | goc/gocl 双平台回归 | 同上 |
| #13  | goclib 通用库化（gcc/clang 可编） | #14–17 已完成（stdarg/stdio 守卫、clang 编译验证），可能只差收尾确认 |

## P3 — 独立功能线

### 4. #22/#24 socket.c 双平台实现 + TCP 环回验证

- socket.h/winsock2.h/syscall 表/errno 宿主映射已就绪（#19–21、#23 完成）。
- 剩：socket.c 实现 → 双平台 TCP 环回 → 回归。

### 5. #25 用 goc 写一个 ls（coreutils 小工具）

### 6. #26 构建脚本：goc 与 gocl 双后端都能编

### 7. #27 跨平台测试：Windows PE 与 Linux ELF 输出逐字节一致

## P4 — 文档线

- **#30** 博客 05 篇按坑扩写（源码位置 + 修复 commit + 验证）
- **#38** README 核实过时说法、取实测真值
- **#39** 重写 README 过时章节
- **#40** 功能覆盖测试程序

## 已完成的近期工作（背景）

- 2026-10-09：#48 goc 原生 fp128 lowering（62dc3f8）；goa e_machine 修复（2714d93）；远端 VLA（ea09539）rebase 同步。
- 详见 `.workbuddy/memory/2026-10-09.md` 与各 commit message。
