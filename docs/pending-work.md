# 未尽工作总表

> 更新：2026-10-09（#48 goc fp128 lowering / 62dc3f8·2714d93 已落，VLA 由远端 ea09539 实现）。
> 本文档按「建议推进顺序」组织，每项标注来源（任务号 / 复现文件 / 相关 commit）。

## P0 — 隐患修复

### 1. goc 原生 `f(x).member` structret bug

- **现象**：结构体返回调用出现在表达式位置（如 `MIX(goc_tf_neg(a).hi)`）会把该调用的**结果地址**当值返回，且污染函数内**后续所有** struct 返回调用（读到 0x140014000 之类 PE 镜像地址，hi==lo）。
- **复现**：`tests/fp128/goc_structret_bug.c`（gcc/gocl 正确，goc 原生错；先赋给变量则正确——`tests/portability_cases/fp128.c` 因此绕开）。
- **影响**：任意用户代码，不只 fp128。
- **验收**：bug 用例在 goc 原生下输出 `r = 3ece32d23193c687.1000000000000000`；变异：撤销修复必须 FAIL。

## P1 — fp128/long double 可用性收尾

### 2. #49 printf/scanf 的 fp128 十进制转换

- `%Lf/%Le/%Lg`（含 #、g 删尾、精度、宽度等既有格式语义）与 scanf 对应项。
- 核心是 binary128 → 十进制字符串：最长 **4932 位有效数字**（次正规 `LDBL_TRUE_MIN` 场景），不能走 double 路径。
- 参考：goclib `fp128.c`（binary128 软浮点，95563b9）；gcc `__float128` 对照 harness `/tmp/fullcmp.c` 模式（#46 日志）。
- **验收**：随机值 + 边界（最大/最小/次正规/NaN/Inf）与 gcc `printf("%Qa/%.4932f")` 逐位对照；portability case 挂 win_regress。

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
