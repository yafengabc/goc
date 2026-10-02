# Group C - C23 attributes [[...]]

Test lead: attribute group (C23 clause 6.7.13). Toolchain: `bin\goc.exe` (2026-10-02)
vs `D:\msys\ucrt64\bin\gcc.exe -std=c2x -Wall -Wextra` (16.2.0). Every file below was
run through `_tools\check_case.ps1`; stdout/exit matched where stated. goc warnings
and errors are quoted verbatim from its stderr.

Note on method: goc emits `__has_c_attribute(...)=no` for every attribute (confirmed
by the organizer's `_build\probe_cattr.asm`), so no `__has_c_attribute` guard is used;
attribute syntax itself is what is tested. Warnings do not fail a build in either
compiler (except the hard errors noted).

## Per-file matrix

| File | Feature | Subcases | Verdict | goc evidence (stderr quoted verbatim) | gcc cross-check | Stdlib advice |
|------|---------|---------|---------|----------------------------------------|-----------------|---------------|
| c23_attr_noreturn.c | [[noreturn]] / _Noreturn decl+def, fn ptr, mixed spelling | 4 | PASS | exit 0, stderr empty. Accepts `[[noreturn]]`, `_Noreturn`, mixed `[[noreturn]]` decl + `_Noreturn` def, and `&die` taking address. Noreturn fns are defined (infinite loop) but never called. | exit 0, clean. Identical stdout, SUMMARY 4/4. | **Safe to use.** |
| c23_attr_nodiscard.c | [[nodiscard]] function + message form, (void) suppression | 5 | PASS | exit 0. Warnings: `line 15: warning: return value of function should not be discarded`, `line 17: warning: return value of function should not be discarded`. (void)-cast site not warned. | exit 0. Warnings: `warning: ignoring return value of 'need_val', declared with attribute 'nodiscard' [-Wunused-result]` and `... declared with attribute 'nodiscard': "must check result" [-Wunused-result]`. gcc warns at the *use* site; goc warns once at the *declaration*. | **Safe on functions.** goc's diagnostic is declaration-site (once per fn) and generic; it does not carry the reason string. |
| c23_attr_nodiscard_type.c | [[nodiscard]] on struct / enum TYPE | 3 | FAIL | exit 1: `parse error: line 13: anonymous struct/union requires a body` (on `struct [[nodiscard]] Node { int v; };`). Also (direct probe) `typedef struct {int v;} [[nodiscard]] NodeB;` -> `parse error: line 6: expected declarator name, token "["`. | exit 0, compiles+runs, SUMMARY 3/3. Warnings `ignoring return value of type 'struct Node', declared with attribute 'nodiscard'` and `... type 'enum E_OK' ...`. | **Avoid.** goc cannot parse attribute-on-type. |
| c23_attr_maybe_unused.c | [[maybe_unused]] on local var + static fn; unused baseline | 5 | PASS | exit 0, **stderr empty**. goc emits NO unused diagnostics at all (neither for the attributed nor the bare cases). | exit 0. Warnings: `unused variable 'bare' [-Wunused-variable]` and `'bare_func' defined but not used [-Wunused-function]`. | **Safe.** goc accepts the syntax; it is effectively a no-op annotation. |
| c23_attr_deprecated.c | [[deprecated]] / [[deprecated("msg")]], decl/def split, var | 4 | PASS | exit 0. Warnings (once per entity, at the declaration line): `line 13: warning: declaration is deprecated`, `line 14: warning: declaration is deprecated`, `line 19: warning: declaration is deprecated`. No extra warning for `&old_api` (case4). Message text "use new_api instead" NOT carried. | exit 0. Warnings per use site: `'old_api' is deprecated [-Wdeprecated-declarations]`, `'old_api2' is deprecated: use new_api instead [-Wdeprecated-declarations]`, `'leg_var' is deprecated`, repeat for `&old_api`. | **Safe syntax.** goc warns generically and drops the custom message; decl/def split works. |
| c23_attr_fallthrough.c | [[fallthrough]]; inside switch (no-attr / with-attr / last case) | 3 | PASS | exit 0, stderr empty. Accepts `[[fallthrough]];` as a null statement; emits no implicit-fallthrough diagnostic. | exit 0. Warnings: `this statement may fall through [-Wimplicit-fallthrough=]` (no-attr case) and `attribute 'fallthrough' not preceding a case label or default label` (last case). | **Use inside switches.** goc does not check fallthrough correctness. |
| c23_attr_fallthrough_reject.c | [[fallthrough]] OUTSIDE a switch (negative) | 1 | FAIL | exit 0, stderr empty. goc ACCEPTS the misplaced attribute (lenient no-op). | exit 1 (compile): `error: invalid use of attribute 'fallthrough'`. | **Caveat.** gcc rejects it; goc silently accepts it. |
| c23_attr_unsequenced.c | [[unsequenced]] / [[reproducible]], stacked with nodiscard | 3 | PASS | exit 0. Accepts prefix form on definitions and stacked `[[unsequenced]] [[nodiscard]] int mixed(...)`. Also emits the per-declaration nodiscard warning (`line 18: ... should not be discarded`). | exit 0. Warnings: `standard 'unsequenced' attribute can only be applied to function declarators ... note: did you mean to specify it after ')' following function parameters?` (x3). gcc prefers post-`)` placement; this is a compile-time hint only, no run-time effect. | **Safe syntactically.** Prefer post-`)` form for gcc cleanliness. |
| c23_attr_unknown.c | [[foo]] unknown; [[gnu::unused]]; [[gnu::aligned(16)]] | 3 | DIFF | exit 0, stderr empty. Silently accepts [[foo]] and [[gnu::unused]]; parses [[gnu::aligned(16)]] but `_Alignof(avar)=4` (natural int alignment, NOT enforced). | exit 0. Warning `'foo' attribute ignored [-Wattributes]`. `_Alignof(avar)=16` (enforced). Only the alignof line differs. | **Avoid relying on [[gnu::aligned]] for real alignment in goc.** Unknown/vendor attrs parse but are inert. |
| c23_attr_positions.c | positions: decl-start / definition / variable / stacked / attr-before-storage | 5 | PASS | exit 0. Accepts all five. Warnings: `line 16: ... should not be discarded`, `line 20: ... should not be discarded`, `line 26: warning: declaration is deprecated`, `line 26: ... should not be discarded` (both attrs on the stacked f_multi). | exit 0. Warning `'f_multi' is deprecated` at the use site. gcc also accepts attribute-after-definition and attr-before-storage. | **Safe in these positions.** |

## Group findings summary

- **goc's diagnostic model is declaration-site, not use-site.** Unlike gcc (which warns
  at every discarded/deprecated *use* and repeats it for `&fn`), goc emits exactly one
  `declaration is deprecated` / `return value ... should not be discarded` warning per
  attributed entity, at its declaration line, and never repeats it. goc also drops the
  custom message text of `[[deprecated("msg")]]` (it only says "declaration is deprecated").
- **goc implements no `-Wunused-*` / `-Wimplicit-fallthrough` diagnostics.** Bare unused
  variables, unused static functions, and implicit fallthroughs are all silent in goc;
  `[[maybe_unused]]`/`[[fallthrough]]` are accepted but have nothing to suppress.
- **goc cannot parse attributes in type positions (real gap).** `struct [[nodiscard]] T`,
  `enum [[nodiscard]] E`, and trailing `typedef int T [[...]]` all fail:
  `anonymous struct/union requires a body` / `expected declarator name, token "["`.
  Likewise attribute-on-parameter (`int f(int a, [[maybe_unused]] int b)`) fails with
  `expected type specifier, token "["`. Use function/variable/declaration positions only.
- **goc parses vendor-scoped attributes but does not enforce them.** `[[gnu::aligned(16)]]`
  is accepted, yet `_Alignof` still reports the natural alignment; `[[gnu::unused]]` and
  unknown `[[foo]]` are silently ignored (gcc at least warns "'foo' attribute ignored").
- **Lenient acceptance gap:** goc accepts `[[fallthrough]]` outside any switch (exit 0,
  silent) where gcc hard-errors `invalid use of attribute 'fallthrough'`.
- **[[using gnu: ...]] (C23 attribute namespace scope) is unimplemented on both sides:**
  gcc 16.2.0 parse-errors (`expected ']' before 'gnu'`), goc parse-errors
  (`expected type specifier, token ";"`). Not a goc-specific regression; avoid it.
- **gcc placement notes** (for cross-porting): `[[unsequenced]]`/`[[reproducible]]` want to
  sit after the parameter list (`int f(...) [[unsequenced]] {}`); a storage class must
  come AFTER the attribute (`[[a]] static int x`, never `static [[a]] int x`); typedef
  attributes go on the declarator (`typedef int T [[...]];`, not after `typedef`).
- **`__has_c_attribute` is unusable as a guard in goc** (returns `no` for all), consistent
  with the organizer's measurement; attribute syntax itself is the correct probe.

### Bottom line for writing goc stdlib
- **Comfortably usable:** `[[noreturn]]`/`_Noreturn`, function-level `[[nodiscard]]`,
  function/variable `[[deprecated]]`, `[[maybe_unused]]`, `[[fallthrough]]` (inside a
  switch), `[[unsequenced]]`/`[[reproducible]]`, stacked attributes, attribute before a
  storage class.
- **Avoid / caveats:** type-level (struct/enum/typedef) `[[nodiscard]]` and any attribute
  on a parameter (goc parse error); depending on `[[gnu::aligned(N)]]` for true alignment
  (parsed but ignored); relying on `[[fallthrough]]` being validated, or on the custom
  deprecated message text reaching the goc user.
