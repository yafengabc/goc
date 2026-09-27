; test_rec.asm - does plain recursion (no idiv) return the right value?
; addk(n) = n + addk(n-1), base 0.  addk(3) = 6.
section .data
buf          db 0
bytesWritten dq 0
hStdout      dq 0

section .text
global _start

extern GetStdHandle, kernel32
extern WriteFile,    kernel32
extern ExitProcess,  kernel32

addk:
    push rbp
    mov rbp, rsp
    sub rsp, 40
    push rbx
    cmp rcx, 0
    je .base
    mov rbx, rcx
    sub rcx, 1
    call addk
    add rax, rbx
    pop rbx
    add rsp, 40
    pop rbp
    ret
.base:
    mov rax, 0
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
    mov rcx, 3
    call addk            ; expect rax = 6
    and rax, 0xF
    add rax, 0x30       ; '6'
    mov [rip+buf], al
    mov rcx, [rip+hStdout]
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
    mov rcx, 0
    call ExitProcess
