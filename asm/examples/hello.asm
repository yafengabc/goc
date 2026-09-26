; hello.asm - prints "Hello, world!" using kernel32 directly.
; Assembled + linked by a0 into a stand-alone Windows PE executable.

section .data
msg          db "Hello, world!", 10, 0
bytesWritten dq 0
hStdout      dq 0

section .text
global _start

extern GetStdHandle, kernel32
extern WriteFile,    kernel32
extern ExitProcess,  kernel32

_start:
    and rsp, -16
    sub rsp, 48
    ; hStdout = GetStdHandle(STD_OUTPUT_HANDLE = -11)
    mov rcx, -11
    call GetStdHandle
    mov [rip+hStdout], rax
    ; WriteFile(hStdout, msg, 14, &bytesWritten, NULL)
    mov rcx, [rip+hStdout]
    lea rdx, [rip+msg]
    mov r8, 14
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
    ; ExitProcess(0)
    mov rcx, 0
    call ExitProcess
