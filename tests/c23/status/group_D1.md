# Group D1 — 字面量（Literals）

对拍基线：`bin\goc.exe`（2026-10-02）vs `gcc.exe (Rev4) 16.2.0 MSYS2 UCRT64 -std=c2x -Wall -Wextra`。
所有 .c 均 LF / UTF-8 无 BOM / ASCII-only。下表"子用例数"指最终 PASS 文件里实际对拍的 case 数。

| 文件 | 特性 | 子用例数 | 判定 | goc 证据（含报错原文） | gcc 对拍结论 | 写标准库建议 |
|---|---|---|---|---|---|---|
| c23_binary_literal.c | 0b/0B 二进制字面量 | 11 | **PASS** | 无报错；11/11 与 gcc 逐行一致（含 0B 大写、0b0、0b0101、与 0xFF 对照、混算、0b1010u/UL、32 位全 1、0b1010'1010、`#if 0b1100==12`） | 全部一致，SUMMARY 11/11，退出 0 | **能放心用**（含 `#if` 中的 0b，goc 预处理器也认得） |
| c23_digit_sep.c | 数字分隔符 ' | 12 | **PASS** | 无报错；12/12 一致（1'000'000、123'456、0xFF'FF、0xDE'AD、0b1010'1010、1.2'34e5、.12、.1'2、1'000e3、3.14'15f、1'000UL、4'2L）。前导点子用例 `.12`/`.1'2` 已于 2026-10-02 加回（lexer 前导点 float 修复）；仅 `0x1p10'0`（hex 浮点指数内分隔符）仍被移除，归 P2.13 | 全部一致，SUMMARY 12/12 | **分隔符合法位置可放心用**（含前导点 `.12`）；仅 `0x1p10'0` 指数内分隔符待修（P2.13） |
| c23_digit_sep_bad.c | 非法数字分隔符（负向） | 6 个构造 | **FAIL（goc 过宽）** | goc **不拒绝**：整文件编译+运行成功，exit=0，`(program exited with code 66661)`，stderr 为空——goc 静默把 `'` 删掉后照常解析（66661 = 1000+65535+1+1+1+123 印证） | gcc 全部拒绝：`adjacent digit separators`(1''000)、`digit separator after base indicator`(0x'FFFF')、`digit separator adjacent to decimal point`(1.'5)、`digit separator adjacent to exponent`(0x1'p0)、`missing terminating ' character`(1'.2、123') | **别指望 goc 揪出分隔符笔误**：`1''000`/`0x'FFFF'` 等会被静默接受；写法要自己核对 |
| c23_hex_float.c | 十六进制浮点 0x1.8p3 | 10 | **PASS** | 无报错；10/10 一致（0x1.8p3=12、0x1p-2=.25、0x.8p1=1、0x1.4p2f=5、0x10p0、0x1p4、算术、L 后缀、== 比较、0x1e5/0xFACE 整数对照） | 全部一致，SUMMARY 10/10 | **带 p 指数的十六进制浮点可放心用**；`%a` 打印与无指数形式见摘要 |
| c23_u8_literal.c | u8 字符串/字符值语义 | 8 | **PASS** | 无报错；8/8 一致（sizeof(u8"abc")=4、sizeof(u8"")=1、字节内容 abc、u8"ab""cd" 拼接 len=5、u8"a""b" 混拼 len=3、u8'x'=120、u8'A'=65、尾部 NUL=0） | 全部一致，SUMMARY 8/8 | **u8 的值/大小/拼接/单字节字符可放心用**；类型身份归 A1 组；原始串与多字节字符见摘要 |
| c23_empty_init.c | 空 {} 初始化 | 9 | **PASS（带备注）** | 无报错；9/9 一致（int/指针/double/数组/struct/union/嵌套 struct/{}vs{0}/块内局部 全部=0）。**两个缺口已移除子用例并在文件注释说明**，见下 | 全部一致，SUMMARY 9/9 | **自动存储期的 {} 基本可用**；但有两个坑（见摘要），关键处建议用 `{0}` 或显式清零 |

## 组内发现摘要

### 与 roadmap 预期不符 / 需记录的 goc 行为

1. **数字分隔符——goc 是"删 `'` 即合法"的宽松实现，不诊断非法位置**。
   六个非法写法（`1''000`、`0x'FFFF'`、`1'.2`、`1.'5`、`0x1'p0`、`123'`）gcc 全部拒绝，goc 全部静默接受并正常运行（退出码 66661）。写标准库时**不要假设 goc 会对分隔符笔误报错**。
   另外 goc 有两处**该接受却拒绝**的合法写法，已从 c23_digit_sep.c 移除并注释：
   - `0x1p10'0`（十六进制浮点**指数部分**内的分隔符，gcc 接受）→ goc：`preprocess error: ... unterminated character literal`。**仍待修（归 P2.13）。**
   - `.1'2` / 前导小数点 `.12`：**已于 2026-10-02 修复**（lexer 前导点 float 分支），子用例已加回 c23_digit_sep.c，与 gcc 逐行一致（SUMMARY 12/12）。

2. **空 {} 初始化有一个 codegen bug + 一个存储期缺口**：
   - **bug**：在已有其它局部声明之后，**第一个**用 `{}` 初始化的标量不会真正被归零（实测读到稳定垃圾值 `71302960`，跨次运行不变）；第二个及以后才归零。示例 c23_emptyinit.c 之所以全 0，是因为 `int a={}` 恰好是 main 的第一个局部、栈面干净。本文件用一个不打印的"预热"局部 `int warm={}` 吸收该坏槽位后才 9/9 对拍一致。**写标准库时避免把 `{}` 放在依赖其必定为 0 的关键位置，优先 `{0}`。**
   - **缺口**：`static int s = {};`（static 存储期）被 goc 拒绝 → `codegen error: invalid braced initialiser for scalar type int`。gcc 接受。**`static T x = {}` 不可用，要用 `= {0}` 或显式赋 0。**

3. **十六进制浮点**：
   - goc 的 printf **不支持 `%a`**：探针 `printf("%a", 0x1.8p3)` 输出字面 `a`（gcc 输出 `0x1.8000000000000p+3`）。故本文件用 `%.3f`/`==` 对拍。**写标准库别用 `%a` 打印十六进制浮点。**
   - **无指数形式 `0x1.8`（C23 新特性）goc 接受（=1.5），但本工具链 gcc 16.2 `-std=c2x` 仍拒绝**：`hexadecimal floating constants require an exponent`。即 goc 在此点**领先于当前 gcc**——goc 里可用，但**移植到该 gcc 不通过**，属 goc 私有扩展。

4. **u8 前缀——本工具链的 gcc `-std=c2x` 有严格模式怪癖**，下列 C23 写法 gcc 也不接受，故未纳入对拍文件：
   - `u8R"(...)"` 原始字符串（甚至普通 `R"(...)"`）在 `-std=c2x` 下报 `'R' undeclared`；但 `-std=gnu2x` 可通过。即原始串在本对拍 gcc 下不可用（与 goc 无关）。
   - 多字节 `u8'<非ASCII>'`（探针用 `u8'\u20AC'`）gcc 报 `character not encodable in a single code unit`，拒绝。
   - 本文件只测 goc 与 gcc 一致的值/大小/拼接/单字节字符；u8 字面量的**类型身份**（char8_t vs unsigned char 的 _Generic 匹配）由 A1 组 c23_char8.c 负责，本组未重复。

### 交叉点说明
- 二进制字面量与数字分隔符同属 lexer 内建，本组已交叉覆盖（`0b1010'1010`、`0xFF'FF`）。
- 前导小数点 `.12` 不支持是 goc 浮点词法的独立缺口，虽与 D1 分隔符文件同遇，但影响所有浮点用例（其它组写浮点字面量时注意）。
