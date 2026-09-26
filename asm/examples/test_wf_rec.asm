; test_wf_rec.asm - recursive function that calls WriteFile inside recursion.
; g(n): if n>0 call g(n-1) then write 'X'.  g(2) should print "XX".
section .data
buf          db "X", 0
bytesWritten dq 0
hStdout      dq 0

section .text
global _start

extern GetStdHandle, kernel32
extern WriteFile,    kernel32
extern ExitProcess,  kernel32

g:
    push rbp
    mov rbp, rsp
    sub rsp, 40
    push rbx
    push rsi
    push r12
    cmp rcx, 0
    jle .ret
    sub rcx, 1
    call g
    ; write 'X'
    mov rcx, [rip+hStdout]
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
.ret:
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
    mov rcx, 2
    call g
    mov rcx, 0
    call ExitProcess
