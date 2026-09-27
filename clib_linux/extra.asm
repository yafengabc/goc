; clib_linux/extra.asm -- additional <stdio.h>/<stdlib.h>/<string.h> functions.
;
; Raw syscalls only (a0 turns extern read into a stub), SysV AMD64 conventions:
;   arguments rdi, rsi, rdx, rcx, r8, r9 ; no shadow space ; at a call RSP%16 == 0.
;   zero pushes + sub rsp,8 (one push + sub rsp,16) keep the stack aligned.

; --- getchar -------------------------------------------------------------
; @data getchar
section .data
__clib_inb   db 0
; @end

; @func getchar
; @extern read
section .text
getchar:
    sub rsp, 8
    mov rdi, 0                  ; fd = stdin
    lea rsi, [rip+__clib_inb]
    mov rdx, 1
    call read
    cmp rax, 1
    jne __clib_gc_eof
    xor rax, rax
    mov al, [rip+__clib_inb]
    add rsp, 8
    ret
__clib_gc_eof:
    mov rax, -1
    add rsp, 8
    ret
; @end

; --- calloc(n, size) ------------------------------------------------------
; @func calloc
; @deps malloc memset
section .text
calloc:
    push rbx
    sub rsp, 16
    mov rbx, rdi                ; n
    imul rbx, rsi               ; rbx = n * size
    mov rdi, rbx
    call malloc
    cmp rax, 0
    je __clib_calloc_done
    mov rdi, rax
    mov rsi, 0
    mov rdx, rbx
    call memset
__clib_calloc_done:
    add rsp, 16
    pop rbx
    ret
; @end

; --- strtol(s, endp, base) -------------------------------------------------
; @func strtol
section .text
strtol:
    ; rdi = s, rsi = endp, rdx = base
    mov r10, rdi                ; current pointer
    mov r9, rsi                 ; endp
    mov r11, rdx                ; base (0 = auto-detect)
    xor rax, rax                ; result accumulator
    xor rcx, rcx                ; sign flag (0 = +, 1 = -)
__clib_sl_ws:
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0x20
    je __clib_sl_ws_adv
    cmp rdx, 0x09
    je __clib_sl_ws_adv
    jmp __clib_sl_sign
__clib_sl_ws_adv:
    inc r10
    jmp __clib_sl_ws
__clib_sl_sign:
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0x2d               ; '-'
    jne __clib_sl_sign_p
    mov rcx, 1
    inc r10
    jmp __clib_sl_base
__clib_sl_sign_p:
    cmp rdx, 0x2b               ; '+'
    jne __clib_sl_base
    inc r10
__clib_sl_base:
    cmp r11, 0
    jne __clib_sl_parse
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0x30               ; '0'
    jne __clib_sl_dec
    mov dl, [r10+1]
    cmp rdx, 0x78               ; 'x'
    je __clib_sl_hex
    cmp rdx, 0x58               ; 'X'
    je __clib_sl_hex
    mov r11, 8                  ; leading 0 (not 0x) => octal
    jmp __clib_sl_parse
__clib_sl_hex:
    mov r11, 16
    add r10, 2
    jmp __clib_sl_parse
__clib_sl_dec:
    mov r11, 10
__clib_sl_parse:
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0
    je __clib_sl_end
    cmp rdx, 0x30
    jl __clib_sl_end
    cmp rdx, 0x39
    jg __clib_sl_a1
    sub rdx, 0x30
    jmp __clib_sl_check
__clib_sl_a1:
    cmp rdx, 0x41               ; 'A'
    jl __clib_sl_end
    cmp rdx, 0x5a               ; 'Z'
    jg __clib_sl_a2
    sub rdx, 0x41
    add rdx, 10
    jmp __clib_sl_check
__clib_sl_a2:
    cmp rdx, 0x61               ; 'a'
    jl __clib_sl_end
    cmp rdx, 0x7a               ; 'z'
    jg __clib_sl_end
    sub rdx, 0x61
    add rdx, 10
__clib_sl_check:
    cmp rdx, r11
    jge __clib_sl_end
    mov r8, r11
    imul r8, rax                ; r8 = base * result
    add r8, rdx                 ; + digit
    mov rax, r8
    inc r10
    jmp __clib_sl_parse
__clib_sl_end:
    cmp rcx, 1
    jne __clib_sl_store
    neg rax
__clib_sl_store:
    cmp r9, 0
    je __clib_sl_ret
    mov [r9], r10               ; *endp = first unconverted position
__clib_sl_ret:
    ret
; @end

; --- rand / srand (LCG) ----------------------------------------------------
; @data rand,srand
section .data
__clib_rand_state    dq 1
; @end

; @func rand
section .text
rand:
    mov rax, [rip+__clib_rand_state]
    imul rax, 1103515245
    add rax, 12345
    mov [rip+__clib_rand_state], rax
    shr rax, 16
    and rax, 0x7fff
    ret
; @end

; @func srand
section .text
srand:
    mov [rip+__clib_rand_state], rdi
    ret
; @end

; --- strchr(s, c) ---------------------------------------------------------
; @func strchr
section .text
strchr:
    and rsi, 0xff               ; mask the search byte
__clib_strchr_loop:
    xor rax, rax
    mov al, [rdi]
    cmp rax, rsi
    je __clib_strchr_found
    cmp rax, 0
    je __clib_strchr_notfound
    inc rdi
    jmp __clib_strchr_loop
__clib_strchr_found:
    mov rax, rdi
    ret
__clib_strchr_notfound:
    xor rax, rax
    ret
; @end

; --- strncmp(s1, s2, n) ---------------------------------------------------
; @func strncmp
section .text
strncmp:
    mov r8, rdi                 ; s1
    mov r9, rsi                 ; s2
    mov r10, rdx                ; n
__clib_strncmp_loop:
    cmp r10, 0
    je __clib_strncmp_eq
    xor rax, rax
    mov al, [r8]
    xor r11, r11
    mov r11b, [r9]
    cmp rax, r11
    jne __clib_strncmp_diff
    cmp rax, 0
    je __clib_strncmp_eq
    inc r8
    inc r9
    dec r10
    jmp __clib_strncmp_loop
__clib_strncmp_diff:
    sub rax, r11
    ret
__clib_strncmp_eq:
    xor rax, rax
    ret
; @end

; --- memcmp(s1, s2, n) ----------------------------------------------------
; @func memcmp
section .text
memcmp:
    mov r8, rdi                 ; s1
    mov r9, rsi                 ; s2
    mov r10, rdx                ; n
__clib_memcmp_loop:
    cmp r10, 0
    je __clib_memcmp_eq
    xor rax, rax
    mov al, [r8]
    xor r11, r11
    mov r11b, [r9]
    cmp rax, r11
    jne __clib_memcmp_diff
    inc r8
    inc r9
    dec r10
    jmp __clib_memcmp_loop
__clib_memcmp_diff:
    sub rax, r11
    ret
__clib_memcmp_eq:
    xor rax, rax
    ret
; @end

; --- memmove(dest, src, n) ------------------------------------------------
; @func memmove
section .text
memmove:
    cmp rdi, rsi
    jg __clib_memmove_bwd
    mov r8, rdi                 ; dest
    mov r9, rsi                 ; src
    mov r10, rdx                ; n
__clib_memmove_fwd_loop:
    cmp r10, 0
    je __clib_memmove_done
    xor rax, rax
    mov al, [r9]
    mov [r8], al
    inc r8
    inc r9
    dec r10
    jmp __clib_memmove_fwd_loop
__clib_memmove_bwd:
    mov r8, rdi
    add r8, rdx
    mov r9, rsi
    add r9, rdx
    mov r10, rdx
__clib_memmove_bwd_loop:
    cmp r10, 0
    je __clib_memmove_done
    dec r8
    dec r9
    xor rax, rax
    mov al, [r9]
    mov [r8], al
    dec r10
    jmp __clib_memmove_bwd_loop
__clib_memmove_done:
    mov rax, rdi
    ret
; @end
