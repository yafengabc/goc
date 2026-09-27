; diag.asm - isolate idiv/remainder extraction without recursion.
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
    sub rsp, 48
    mov rcx, -11
    call GetStdHandle
    mov [rip+hStdout], rax

    ; rax = 120 / 10  -> q=12, r=0
    mov rax, 120
    xor rdx, rdx
    mov rcx, 10
    idiv rcx
    mov rbx, rax   ; quotient 12
    mov r12, rdx   ; remainder 0

    ; print quotient as char
    mov rax, rbx
    add rax, 0x30
    mov [rip+buf], al
    mov rcx, [rip+hStdout]
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile

    ; print remainder as char
    mov rax, r12
    add rax, 0x30
    mov [rip+buf], al
    mov rcx, [rip+hStdout]
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile

    mov rcx, 0
    call ExitProcess
