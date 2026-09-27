; test_rec_idiv.asm - recursion + idiv, but NO WriteFile inside recursion.
; digitcount(n): if n<=9 return 1 else 1 + digitcount(n/10). digitcount(120)=3.
section .data
buf          db 0
bytesWritten dq 0
hStdout      dq 0

section .text
global _start

extern GetStdHandle, kernel32
extern WriteFile,    kernel32
extern ExitProcess,  kernel32

digitcount:
    push rbp
    mov rbp, rsp
    sub rsp, 40
    push rbx
    push rsi
    push r12
    cmp rcx, 9
    jle .base
    mov rbx, rcx
    xor rdx, rdx
    mov rax, rcx
    mov rcx, 10
    idiv rcx            ; rax = n/10
    mov rcx, rax
    call digitcount
    inc rax
    pop r12
    pop rsi
    pop rbx
    add rsp, 40
    pop rbp
    ret
.base:
    mov rax, 1
    pop r12
    pop rsi
    pop rbx
    add rsp, 40
    pop rbp
    ret

_start:
    and rsp, -16
    sub rsp, 48
    mov rcx, -11
    call GetStdHandle
    mov [rip+hStdout], rax
    mov rcx, 120
    call digitcount      ; expect rax = 3
    and rax, 0xF
    add rax, 0x30        ; '3'
    mov [rip+buf], al
    mov rcx, [rip+hStdout]
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
    mov rcx, 0
    call ExitProcess
