; clib_linux/string.asm -- <string.h> subset, SysV argument order
; (rdi, rsi, rdx).
;
; All of these are leaf functions: they touch only volatile registers, so
; they need no prologue and no stack alignment -- there is no call inside.
;
; Strings are NUL-terminated byte sequences, 8-bit ops throughout.

; @func strlen
section .text
strlen:
    xor rax, rax
__clib_strlen_loop:
    xor rdx, rdx
    mov dl, [rdi+rax]
    cmp rdx, 0
    je __clib_strlen_done
    inc rax
    jmp __clib_strlen_loop
__clib_strlen_done:
    ret
; @end

; @func strcpy
section .text
strcpy:
    mov r8, rdi                  ; remember dst
__clib_strcpy_loop:
    xor rax, rax
    mov al, [rsi]
    mov [rdi], al                ; copies the NUL too
    inc rdi
    inc rsi
    cmp rax, 0
    jne __clib_strcpy_loop
    mov rax, r8
    ret
; @end

; @func strcmp
section .text
strcmp:
    push rbx
__clib_strcmp_loop:
    xor rax, rax
    mov al, [rdi]
    xor rbx, rbx
    mov bl, [rsi]
    cmp rax, rbx                 ; both zero-extended: full-width compare is fine
    jne __clib_strcmp_diff
    cmp rax, 0
    je __clib_strcmp_eq
    inc rdi
    inc rsi
    jmp __clib_strcmp_loop
__clib_strcmp_diff:
    sub rax, rbx
    pop rbx
    ret
__clib_strcmp_eq:
    xor rax, rax
    pop rbx
    ret
; @end

; @func strcat
section .text
strcat:
    mov r8, rdi                  ; remember dst
__clib_strcat_scan:
    xor rax, rax
    mov al, [rdi]
    cmp rax, 0
    je __clib_strcat_copy
    inc rdi
    jmp __clib_strcat_scan
__clib_strcat_copy:
    xor rax, rax
    mov al, [rsi]
    mov [rdi], al
    inc rdi
    inc rsi
    cmp rax, 0
    jne __clib_strcat_copy
    mov rax, r8
    ret
; @end

; @func memset
section .text
memset:
    mov r9, rdi                  ; remember dst
__clib_memset_loop:
    cmp rdx, 0
    je __clib_memset_done
    mov [rdi], sil
    inc rdi
    dec rdx
    jmp __clib_memset_loop
__clib_memset_done:
    mov rax, r9
    ret
; @end

; @func memcpy
section .text
memcpy:
    mov r9, rdi                  ; remember dst
__clib_memcpy_loop:
    cmp rdx, 0
    je __clib_memcpy_done
    xor rax, rax
    mov al, [rsi]
    mov [rdi], al
    inc rdi
    inc rsi
    dec rdx
    jmp __clib_memcpy_loop
__clib_memcpy_done:
    mov rax, r9
    ret
; @end
