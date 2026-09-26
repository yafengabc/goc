; clib_win/string.asm -- <string.h> subset.
;
; All of these are leaf functions: they touch only volatile registers
; (rax/rcx/rdx/r8/r9/r10/r11), so they need no prologue and no stack
; alignment -- there is no call inside them.
;
; Strings are NUL-terminated byte sequences, 8-bit ops throughout.

; @func strlen
section .text
strlen:
    xor rax, rax
__clib_strlen_loop:
    xor rdx, rdx
    mov dl, [rcx+rax]
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
    mov r8, rcx                  ; remember dst
__clib_strcpy_loop:
    xor rax, rax
    mov al, [rdx]
    mov [rcx], al                ; copies the NUL too
    inc rcx
    inc rdx
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
    mov al, [rcx]
    xor rbx, rbx
    mov bl, [rdx]
    cmp rax, rbx                 ; both zero-extended: full-width compare is fine
    jne __clib_strcmp_diff
    cmp rax, 0
    je __clib_strcmp_eq
    inc rcx
    inc rdx
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
    mov r8, rcx                  ; remember dst
__clib_strcat_scan:
    xor rax, rax
    mov al, [rcx]
    cmp rax, 0
    je __clib_strcat_copy
    inc rcx
    jmp __clib_strcat_scan
__clib_strcat_copy:
    xor rax, rax
    mov al, [rdx]
    mov [rcx], al
    inc rcx
    inc rdx
    cmp rax, 0
    jne __clib_strcat_copy
    mov rax, r8
    ret
; @end

; @func memset
section .text
memset:
    mov r9, rcx                  ; remember dst
__clib_memset_loop:
    cmp r8, 0
    je __clib_memset_done
    mov [rcx], dl
    inc rcx
    dec r8
    jmp __clib_memset_loop
__clib_memset_done:
    mov rax, r9
    ret
; @end

; @func memcpy
section .text
memcpy:
    mov r9, rcx                  ; remember dst
__clib_memcpy_loop:
    cmp r8, 0
    je __clib_memcpy_done
    xor rax, rax
    mov al, [rdx]
    mov [rcx], al
    inc rcx
    inc rdx
    dec r8
    jmp __clib_memcpy_loop
__clib_memcpy_done:
    mov rax, r9
    ret
; @end
