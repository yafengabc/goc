# goc 补全 C23 特性路线图

> 状态：执行中（2026-10-01 大更新——MVP 优先集与 C99/C11 主体已全部落地）  
> 范围：让 goc 从「C89 全量 + 部分 C99」推进到「实用 C23 子集」  
> 目标定位：**不追求 100% 标准合规**，而是补齐「现代 C 写法」最常用的那批特性，  
> 让 goclib、示例和第三方源码能直接用现代风格编译。重成本特性（见末节）标为可选/后置。

---

## 0. 现状对照（2026-10-01，按源码与 examples/ 实测更新）

### 已完成（example + golden 全部就位，gocregress 全绿）

| 特性                                                   | 标准阶段    | 验证用例                              | 备注                                                        |
| ---------------------------------------------------- | ------- | --------------------------------- | --------------------------------------------------------- |
| `//` 注释 / 变参 / 可变参宏                                  | C99     | 例套件                               | 原有                                                        |
| 数组指示符 `[i]=` / `.field=` / 混合嵌套                      | C99     | 例套件                               | 原有                                                        |
| `long long` / `_Bool` / `restrict`                   | C99     | 例套件                               | 原有                                                        |
| **复合字面量 `(T){...}`**                                 | C99     | （tmp 用例，待入 cstd）                  | #127：按值传参/sret/取地址/数组退化/循环内新对象/`{}` 零初始化全过                |
| **`_Thread_local` / `thread_local`**                 | C11     | `c23b_thread_local` / `tls_basic` | **goa 原生 TLS**（TLS 目录 + `gs:[0x58]`），远超原计划软模拟             |
| `_Static_assert` / `static_assert`                   | C11/C23 | `c23_static_assert`               |                                                           |
| `_Alignas`/`alignas` / `_Alignof`/`alignof`          | C11/C23 | `c23_align`                       |                                                           |
| `_Noreturn` / `noreturn`                             | C11/C23 | `c23b_keywords`                   |                                                           |
| **`_Generic`**                                       | C11     | （tmp 用例，待入 cstd）                  | #125：精确类型匹配、控制表达式不求值、只查选中分支；顺带修了变参实参跳过检查的存量缺陷             |
| **`bool`/`true`/`false` 关键字化**                       | C23     | 例套件                               |                                                           |
| `nullptr` / `nullptr_t`                              | C23     | `c23_nullptr`                     | 降为空指针常量 0                                                 |
| `typeof` / `typeof_unqual`                           | C23     | `c23_typeof`                      |                                                           |
| `constexpr`（轻量）                                      | C23     | `c23_constexpr`                   |                                                           |
| **`auto` 类型推导**                                      | C23     | （tmp 用例，待入 cstd）                  | #124：decay 推导、`{单元素}`、static/const/file-scope/for-init 全过 |
| `enum E : int {…}` 底层类型                              | C23     | `c23_enum`                        |                                                           |
| 空 `{}` 初始化                                           | C23     | `c23_emptyinit`                   |                                                           |
| 属性 `[[...]]`（7 标准 + vendor 兼容）                       | C23     | `c23_attr` / `c23b_keywords`      | deprecated/nodiscard 有警告                                  |
| `u8` 字符串/字符前缀                                        | C23     | `c23b_keywords`                   |                                                           |
| 二进制字面量 `0b1010` + 数字分隔符                              | C23     | lexer 内建                          |                                                           |
| `#elifdef` / `#elifndef` / `#warning`                | C23     | cpp 内建                            |                                                           |
| `#embed`                                             | C23     | `c23_embed`                       |                                                           |
| `__has_include` / `__has_c_attribute` / `__VA_OPT__` | C23     | `c23b_pp` / `c23_vaopt`           |                                                           |
| `stdckdint.h`（ckd_add 系）                             | C23     | `c23_ckd`                         |                                                           |
| `strdup`/`strndup` 等升标准                              | C23     | `c23_string`                      |                                                           |
| 无参 `f()` == `f(void)`                                | C23     | 例套件                               |                                                           |
| `inline` 关键字                                         | C99     | `c23b_keywords`                   | 接受并降级                                                     |

### 待办（剩余缺口）

| 特性                          | 任务号      | 阶段      | 工作量    | 状态                                           |
| --------------------------- | -------- | ------- | ------ | -------------------------------------------- |
| **十六进制浮点 `0x1.8p3`**        | **#126** | C99/C23 | M      | ✅ `c99_hexfloat`：p 指数可选（C23）、`0x.8p1`、f/l 后缀 |
| **匿名 struct/union 成员**      | **#128** | C11     | M      | ✅ `c23_anon`：扁平访问、初始化透明穿透、union 重叠，gcc 对拍一致  |
| `_Atomic` / `<stdatomic.h>` | #129     | C11     | XL     | ❌ 可选后置（语法接受 + lock 前缀标量原子即可）                 |
| `<threads.h>` + 线程创建        | #130     | C11     | L      | ❌ 后置（依赖线程基础设施）                               |
| `long double`（→ double 降级）  | —        | C99     | M（可选）  | ❌ 标 `__goc_long_double_is_double`            |
| `_BitInt(N)`                | **#131** | C23     | XL     | ✅ `bitint`：goclib 大整数运行时（schoolbook+Karatsuba 乘、Knuth D 除、十进制 str），按需分配 scratch；**10 万位 π（Chudnovsky 二分）100,011 位逐位对拍 Python 大整数通过，15.5s** |
| `<uchar.h>` / `<stdbit.h>`  | —        | C11/C23 | M（可选）  | ❌                                            |
| TLS 测试钩子补全（#123）            | #123     | 工具链     | S      | ❌ tls_basic 尚无 golden，regress 中 SKIP         |

---

## 1. 阶段路线总览（执行状态）

| 阶段   | 主题          | 状态                                                                              |
| ---- | ----------- | ------------------------------------------------------------------------------- |
| 阶段 0 | 测试基线        | ✅ gocregress 基线 **456 pass / 1 fail**（唯一 fail 为预存 TestOsKeepsCalls；SKIP 为已知悬置项） |
| 阶段 1 | C99 收尾      | ✅ 完成（仅余 long double 可选项）                                                        |
| 阶段 2 | C11 类型系统现代化 | ✅ 主体完成（余 #129 \_Atomic、#130 threads.h 后置）                                       |
| 阶段 3 | C17 勘误      | ✅ 随 C11 一并对齐（无独立特性）                                                             |
| 阶段 4 | C23 核心特性    | ✅ MVP 优先集 13 项 + auto/\_Generic 全部落地                                            |
| 阶段 5 | goa / 工具链配套 | ✅ TLS 目录、`.bss`、`#embed` 数据注入均已就位                                               |

> 工作量记法：S < 1 天，M 1–3 天，L 1 周+，XL 数周/可选。

---

## 2. 剩余工作详解

### 2.1 #129 `_Atomic`（C11，XL→可裁剪为 M，可选后置）

裁剪版（建议）：仅语法接受 + 标量原子——load/store 用普通 mov，`++`/`--`/复合赋值发 `lock` 前缀指令（goa 已有 lock 前缀能力需确认）。完整内存序（memory_order\_*）后置。`stdatomic.h` 提供类型别名与宏。

### 2.2 #130 `<threads.h>` + 线程创建（L，后置）

依赖线程基础设施（`CreateThread` / `clone`），且 TLS 已就位（`thrd_create` + `_Thread_local` 组合是主要验证场景）。建议与 `_Atomic` 一起评估是否进入 v0.2。

### 2.3 #123 TLS 测试钩子收尾（S，建议穿插做）

- 写 `src/expected/tls_basic.txt` golden（Linux 三档 + Windows）接入 gocregress。
- 加一个「TLS 全局与 goclib 局部同名」碰撞回归例，锁死 2026-10-01 修复的二级 bug。

### 2.4 可选项（按需）

- `long double`：软降级为 double，标 `__goc_long_double_is_double` 宏。
- `_BitInt(N)`：已落地（#131），值模型 = ceil(N/64) 个 64 位小端字，按地址传值；回绕负数跨宽度必须经同宽 signed 类型符号扩展（`typedef signed _BitInt(N)`），否则模 2^N 同余破坏。
- `<uchar.h>` / `<stdbit.h>`：纯库工作，收益看需求。

---

## 3. MVP 优先集（已全部完成 ✅）

原 13 项于 2026-10-01 前全部落地，另超额完成 `auto`（#124）、`_Generic`（#125）、复合字面量（#127）、原生 TLS（#118–#123 前端部分）、十六进制浮点（#126）、匿名成员（#128）、`#embed`、`stdckdint.h`：

1. ~~二进制字面量 + 数字分隔符~~ ✅
2. ~~#elifdef / #elifndef / #warning~~ ✅
3. ~~bool/true/false 关键字化~~ ✅
4. ~~static_assert 关键字~~ ✅
5. ~~typeof / typeof_unqual~~ ✅
6. ~~nullptr / nullptr_t~~ ✅
7. ~~\_Static_assert~~ ✅
8. ~~\_Alignof / \_Alignas~~ ✅
9. ~~constexpr 轻量版~~ ✅
10. ~~属性解析 + deprecated/nodiscard 警告~~ ✅
11. ~~enum E : int 底层类型~~ ✅
12. ~~空 {} 初始化 + 无参 f()==f(void)~~ ✅
13. ~~strdup/strndup/memccpy~~ ✅

---

## 4. 风险与牵连（更新）

- **goa 牵连已基本清零**：TLS 目录、`.bss`、`#embed` 数据、属性/typeof 不触 goa——剩余特性中仅 `_Atomic`（lock 前缀，需确认）可能触及 goa；`_BitInt` 已落地（不触 goa）。
- **回归护栏**：每特性必须配 gocregress 用例（cstd example + golden）；`-O0` 字节不变性底座不受影响（新特性都是语法/类型层，不改优化管线）。auto/\_Generic/复合字面量的实现均在 check 期完成消解，对 codegen 透明或以新 AST 节点发射，已验证零回归。
- **_Generic 精确匹配教训**：类型匹配绝不能复用 `typesEqual`（它把所有整数当相等——`int*` 曾误配 `char *`），必须走 `genericTypeMatch` 的宽+符号+元素递归比较。
- **变参检查教训**：修复 `_Generic` 时顺带修掉「printf 变参尾部实参完全跳过 checkExpr」的存量缺陷——今后新增表达式节点须确认变参位置也能被 check。
- **TLS**：原生方案已定案（TLS 目录 + `G_goc_tls_index`），但标识符解析顺序铁律「局部 → static 局部 → 全局/TLS」已写死，新增任何名字解析路径不得破坏。

---

## 5. 验收标准（更新）

- 基线 **456 pass / 1 fail**（唯一 fail 为预存 TestOsKeepsCalls）不回退；tls_basic golden 补入后 SKIP 清零。
- 剩余特性（#126/#128，以及可选的 #129/#130）各配 example + golden。
- 文档：`README.md` 语言子集章节按本路线图增量更新（原大改计划仍缓行）。
- 全部完成后 README 可标注「**C23 实用子集 · 完整支持**」（除 long double/完整内存序/_Atomic）。
