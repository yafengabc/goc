; Linux ELF64 hello world -- static, no libc, no dynamic linker.
;
; `extern write` / `extern exit` are not DLL imports here: for an ELF target
; goa turns each into a `mov rax, <number>; syscall; ret` stub, so the call
; site uses the SysV argument registers (rdi, rsi, rdx) directly.
;
;   goa -f elf hello.asm  &&  ./hello

extern write
extern exit

section .data
msg db "Hello, Linux!", 10

section .text
global _start
_start:
    mov rdi, 1              ; fd = stdout
    lea rsi, [rip+msg]      ; buf
    mov rdx, 14             ; count ("Hello, Linux!" + LF)
    call write

    mov rdi, 0              ; exit status
    call exit
