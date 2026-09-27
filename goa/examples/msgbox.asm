; msgbox.asm - Win32 GUI demo: a real message box, no console, no libc.
;
; Exercises: importing from a second DLL (user32), the wide-char API
; (MessageBoxW with UTF-16 strings via `du`), branching on the return value,
; and `subsystem windows` so Windows does not allocate a console.

section .data
caption  du "goa GUI 演示"
text     du "这是 goa 编译出来的。\n纯 Go 汇编器 + 手写 PE 写入器，全程没用 gcc。\n\n点“是”再看一个框，点“否”直接退出。"
yes_text du "你点了「是」。\nMessageBoxW 返回 IDYES = 6"
no_text  du "你点了「否」。\nMessageBoxW 返回 IDNO  = 7"
res_cap  du "结果"

section .text
global _start
subsystem windows

extern MessageBoxW, user32
extern ExitProcess, kernel32

; MessageBoxW flags
MB_YESNO_QUESTION = 0x24   ; MB_YESNO(4) | MB_ICONQUESTION(0x20)
MB_OK_INFO        = 0x40   ; MB_OK(0)    | MB_ICONINFORMATION(0x40)
IDYES             = 6

_start:
    and rsp, -16
    sub rsp, 48            ; shadow space (32) + slack; keeps calls aligned

    ; MessageBoxW(hWnd, lpText, lpCaption, uType) -- 4 register args
    xor rcx, rcx           ; hWnd = NULL
    lea rdx, [rip+text]
    lea r8,  [rip+caption]
    mov r9, MB_YESNO_QUESTION
    call MessageBoxW

    cmp rax, IDYES
    jne .Lno

    xor rcx, rcx
    lea rdx, [rip+yes_text]
    lea r8,  [rip+res_cap]
    mov r9, MB_OK_INFO
    call MessageBoxW
    jmp .Lexit

.Lno:
    xor rcx, rcx
    lea rdx, [rip+no_text]
    lea r8,  [rip+res_cap]
    xor r9, r9              ; MB_OK
    call MessageBoxW

.Lexit:
    xor rcx, rcx
    call ExitProcess
