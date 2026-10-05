; loop.asm - print 120 via stack-based digit extraction (no recursion).
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

    mov rax, 120
    xor rbx, rbx          ; digit count
    mov rcx, 10
.loop:
    xor rdx, rdx
    idiv rcx              ; rax=q, rdx=digit
    add rdx, 0x30
    push rdx
    inc rbx
    cmp rax, 0
    jne .loop
.printloop:
    cmp rbx, 0
    je .done
    pop rdx                ; digit (LIFO -> most significant digit first)
    mov rbp, rsp           ; remember the digit-stack top
    mov [rip+buf], dl
    ; Drop BELOW the digits to build the call frame: 32 bytes of shadow space
    ; plus 8 bytes for the 5th argument. The remaining digits live above rsp,
    ; i.e. inside the shadow space, so WriteFile would clobber them otherwise.
    sub rsp, 40
    and rsp, -16           ; Win64 requires 16-byte alignment at the call
    mov rcx, [rip+hStdout]
    lea rdx, [rip+buf]
    mov r8, 1
    lea r9, [rip+bytesWritten]
    mov [rsp+32], 0
    call WriteFile
    mov rsp, rbp           ; restore the digit stack
    dec rbx
    jmp .printloop
.done:
    mov rcx, 0
    call ExitProcess
