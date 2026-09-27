; Same hello, but issuing `syscall` inline instead of going through stubs.
; Exercises the raw instruction path and the .rdata section.

section .rdata
msg db "syscall works", 10

section .text
global _start
_start:
    mov rax, 1              ; SYS_write
    mov rdi, 1              ; fd = stdout
    lea rsi, [rip+msg]
    mov rdx, 14
    syscall

    mov rax, 60             ; SYS_exit
    xor rdi, rdi            ; status 0
    syscall
