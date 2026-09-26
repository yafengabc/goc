; Linux ELF64 -- recursive fact/fib plus an iterative decimal printer.
;
; Chosen deliberately as a *numeric* test: "hello" would pass even if the
; interpreter merely guessed. fact(5)=120 and fib(10)=55 exercise recursion,
; idiv, SIB byte addressing and a call-through-stub loop.
;
; expect exactly:
;   fact(5) = 120
;   fib(10) = 55
;
;   a0 -f elf num.asm && ./num

extern write
extern exit

section .rdata
msg1 db "fact(5) = "
msg2 db "fib(10) = "
nl   db 10

section .data
digits db 32 dup(0)
buf    db 0

section .text
global _start
_start:
    mov rdi, 1
    lea rsi, [rip+msg1]
    mov rdx, 10
    call write

    mov rcx, 5
    call fact
    mov rdi, rax
    call print_num
    call newline

    mov rdi, 1
    lea rsi, [rip+msg2]
    mov rdx, 10
    call write

    mov rcx, 10
    call fib
    mov rdi, rax
    call print_num
    call newline

    mov rdi, 0
    call exit

newline:
    mov rdi, 1
    lea rsi, [rip+nl]
    mov rdx, 1
    call write
    ret

; fact(rcx) -> rax
fact:
    cmp rcx, 1
    jg .Lrec
    mov rax, 1
    ret
.Lrec:
    push rcx
    dec rcx
    call fact
    pop rcx
    imul rax, rcx
    ret

; fib(rcx) -> rax
fib:
    cmp rcx, 2
    jge .Lrec
    mov rax, rcx
    ret
.Lrec:
    push rcx
    dec rcx
    call fib
    mov rbx, rax          ; fib(n-1)
    pop rcx
    push rbx
    sub rcx, 2
    call fib              ; rax = fib(n-2)
    pop rbx
    add rax, rbx
    ret

; print_num(rdi) -- iterative: digits land in `digits` LSD-first, then are
; written back MSD-first. r13/r14 are call-preserved, so the write stub
; (which clobbers rax/rcx/rdx/rsi/rdi/r11) cannot disturb the loop.
print_num:
    push r13
    push r14
    lea r13, [rip+digits]
    xor r14, r14
    mov rax, rdi
.Ldiv:
    xor rdx, rdx
    mov rcx, 10
    idiv rcx              ; rax = quotient, rdx = remainder
    mov rbx, rdx
    add rbx, 0x30
    mov [r13+r14], bl
    inc r14
    cmp rax, 0
    jne .Ldiv
.Lemit:
    dec r14
    mov al, [r13+r14]
    mov [rip+buf], al
    mov rdi, 1
    lea rsi, [rip+buf]
    mov rdx, 1
    call write
    cmp r14, 0
    jne .Lemit
    pop r14
    pop r13
    ret
