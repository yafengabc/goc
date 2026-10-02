# goc C89-C23 缺口修复 Roadmap（fix-roadmap）

> 版本：2026-10-02 · 依据：`tests/cstd/CSTD_STATUS.md`（C89-C17）+ `tests/c23/C23_STATUS.md`  
> 每个缺口均有一对拍用例作证据与验收锚点；修复后对应用例须从 FAIL/PARTIAL 转 PASS，  
> 套件全量重跑 0 MISMATCH，`go test ./src ./src/goa` 全绿，456 pass/1 fail 基线不回退。

---

## 0. 分级原则

| 级      | 判定                                     | 处理时机  |
| ------ | -------------------------------------- | ----- |
| **P0** | 编译器崩溃（panic）或**静默错译**（不报错、结果错——写库时最危险） | 立即    |
| **P1** | 硬错误（报错拒绝）或写标准库的直接阻塞项（头/宏/函数缺失）         | 紧接 P0 |
| **P2** | C23 特性语义未达标（roadmap 承诺项）               | 中期    |
| **P3** | 库头/宏补全与兼容性收尾                           | 按需    |

---

## 1. P0 — 编译器崩溃与静默错译（立即修）

| #    | 缺口                                       | 现象（证据用例）                                          | 根因方向                           | 工作量    |
| ---- | ---------------------------------------- | ------------------------------------------------- | ------------------------------ | ------ |
| P0.1 | **`_BitInt` 裸字面量比较 → 编译器 panic**         | c23 `_BitInt(N)` 与裸 `200` 比较直接崩溃                  | codegen big 路径：字面量类型判定/比较处理缺分支 | S（进行中） |
| P0.2 | **块作用域非 static `_Thread_local` → panic** | c11_thread_local.c；codegen.go:4724 崩溃             | TLS 槽布局对块作用域非 static 未分配       | S      |
| P0.3 | **八进制字面量按十进制解析**                         | c89_lit_octal.c：`010`=10（应为 8）                    | lexer 数字字面量前缀未处理 0 前缀          | S      |
| P0.4 | **字符串内 UCN 吞反斜杠**                        | c99_ucn.c：`"\u00e9"` 输出字面 `u00e9`                 | lexer/字符串转义处理 UCN 反斜杠被吃        | S      |
| P0.5 | **`sizeof(复合字面量数组)` 恒为 0**               | c99_compound.c：`sizeof((int[]){1,2,3})`=0（gcc=12） | 复合字面量数组类型的 sizeof 未走数组宽度       | M      |
| P0.6 | **多 `#elif` 链真分支落 #else**                | c89_pp_elif.c：真条件分支未命中                            | cpp 条件编译链状态机                   | M      |
| P0.7 | **`%a` 打印退化**                            | c89_lit_float.c / c23：输出字面退化                      | print/goclib printf 缺 %a 格式    | M      |
| P0.8 | **字符串字面量直接下标返回垃圾值**                      | c23 B 组：`"hello"[0]` 垃圾值（先赋 `const char*` 才对）     | 字符串字面量作为数组左值时的地址/取址处理          | M      |

## 2. P1 — 硬错误与写库阻塞（紧接修）

| #    | 缺口                           | 现象（证据用例）                                                                     | 根因方向                    | 工作量          |
| ---- | ---------------------------- | ---------------------------------------------------------------------------- | ----------------------- | ------------ |
| P1.1 | **`\ooo`/`\xhh` 转义报错**       | c89_lit_esc.c：unterminated character literal                                 | lexer 八进制/十六进制转义扫描      | S            |
| P1.2 | **前导小数点 `.5` 被拒**            | c89_lit_dotfloat.c：unexpected token "."                                      | lexer 数字起始判定缺 `.` 分支    | S            |
| P1.3 | **stdint.h / inttypes.h 缺失** | c99_stdint.c：`expected ";", got "i8"`                                        | 缺头文件 + intN_t 类型支持      | L            |
| P1.4 | **`__func__` 未定义**           | c99_funcname.c                                                               | 预定义标识符未注入               | S            |
| P1.5 | **`va_copy` 不在 goclib**      | c99_vacopy.c                                                                 | goclib stdarg 缺 va_copy | S            |
| P1.6 | **\`\_BitInt(N)≤64 不回绕**     | c23 A2 组：`(S8)200`→200（应 -56）                                                | big 路径对小宽度位宽/回绕未处理      | M            |
| P1.7 | **预定义宏全线缺失**                 | 多用例：`__STDC_VERSION__/__STDC__/__STDC_HOSTED__/__DATE__/__TIME__` undeclared | cpp 预定义宏表为空             | S            |
| P1.8 | **`-std` 被接受但忽略**            | 单模式编译器；`__STDC_VERSION__` 恒无                                                 | 需文档化单模式或实现档位            | S（文档化）/L（实现） |

## 3. P2 — C23 特性语义（roadmap 承诺项，中期）

| #     | 缺口                                           | 现象（证据用例）                                                  | 状态                              |
| ----- | -------------------------------------------- | --------------------------------------------------------- | ------------------------------- |
| P2.1  | **`_Atomic` 连语法都不解析**                        | c23_atomic_probe.c：`parse error: expected ";", got "int"` | roadmap "语法接受"不成立，需 parser+type |
| P2.2  | **constexpr 进不了编译期位置**                       | c23_constexpr_use.c：数组尺寸/case/\_Static_assert/位域宽全拒       | 与 P1.7 预定义宏/常量求值联动              |
| P2.3  | **VLA 不可用且不定义 `__STDC_NO_VLA__`**            | c23_vla.c：conformance 缺口                                  | 选实现 VLA 或定义 NO_VLA 宏            |
| P2.4  | *ckd_ 64 位溢出标志恒 0**                          | c23_ckd.c：goc 9/12，64 位误报                                 | goclib 头需 128 位数学或改用位宽判定        |
| P2.5  | **enum E:T 布局忽略**                            | c23 枚举：sizeof 恒 4                                         | parser/类型宽度                     |
| P2.6  | **alignas 对成员/数组不生效**                        | c23_alignas_alignof.c                                     | 属性落位到布局                         |
| P2.7  | **双层嵌套匿名字段错位**                               | c23_anon：a=11 vs gcc a=10                                 | 匿名成员偏移计算                        |
| P2.8  | **首个 `{}` 标量不归零、`static int s={}` 被拒**       | c23 D1 组                                                  | 初始化语义                           |
| P2.9  | **`_Generic` LLP64 折叠 long/unsigned→int**    | c23 泛型选择                                                  | 类型判定宽表                          |
| P2.10 | **属性在 struct/enum/typedef/参数位置 parse error** | c23 C 组                                                   | parser 属性落位扩展                   |
| P2.11 | **`[[gnu::aligned(16)]]` 解析不生效**             | c23 C 组                                                   | 属性参数求值                          |
| P2.12 | **`[[fallthrough]]` switch 外静默接受**           | c23 C 组                                                   | 属性位置校验                          |
| P2.13 | **非法数字分隔符被静默剥离**                             | c23 D1 组                                                  | 分隔符位置校验                         |
| P2.14 | **前导小数点 `.12` 不支持**                          | c23 D1 组（同 P1.2）                                          | lexer                           |
| P2.15 | **`defined(__has_c_attribute)` 返回 0**        | c23 B 组：标准守卫失效                                            | cpp defined 特判                  |

## 4. P3 — 库头与宏补全（写库配套，按需）

| #    | 缺口                                    | 说明                                                                                 |
| ---- | ------------------------------------- | ---------------------------------------------------------------------------------- |
| P3.1 | **头缺失**                               | stdbit.h、uchar.h、stdatomic.h、threads.h、stdnoreturn.h、stdbool.h（goc 侧关键字已内建，头可空壳+宏） |
| P3.2 | **limits.h/float.h C23 宽度宏缺失**        | C23 宽度/归一化宏全套                                                                      |
| P3.3 | **offsetof / max_align_t 未提供**        | stddef.h 补 max_align_t；offsetof 宏实现                                                |
| P3.4 | **RAND_MAX / HUGE_VAL 缺失**            | stdlib.h / math.h                                                                  |
| P3.5 | **`__goc_long_double_is_double` 未定义** | long double 降级标记宏                                                                  |
| P3.6 | **K\&R 定义 / trigraph**                | C89 兼容收尾（trigraph 可对标 gcc 默认关闭，文档化即可）                                              |

---

## 5. 修复批次建议

```
批次 A（本次）  : P0.1 _BitInt panic + P1.6 _BitInt 回绕（同模块一起修）
批次 B         : P0.2 _Thread_local panic
批次 C（字面量）: P0.3 八进制 + P0.4 UCN + P1.1 转义 + P1.2/.12 前导点（lexer 集中区）
批次 D         : P0.5 sizeof 复合字面量 + P0.8 字符串下标（codegen 类型/地址）
批次 E（cpp）  : P0.6 #elif 链 + P1.7 预定义宏 + P2.15 has_c_attribute
批次 F（库）   : P1.3 stdint + P1.4 __func__ + P1.5 va_copy + P0.7 %a
批次 G（C23）  : P2.1→P2.14 特性语义（按依赖排序：语法→类型→布局→初始化→泛型）
批次 H（宏）   : P3.1→P3.5 头与宏补全
```

**依赖说明**：批次 F 的 stdint.h 依赖批次 E 的预定义宏（`__STDC_VERSION__` 决定 intN_t 可见性）；批次 G 的 constexpr 依赖常量求值基础设施；其余批次相互独立，可并行。

## 6. 验收方法论

- 每个修复对应套件用例 FAIL/PARTIAL → PASS，报错证据原文从状态矩阵移除；
- `tests/cstd/run_cstd_tests.ps1` 与 `tests/c23/run_c23_tests.ps1` 全量重跑 0 MISMATCH；
- `go test ./src ./src/goa` 全绿；gocregress 基线 456 pass/1 fail 不回退；
- 静默错译类修复须新增防回归单测（lexer/codegen 单元级），不止依赖对拍套件。
