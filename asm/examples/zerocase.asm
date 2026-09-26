; zerocase.asm - isolates the "mov byte [rip+sym], imm" (C6) encoding.
; Should print 'A' (0x41). If it prints something else, branch 6 is buggy.
section .data
buf          db 0
bytesWritten dq 0
hStdout      dq 0
section .text
global _start
extern GetStdHandle, kernel32
extern WriteFile,    kernel32
extern ExitProcess,  kernel32
_start:
    and rsp, -16
    sub rsp, 0x40
    mov rcx, -11
    call GetStdHandle
    mov [rip+hStdout], rax
    mov byte [rip+buf], 0x41
    mov rcx, [rip+hStdout]
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
    mov rcx, 0
    call ExitProcess
