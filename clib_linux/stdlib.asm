; clib_linux/stdlib.asm -- <stdlib.h> subset for Linux.
;
; malloc grows the program break with the brk syscall (a0 turns
; `extern brk` into `mov rax,12; syscall; ret`). free is a no-op: this is a
; bump allocator, which is honest for a toy libc -- the process gives
; everything back at exit anyway.

; @func __clib_exit
; @extern exit
section .text
; NOTE: this is deliberately not named `exit`. On an ELF target a0 defines a
; syscall stub with the extern's own name, so a clib function called `exit`
; would overwrite that stub's symbol and recurse forever. c0 maps the C name
; `exit` to this symbol (see clibAlias).
__clib_exit:
    sub rsp, 8                   ; keep the stack 16-aligned across the call
    call exit                    ; rdi already holds the status
    ret                          ; never reached
; @end

; @func malloc
; @extern brk
section .data
__clib_brk    dq 0              ; cached program break (0 = not initialised)
section .text
malloc:
    ; rdi = size -> rax = pointer (or 0 on failure)
    push rbx
    push r12
    sub rsp, 8
    mov rbx, rdi                 ; size
    mov rax, [rip+__clib_brk]
    cmp rax, 0
    jne __clib_malloc_have
    xor rdi, rdi
    call brk                     ; brk(0) -> current break
    mov [rip+__clib_brk], rax
__clib_malloc_have:
    mov r12, [rip+__clib_brk]    ; this block starts here
    mov rax, r12
    add rax, rbx                 ; new break
    add rax, 15
    and rax, -16                 ; keep blocks 16-byte aligned
    mov rdi, rax
    call brk
    cmp rax, 0
    jl __clib_malloc_fail
    mov [rip+__clib_brk], rax
    mov rax, r12
    jmp __clib_malloc_done
__clib_malloc_fail:
    xor rax, rax
__clib_malloc_done:
    add rsp, 8
    pop r12
    pop rbx
    ret
; @end

; @func free
section .text
free:
    ; Bump allocator: nothing to reclaim.
    ret
; @end

; @func atoi
section .text
atoi:
    xor rax, rax                 ; value
    xor r8, r8                   ; negative flag
    xor rdx, rdx
    mov dl, [rdi]
    cmp rdx, 0x2d                ; '-'
    jne __clib_atoi_loop
    mov r8, 1
    inc rdi
__clib_atoi_loop:
    xor rdx, rdx
    mov dl, [rdi]
    cmp rdx, 0
    je __clib_atoi_done
    cmp rdx, 0x30
    jl __clib_atoi_done          ; stop at the first non-digit
    cmp rdx, 0x39
    jg __clib_atoi_done
    sub rdx, 0x30
    mov r9, 10
    imul rax, r9
    add rax, rdx
    inc rdi
    jmp __clib_atoi_loop
__clib_atoi_done:
    cmp r8, 0
    je __clib_atoi_pos
    neg rax
__clib_atoi_pos:
    ret
; @end

; @func abs
section .text
abs:
    mov rax, rdi
    cmp rax, 0
    jge __clib_abs_pos
    neg rax
__clib_abs_pos:
    ret
; @end
