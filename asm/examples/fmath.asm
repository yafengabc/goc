; fmath.asm - exercises the SSE2 scalar-double subset of a0.
; Output should be "46321":
;   1.5 + 2.5            -> 4
;   3.0 * 2.0            -> 6
;   7.0 / 2.0            -> 3   (truncated 3.5)
;   sqrt(4.0)            -> 2
;   3.0 > 2.0 ? 1 : 0    -> 1

section .data
buf          db 0
bytesWritten dq 0
hStdout      dq 0
C1_5         dq 1.5
C2_5         dq 2.5
C3_0         dq 3.0
C2_0         dq 2.0
C7_0         dq 7.0
C4_0         dq 4.0

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

    ; 1.5 + 2.5 = 4.0
    movsd xmm0, [rip+C1_5]
    addsd xmm0, [rip+C2_5]
    cvttsd2si rax, xmm0
    call write_digit

    ; 3.0 * 2.0 = 6.0
    movsd xmm0, [rip+C3_0]
    mulsd xmm0, [rip+C2_0]
    cvttsd2si rax, xmm0
    call write_digit

    ; 7.0 / 2.0 = 3.5 -> truncate 3
    movsd xmm0, [rip+C7_0]
    divsd xmm0, [rip+C2_0]
    cvttsd2si rax, xmm0
    call write_digit

    ; sqrt(4.0) = 2.0
    movsd xmm0, [rip+C4_0]
    sqtsd xmm0, xmm0
    cvttsd2si rax, xmm0
    call write_digit

    ; 3.0 > 2.0 ? 1 : 0
    movsd xmm0, [rip+C3_0]
    ucomisd xmm0, [rip+C2_0]
    ja .gt
    mov rax, 0
    jmp .done
.gt:
    mov rax, 1
.done:
    call write_digit

    mov rcx, 0
    call ExitProcess

write_digit:
    add al, 48
    mov [rip+buf], al
    mov rcx, [rip+hStdout]
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
    ret
