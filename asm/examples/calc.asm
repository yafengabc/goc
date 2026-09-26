; calc.asm - computes fact(5) and prints "fact(5) = 120" via kernel32.
; Exercises: imports, recursion, loops-style division, RIP-relative data,
; internal calls, and a hand-written decimal print routine.

section .data
msg          db "fact(5) = ", 0
lf           db 10, 0
buf          db 0
digits       db 32 dup(0)
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
    ; hStdout = GetStdHandle(-11)
    mov rcx, -11
    call GetStdHandle
    mov [rip+hStdout], rax
    ; print "fact(5) = "
    lea rcx, [rip+msg]
    mov rdx, 10
    call print_str
    ; compute fact(5)
    mov rcx, 5
    call fact
    mov rcx, rax
    call print_num
    ; newline
    lea rcx, [rip+lf]
    mov rdx, 1
    call print_str
    ; exit
    mov rcx, 0
    call ExitProcess

; print_str(rcx = ptr, rdx = len)
print_str:
    push rbp
    mov rbp, rsp
    sub rsp, 40
    push rbx
    push rsi
    push r12
    mov rsi, rcx        ; ptr
    mov rbx, rdx        ; len
    mov r12, [rip+hStdout]
    mov rcx, r12        ; hFile
    mov rdx, rsi        ; lpBuffer
    mov r8, rbx         ; nNumberOfBytesToWrite
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
    pop r12
    pop rsi
    pop rbx
    add rsp, 40
    pop rbp
    ret

; print_num(rcx = unsigned number) -- iterative, no self-recursion.
; Digits are extracted LSD-first into a scratch buffer, then emitted MSD-first.
; Loop variables use NON-VOLATILE registers (r13=base, r14=index) because
; WriteFile (a Windows API) is free to clobber the volatile r10/r11.
print_num:
    push rbp
    mov rbp, rsp
    sub rsp, 0x100
    push rbx
    push r12
    push r13
    push r14
    mov r12, [rip+hStdout]
    cmp rcx, 0
    jne .Lnz
    ; special case: print '0'
    mov byte [rip+buf], 0x30
    mov rcx, r12
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
    jmp .Lend
.Lnz:
    lea r13, [rip+digits]   ; buffer base
    xor r14, r14            ; digit index = 0
.Ldiv:
    mov rax, rcx
    xor rdx, rdx
    mov rcx, 10
    idiv rcx                ; rax = quotient, rdx = remainder
    mov rbx, rdx
    add rbx, 0x30          ; ASCII digit
    mov [r13+r14], bl       ; store LSD first
    inc r14
    mov rcx, rax
    cmp rcx, 0
    jne .Ldiv
.Lemit:
    cmp r14, 0
    je .Lend
    dec r14
    mov al, [r13+r14]       ; ASCII digit (MSD first on the way out)
    mov [rip+buf], al
    mov rcx, r12
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
    jmp .Lemit
.Lend:
    pop r14
    pop r13
    pop r12
    pop rbx
    add rsp, 0x100
    pop rbp
    ret

; fact(rcx = n) -> rax
fact:
    push rbp
    mov rbp, rsp
    sub rsp, 40
    push rbx
    mov rbx, rcx
    cmp rcx, 1
    jg .Lrec
    mov rax, 1
    jmp .Lret
.Lrec:
    sub rcx, 1
    call fact
    mov rcx, rbx
    imul rax, rcx
.Lret:
    pop rbx
    add rsp, 40
    pop rbp
    ret
