; arrsum.asm - sums an integer array using SIB addressing [base+index*scale]
; and prints the result. Exercises: import table, lea, register-based memory
; ([r11+r13*8]), the iterative print routine, and non-volatile register use.
;
; Expected output: "sum = 28" (+ newline)

section .data
arr    dq 3, 7, 11, 2, 5      ; 5 qwords, sum = 28
n      dq 5
msg    db "sum = ", 0
lf     db 10, 0
buf    db 0
digits db 64 dup(0)
bw     dq 0
hOut   dq 0

section .text
global _start

extern GetStdHandle, kernel32
extern WriteFile,    kernel32
extern ExitProcess,  kernel32

_start:
    and rsp, -16
    sub rsp, 0x40
    ; hOut = GetStdHandle(-11)
    mov rcx, -11
    call GetStdHandle
    mov [rip+hOut], rax
    ; print "sum = "
    lea rcx, [rip+msg]
    mov rdx, 6
    call print_str
    ; sum the array: r11=base, r12=count, r13=i, r14=accumulator
    lea r11, [rip+arr]
    mov r12, [rip+n]
    xor r13, r13
    xor r14, r14
.Lsum:
    cmp r13, r12
    jge .Ldone
    mov rax, [r11 + r13*8]   ; arr[i] via SIB (base + index*8)
    add r14, rax
    inc r13
    jmp .Lsum
.Ldone:
    mov rcx, r14
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
    sub rsp, 0x28
    push rbx
    push rsi
    push r12
    mov rsi, rcx
    mov rbx, rdx
    mov r12, [rip+hOut]
    mov rcx, r12
    mov rdx, rsi
    mov r8, rbx
    lea r9, [rip+bw]
    mov [rsp+32], 0
    call WriteFile
    pop r12
    pop rsi
    pop rbx
    add rsp, 0x28
    pop rbp
    ret

; print_num(rcx = unsigned number) -- iterative, SIB-based scratch buffer.
print_num:
    push rbp
    mov rbp, rsp
    sub rsp, 0x100
    push rbx
    push r12
    push r13
    push r14
    mov r12, [rip+hOut]
    cmp rcx, 0
    jne .Lnz
    mov byte [rip+buf], 0x30
    mov rcx, r12
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bw]
    mov [rsp+32], 0
    call WriteFile
    jmp .Lend
.Lnz:
    lea r13, [rip+digits]
    xor r14, r14
.Ldiv:
    mov rax, rcx
    xor rdx, rdx
    mov rcx, 10
    idiv rcx
    mov rbx, rdx
    add rbx, 0x30
    mov [r13+r14], bl
    inc r14
    mov rcx, rax
    cmp rcx, 0
    jne .Ldiv
.Lemit:
    cmp r14, 0
    je .Lend
    dec r14
    mov al, [r13+r14]
    mov [rip+buf], al
    mov rcx, r12
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bw]
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
