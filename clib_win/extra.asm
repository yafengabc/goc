; clib_win/extra.asm -- additional <stdio.h>/<stdlib.h>/<string.h> functions.
;
; All implemented on top of kernel32, matching the rest of the Win clib:
;   arguments rcx, rdx, r8, r9 ; 32-byte shadow space ; at a call RSP%16 == 0.
;   odd pushes + sub rsp,32 (two pushes + sub rsp,40) keep the stack aligned.

; --- getchar -------------------------------------------------------------
; @data getchar
section .data
__clib_in    dq 0               ; stdin handle, lazily initialised
__clib_inb   db 0               ; read scratch byte
__clib_inr   dq 0               ; bytes-read scratch
; @end

; @func getchar
; @extern GetStdHandle ReadFile
section .text
getchar:
    push r12
    sub rsp, 32
    mov r12, [rip+__clib_in]
    cmp r12, 0
    jne __clib_gc_have
    mov rcx, -10                ; STD_INPUT_HANDLE
    call GetStdHandle
    mov [rip+__clib_in], rax
__clib_gc_have:
    mov rcx, [rip+__clib_in]
    lea rdx, [rip+__clib_inb]
    mov r8, 1
    lea r9, [rip+__clib_inr]
    mov [rsp+32], 0             ; lpOverlapped = NULL
    call ReadFile
    mov rax, [rip+__clib_inr]
    cmp rax, 1
    jne __clib_gc_eof
    xor rax, rax
    mov al, [rip+__clib_inb]
    add rsp, 32
    pop r12
    ret
__clib_gc_eof:
    mov rax, -1
    add rsp, 32
    pop r12
    ret
; @end

; --- calloc(n, size) = malloc(n*size) then memset(p, 0, n*size) -----------
; @func calloc
; @deps malloc memset
section .text
calloc:
    push rbx
    sub rsp, 32
    mov rbx, rcx                ; n
    imul rbx, rdx               ; rbx = n * size
    mov rcx, rbx
    call malloc
    cmp rax, 0
    je __clib_calloc_done
    mov rcx, rax
    mov rdx, 0
    mov r8, rbx
    call memset
__clib_calloc_done:
    add rsp, 32
    pop rbx
    ret
; @end

; --- strtol(s, endp, base) -------------------------------------------------
; @func strtol
section .text
strtol:
    ; rcx = s, rdx = endp, r8 = base
    mov r10, rcx                ; current pointer
    mov r9, rdx                 ; endp
    mov r11, r8                 ; base (0 = auto-detect)
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
    mov [rip+__clib_rand_state], rcx
    ret
; @end

; --- strchr(s, c) ---------------------------------------------------------
; @func strchr
section .text
strchr:
    and rdx, 0xff                ; mask the search byte
__clib_strchr_loop:
    xor rax, rax
    mov al, [rcx]
    cmp rax, rdx
    je __clib_strchr_found
    cmp rax, 0
    je __clib_strchr_notfound
    inc rcx
    jmp __clib_strchr_loop
__clib_strchr_found:
    mov rax, rcx
    ret
__clib_strchr_notfound:
    xor rax, rax
    ret
; @end

; --- strncmp(s1, s2, n) ---------------------------------------------------
; @func strncmp
section .text
strncmp:
    mov r10, r8                 ; n
__clib_strncmp_loop:
    cmp r10, 0
    je __clib_strncmp_eq
    xor rax, rax
    mov al, [rcx]
    xor r9, r9
    mov r9b, [rdx]
    cmp rax, r9
    jne __clib_strncmp_diff
    cmp rax, 0
    je __clib_strncmp_eq
    inc rcx
    inc rdx
    dec r10
    jmp __clib_strncmp_loop
__clib_strncmp_diff:
    sub rax, r9
    ret
__clib_strncmp_eq:
    xor rax, rax
    ret
; @end

; --- memcmp(s1, s2, n) ----------------------------------------------------
; @func memcmp
section .text
memcmp:
    mov r10, r8                 ; n
__clib_memcmp_loop:
    cmp r10, 0
    je __clib_memcmp_eq
    xor rax, rax
    mov al, [rcx]
    xor r9, r9
    mov r9b, [rdx]
    cmp rax, r9
    jne __clib_memcmp_diff
    inc rcx
    inc rdx
    dec r10
    jmp __clib_memcmp_loop
__clib_memcmp_diff:
    sub rax, r9
    ret
__clib_memcmp_eq:
    xor rax, rax
    ret
; @end

; --- memmove(dest, src, n) ------------------------------------------------
; @func memmove
section .text
memmove:
    cmp rcx, rdx
    jg __clib_memmove_bwd
    mov r9, rcx                 ; dest
    mov r10, rdx                ; src
    mov r11, r8                 ; n
__clib_memmove_fwd_loop:
    cmp r11, 0
    je __clib_memmove_done
    xor rax, rax
    mov al, [r10]
    mov [r9], al
    inc r9
    inc r10
    dec r11
    jmp __clib_memmove_fwd_loop
__clib_memmove_bwd:
    mov r9, rcx
    add r9, r8
    mov r10, rdx
    add r10, r8
    mov r11, r8
__clib_memmove_bwd_loop:
    cmp r11, 0
    je __clib_memmove_done
    dec r9
    dec r10
    xor rax, rax
    mov al, [r10]
    mov [r9], al
    dec r11
    jmp __clib_memmove_bwd_loop
__clib_memmove_done:
    mov rax, rcx
    ret
; @end
