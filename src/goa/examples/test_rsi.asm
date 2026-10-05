; test_rsi.asm - does rsi (set from idiv remainder) survive a depth-2 call?
; f(13): remainder of 13/10 = 3. Store in rsi, then call print_digit(rsi).
; Expect '3'. If corrupted to arg (13), print_digit(13&0xF=13) -> '='.
section .data
buf          db 0
bytesWritten dq 0
hStdout      dq 0

section .text
global _start

extern GetStdHandle, kernel32
extern WriteFile,    kernel32
extern ExitProcess,  kernel32

f:
    push rbp
    mov rbp, rsp
    sub rsp, 40
    push rbx
    push rsi
    push r12
    mov rbx, rcx           ; n
    xor rdx, rdx
    mov rax, rcx
    mov rcx, 10
    idiv rcx               ; rax=q, rdx=remainder
    mov rsi, rdx           ; rsi = remainder
    mov rcx, rsi           ; pass rsi to leaf
    call print_digit       ; depth 2
    pop r12
    pop rsi
    pop rbx
    add rsp, 40
    pop rbp
    ret

print_digit:
    push rbp
    mov rbp, rsp
    sub rsp, 40
    push rbx
    push rsi
    push r12
    and rcx, 0xF
    add rcx, 0x30
    mov [rip+buf], cl
    mov rcx, [rip+hStdout]
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
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
    mov rcx, 13
    call f
    mov rcx, 0
    call ExitProcess
