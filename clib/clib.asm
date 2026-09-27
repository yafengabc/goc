; clib/clib.asm -- single, conditionally-compiled C-library backend for c0.
;
; One source carries BOTH platforms. c0 selects the right branch at load time
; with #if defined(_WIN64) / #else (see selectPlatform in codegen.go). This is
; the same trick a normal compiler uses for inline assembly: write the
; cross-platform parts once and isolate the OS-specific bits behind #ifdef.
;
; The Windows branch mirrors the old clib/windows/* (kernel32 WriteFile,
; GetProcessHeap/HeapAlloc); the Linux branch mirrors clib/linux/* (raw
; write/brk/exit/read syscalls, SysV argument order). Same C names.
;
; Public functions: printf sprintf puts putchar getchar strlen strcpy strcmp
; strcat strchr memset memcpy memmove memcmp strncmp malloc free calloc atoi
; abs strtol rand srand exit.  Platform primitives (the only bits that truly
; cannot be cross-platform) are __clib_write __clib_exit __clib_heap_alloc
; __clib_heap_free __clib_read and the formatter __clib_vfmt.

#if defined(_WIN64)
; clib/stdio.asm -- <stdio.h> subset, built on kernel32 WriteFile only.
;
; Layout of a formatted call on Windows x64:
;   printf(fmt, ...)      rcx = fmt, varargs start at rdx
;   sprintf(dst, fmt, ...) rcx = dst, rdx = fmt, varargs start at r8
; Varargs land in registers first (3 of them), then on the stack, which the
; callee sees at [rbp+48], [rbp+56], ... (above the 32-byte home space).
; We spill them all into __clib_va once, up front, before any call.

; Static data is tagged with the functions that need it, so a program that
; only calls putchar() does not carry printf()'s 512-byte buffer around.

; @data __clib_write
section .data
__clib_out    dq 0               ; stdout handle, lazily initialised
__clib_bytes  dq 0               ; WriteFile scratch
; @end

; @data putchar
section .data
__clib_ch     db 0               ; putchar scratch
; @end

; @data puts
section .data
__clib_nl     db 10, 0           ; puts() newline
; @end

; @data printf,sprintf
section .data
__clib_va     dq 0, 0, 0, 0, 0, 0, 0, 0
__clib_digits db 32 dup(0)       ; reversed digits while converting
__clib_buf    db 512 dup(0)      ; printf() output buffer
__clib_f10    dq 10.0            ; %f: fractional-digit scaling factor
__clib_fneg   dq -0.0            ; %f: sign bit (0x8000000000000000)
; @end

; @func __clib_write
; @extern GetStdHandle WriteFile
section .text
__clib_write:
    ; rcx = buffer, rdx = length -> rax = bytes written
    push r12
    push r13
    sub rsp, 40                  ; 32 shadow + 8 for the 5th arg (2 pushes = even)
    mov r12, rcx
    mov r13, rdx
    mov rcx, [rip+__clib_out]
    cmp rcx, 0
    jne __clib_write_have
    mov rcx, -11                 ; STD_OUTPUT_HANDLE
    call GetStdHandle
    mov [rip+__clib_out], rax
__clib_write_have:
    mov rcx, [rip+__clib_out]
    mov rdx, r12
    mov r8, r13
    lea r9, [rip+__clib_bytes]
    mov [rsp+32], 0              ; lpOverlapped = NULL
    call WriteFile
    add rsp, 40
    pop r13
    pop r12
    ret
; @end

; @func __clib_vfmt
section .text
__clib_vfmt:
    ; rcx = dst, rdx = fmt, r8 = va array, r9 = limit -> rax = chars written
    ; Supports %d %s %c %x %f %%. Stops as soon as the buffer is full.
    ; %f prints a fixed 6 fractional digits, like C's default %.6f.
    push rbp
    mov rbp, rsp
    push rbx
    push rsi
    push rdi
    push r12
    push r13
    push r14
    push r15
    mov rdi, rcx                 ; dst
    mov rsi, rdx                 ; fmt
    mov r13, r8                  ; va cursor
    mov r14, r9                  ; limit
    xor r12, r12                 ; output length
    cmp r14, 0
    jg __clib_vfmt_loop
    jmp __clib_vfmt_done
__clib_vfmt_loop:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0
    je __clib_vfmt_done
    cmp rax, 0x25                ; '%'
    je __clib_vfmt_pct
    mov [rdi+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_loop
__clib_vfmt_pct:
    inc rsi
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0                   ; trailing '%' -> stop
    je __clib_vfmt_done
    cmp rax, 0x64                ; 'd'
    je __clib_vfmt_num
    cmp rax, 0x73                ; 's'
    je __clib_vfmt_str
    cmp rax, 0x63                ; 'c'
    je __clib_vfmt_chr
    cmp rax, 0x78                ; 'x'
    je __clib_vfmt_hex
    cmp rax, 0x66                ; 'f'
    je __clib_vfmt_flt
    cmp rax, 0x25                ; '%'
    je __clib_vfmt_esc
    mov bl, 0x25                 ; unknown spec: emit it verbatim
    mov [rdi+r12], bl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    mov [rdi+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_loop
__clib_vfmt_esc:
    mov [rdi+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_loop
__clib_vfmt_chr:
    mov rbx, [r13]
    add r13, 8
    mov [rdi+r12], bl
    inc r12
    inc rsi
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_loop
__clib_vfmt_str:
    mov rbx, [r13]
    add r13, 8
__clib_vfmt_strl:
    xor rax, rax
    mov al, [rbx]
    cmp rax, 0
    je __clib_vfmt_next
    mov [rdi+r12], al
    inc r12
    inc rbx
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_strl
__clib_vfmt_num:
    mov rax, [r13]
    add r13, 8
    cmp rax, 0
    jge __clib_vfmt_pos
    neg rax
    mov bl, 0x2d                 ; '-'
    mov [rdi+r12], bl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
__clib_vfmt_pos:
    lea r15, [rip+__clib_digits]
    xor rcx, rcx                 ; digit count
__clib_vfmt_dv:
    xor rdx, rdx
    mov rbx, 10
    idiv rbx                     ; rax = quotient, rdx = remainder
    add rdx, 0x30
    mov [r15+rcx], dl
    inc rcx
    cmp rax, 0
    jne __clib_vfmt_dv
__clib_vfmt_em:
    cmp rcx, 0
    je __clib_vfmt_next
    dec rcx
    mov dl, [r15+rcx]
    mov [rdi+r12], dl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_em
__clib_vfmt_hex:
    mov rax, [r13]
    add r13, 8
    lea r15, [rip+__clib_digits]
    xor rcx, rcx
__clib_vfmt_hv:
    xor rdx, rdx
    mov rbx, 16
    idiv rbx
    cmp rdx, 10
    jge __clib_vfmt_halpha
    add rdx, 0x30                ; '0'..'9'
    jmp __clib_vfmt_hsto
__clib_vfmt_halpha:
    add rdx, 0x57                ; 'a'..'f'  ('a' - 10 == 0x57)
__clib_vfmt_hsto:
    mov [r15+rcx], dl
    inc rcx
    cmp rax, 0
    jne __clib_vfmt_hv
    jmp __clib_vfmt_em
__clib_vfmt_flt:
    ; 8-byte va slot holds the IEEE-754 bits of the double
    mov rax, [r13]
    add r13, 8
    movq xmm0, rax
    cmp rax, 0                   ; sign bit set?
    jl __clib_vfmt_flt_neg
__clib_vfmt_flt_pos:
    cvttsd2si rax, xmm0          ; integer part (truncated toward zero)
    cvtsi2sd xmm1, rax
    subsd xmm0, xmm1             ; xmm0 = fractional part, 0 <= frac < 1
    lea r15, [rip+__clib_digits]
    xor rcx, rcx                 ; digit count
__clib_vfmt_flt_dv:
    xor rdx, rdx
    mov rbx, 10
    idiv rbx
    add rdx, 0x30
    mov [r15+rcx], dl
    inc rcx
    cmp rax, 0
    jne __clib_vfmt_flt_dv
__clib_vfmt_flt_em:
    cmp rcx, 0
    je __clib_vfmt_flt_dot
    dec rcx
    mov dl, [r15+rcx]
    mov [rdi+r12], dl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_flt_em
__clib_vfmt_flt_dot:
    mov bl, 0x2e                 ; '.'
    mov [rdi+r12], bl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    mov r9, 6                    ; six fractional digits
__clib_vfmt_flt_fr:
    mulsd xmm0, [rip+__clib_f10]
    cvttsd2si rcx, xmm0          ; next digit
    cvtsi2sd xmm1, rcx
    subsd xmm0, xmm1
    mov rdx, 0x30
    add rdx, rcx
    mov [rdi+r12], dl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    dec r9
    jne __clib_vfmt_flt_fr
    jmp __clib_vfmt_next
__clib_vfmt_flt_neg:
    mov bl, 0x2d                 ; '-'
    mov [rdi+r12], bl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    mov rax, [rip+__clib_fneg]   ; clear the sign bit: |x| = x xor sign
    movq xmm1, rax
    xorpd xmm0, xmm1
    jmp __clib_vfmt_flt_pos
__clib_vfmt_next:
    inc rsi
    jmp __clib_vfmt_loop
__clib_vfmt_done:
    mov rax, r12
    pop r15
    pop r14
    pop r13
    pop r12
    pop rdi
    pop rsi
    pop rbx
    mov rsp, rbp
    pop rbp
    ret
; @end

; @func printf
; @deps __clib_vfmt __clib_write
section .text
printf:
    ; rcx = fmt, varargs in rdx/r8/r9 then the stack
    push rbp
    mov rbp, rsp
    push rbx
    push rsi
    push rdi
    push r12
    push r13
    push r14
    push r15
    sub rsp, 40                  ; 8 pushes = even, so 40 lands on the boundary
    mov rsi, rcx                 ; fmt
    lea rbx, [rip+__clib_va]
    mov [rbx], rdx
    mov [rbx+8], r8
    mov [rbx+16], r9
    mov rax, [rbp+48]
    mov [rbx+24], rax
    mov rax, [rbp+56]
    mov [rbx+32], rax
    mov rax, [rbp+64]
    mov [rbx+40], rax
    mov rax, [rbp+72]
    mov [rbx+48], rax
    mov rax, [rbp+80]
    mov [rbx+56], rax
    lea rcx, [rip+__clib_buf]
    mov rdx, rsi
    lea r8, [rip+__clib_va]
    mov r9, 512
    call __clib_vfmt
    mov r12, rax
    lea rcx, [rip+__clib_buf]
    mov rdx, r12
    call __clib_write
    mov rax, r12
    add rsp, 40
    pop r15
    pop r14
    pop r13
    pop r12
    pop rdi
    pop rsi
    pop rbx
    mov rsp, rbp
    pop rbp
    ret
; @end

; @func sprintf
; @deps __clib_vfmt
section .text
sprintf:
    ; rcx = dst, rdx = fmt, varargs in r8/r9 then the stack
    ; No bounds checking, exactly like the real sprintf: the caller owns the
    ; buffer. Returns the number of characters written (excluding the NUL).
    push rbp
    mov rbp, rsp
    push rbx
    push rsi
    sub rsp, 32                  ; 3 pushes = odd, vfmt takes 4 register args
    mov rsi, rcx                 ; dst
    lea rbx, [rip+__clib_va]
    mov [rbx], r8
    mov [rbx+8], r9
    mov rax, [rbp+48]
    mov [rbx+16], rax
    mov rax, [rbp+56]
    mov [rbx+24], rax
    mov rax, [rbp+64]
    mov [rbx+32], rax
    mov rax, [rbp+72]
    mov [rbx+40], rax
    mov rax, [rbp+80]
    mov [rbx+48], rax
    mov rcx, rsi
    lea r8, [rip+__clib_va]
    mov r9, 0x7fffffff
    call __clib_vfmt
    add rsi, rax                 ; NUL-terminate
    xor rbx, rbx
    mov [rsi], bl
    add rsp, 32
    pop rsi
    pop rbx
    mov rsp, rbp
    pop rbp
    ret
; @end

; @func puts
; @deps __clib_write strlen
section .text
puts:
    ; rcx = string; prints it plus a newline
    push r12
    push r13
    sub rsp, 40
    mov r12, rcx
    call strlen                  ; rcx is still the string
    mov r13, rax
    mov rcx, r12
    mov rdx, r13
    call __clib_write
    lea rcx, [rip+__clib_nl]
    mov rdx, 1
    call __clib_write
    mov rax, r13
    add rsp, 40
    pop r13
    pop r12
    ret
; @end

; @func putchar
; @deps __clib_write
section .text
putchar:
    ; rcx = character
    push r12
    push r13
    sub rsp, 40
    mov r12, rcx
    mov [rip+__clib_ch], cl
    lea rcx, [rip+__clib_ch]
    mov rdx, 1
    call __clib_write
    mov rax, r12
    add rsp, 40
    pop r13
    pop r12
    ret
; @end
; clib/stdlib.asm -- <stdlib.h> subset.
;
; malloc/free go straight to the process heap via kernel32
; (GetProcessHeap / HeapAlloc / HeapFree), so there is still no msvcrt.
;
; Stack alignment reminder (Windows x64): on entry RSP is 8 mod 16.
;   odd number of pushes  + "sub rsp, 32"  -> 16-aligned at call
;   even number of pushes + "sub rsp, 40"  -> 16-aligned at call
; The 40-byte form is needed whenever we pass a 5th argument at [rsp+32].

; @func exit
; @extern ExitProcess
section .text
exit:
    sub rsp, 40                  ; shadow space for ExitProcess
    call ExitProcess             ; rcx already holds the exit code
    ret                          ; never reached
; @end

; --- platform primitives carved out for the C version (clib/c0lib.c) ---------
; These mirror the inline heap/IO logic already used by malloc/getchar but are
; exposed under the __clib_ prefix so clib/c0lib.c can call them directly.

; @func __clib_exit
; @extern ExitProcess
section .text
__clib_exit:
    sub rsp, 40                  ; shadow space for ExitProcess
    call ExitProcess             ; rcx already holds the exit code
    ret                          ; never reached
; @end

; @func __clib_heap_alloc
; @extern GetProcessHeap HeapAlloc
section .text
__clib_heap_alloc:
    push rbx
    sub rsp, 32
    mov rbx, rcx                 ; size
    call GetProcessHeap
    mov rcx, rax                 ; heap
    mov rdx, 0                   ; flags
    mov r8, rbx                  ; size
    call HeapAlloc
    add rsp, 32
    pop rbx
    ret
; @end

; @func __clib_heap_free
; @extern GetProcessHeap HeapFree
section .text
__clib_heap_free:
    push rbx
    sub rsp, 32
    mov rbx, rcx                 ; pointer
    call GetProcessHeap
    mov rcx, rax                 ; heap
    mov rdx, 0                   ; flags
    mov r8, rbx                  ; pointer
    call HeapFree
    add rsp, 32
    pop rbx
    ret
; @end

; @data __clib_read
section .data
__clib_rdh    dq 0               ; stdin handle, lazily initialised
__clib_rdn    dq 0               ; bytes-read scratch
; @end

; @func __clib_read
; @extern GetStdHandle ReadFile
section .text
__clib_read:
    push r12
    push r13
    sub rsp, 32
    mov r12, rcx                 ; buf
    mov r13, rdx                 ; len
    mov rdx, [rip+__clib_rdh]
    cmp rdx, 0
    jne __clib_read_have
    mov rcx, -10                 ; STD_INPUT_HANDLE
    call GetStdHandle
    mov [rip+__clib_rdh], rax
__clib_read_have:
    mov rcx, [rip+__clib_rdh]
    mov rdx, r12                 ; buf
    mov r8, r13                  ; len
    lea r9, [rip+__clib_rdn]
    mov [rsp+32], 0              ; lpOverlapped = NULL
    call ReadFile
    mov rax, [rip+__clib_rdn]
    add rsp, 32
    pop r13
    pop r12
    ret
; @end

; @func malloc
; @extern GetProcessHeap HeapAlloc
section .text
malloc:
    push rbx
    sub rsp, 32
    mov rbx, rcx                 ; size (GetProcessHeap would clobber it)
    call GetProcessHeap
    mov rcx, rax                 ; heap
    mov rdx, 0                   ; flags
    mov r8, rbx                  ; size
    call HeapAlloc
    add rsp, 32
    pop rbx
    ret
; @end

; @func free
; @extern GetProcessHeap HeapFree
section .text
free:
    push rbx
    sub rsp, 32
    mov rbx, rcx                 ; pointer
    call GetProcessHeap
    mov rcx, rax                 ; heap
    mov rdx, 0                   ; flags
    mov r8, rbx                  ; pointer
    call HeapFree
    add rsp, 32
    pop rbx
    ret
; @end

; @func atoi
section .text
atoi:
    xor rax, rax                 ; value
    xor r8, r8                   ; negative flag
    xor rdx, rdx
    mov dl, [rcx]
    cmp rdx, 0x2d                ; '-'
    jne __clib_atoi_loop
    mov r8, 1
    inc rcx
__clib_atoi_loop:
    xor rdx, rdx
    mov dl, [rcx]
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
    inc rcx
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
    mov rax, rcx
    cmp rax, 0
    jge __clib_abs_pos
    neg rax
__clib_abs_pos:
    ret
; @end
; clib/string.asm -- <string.h> subset.
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
; clib/extra.asm -- additional <stdio.h>/<stdlib.h>/<string.h> functions.
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

#else
; clib/stdio.asm -- <stdio.h> subset for Linux, raw syscalls only.
;
; SysV AMD64 argument order: rdi, rsi, rdx, rcx, r8, r9, then the stack at
; [rbp+16], [rbp+24], ... (there is no shadow space).
;
;   printf(fmt, ...)        rdi = fmt, varargs start at rsi
;   sprintf(dst, fmt, ...)  rdi = dst, rsi = fmt, varargs start at rdx
;
; a0 turns `extern write` into a `mov rax,1; syscall; ret` stub, so there is
; no libc and no dynamic linker anywhere in the pipeline.

; @data __clib_write
section .data
__clib_bytes  dq 0               ; write() scratch (unused, kept for parity)
; @end

; @data putchar
section .data
__clib_ch     db 0               ; putchar scratch
; @end

; @data puts
section .data
__clib_nl     db 10, 0           ; puts() newline
; @end

; @data printf,sprintf
section .data
__clib_va     dq 0, 0, 0, 0, 0, 0, 0, 0
__clib_digits db 32 dup(0)       ; reversed digits while converting
__clib_buf    db 512 dup(0)      ; printf() output buffer
__clib_f10    dq 10.0            ; %f: fractional-digit scaling factor
__clib_fneg   dq -0.0            ; %f: sign bit (0x8000000000000000)
; @end

; @func __clib_write
; @extern write
section .text
__clib_write:
    ; rdi = buffer, rsi = length -> rax = bytes written
    push r12
    push r13
    sub rsp, 8
    mov r12, rdi
    mov r13, rsi
    mov rdi, 1                   ; fd = stdout
    mov rsi, r12
    mov rdx, r13
    call write
    add rsp, 8
    pop r13
    pop r12
    ret
; @end

; @func __clib_vfmt
section .text
__clib_vfmt:
    ; rdi = dst, rsi = fmt, rdx = va array, rcx = limit -> rax = chars written
    ; Supports %d %s %c %x %f %%. Stops as soon as the buffer is full.
    ; %f prints a fixed 6 fractional digits, like C's default %.6f.
    push rbp
    mov rbp, rsp
    push rbx
    push r12
    push r13
    push r14
    push r15
    sub rsp, 8
    mov r15, rdi                 ; dst
    mov r13, rdx                 ; va cursor
    mov r14, rcx                 ; limit
    xor r12, r12                 ; output length
    cmp r14, 0
    jg __clib_vfmt_loop
    jmp __clib_vfmt_done
__clib_vfmt_loop:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0
    je __clib_vfmt_done
    cmp rax, 0x25                ; '%'
    je __clib_vfmt_pct
    mov [r15+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_loop
__clib_vfmt_pct:
    inc rsi
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0                   ; trailing '%' -> stop
    je __clib_vfmt_done
    cmp rax, 0x64                ; 'd'
    je __clib_vfmt_num
    cmp rax, 0x73                ; 's'
    je __clib_vfmt_str
    cmp rax, 0x63                ; 'c'
    je __clib_vfmt_chr
    cmp rax, 0x78                ; 'x'
    je __clib_vfmt_hex
    cmp rax, 0x66                ; 'f'
    je __clib_vfmt_flt
    cmp rax, 0x25                ; '%'
    je __clib_vfmt_esc
    mov bl, 0x25                 ; unknown spec: emit it verbatim
    mov [r15+r12], bl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    mov [r15+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_loop
__clib_vfmt_esc:
    mov [r15+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_loop
__clib_vfmt_chr:
    mov rbx, [r13]
    add r13, 8
    mov [r15+r12], bl
    inc r12
    inc rsi
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_loop
__clib_vfmt_str:
    mov rbx, [r13]
    add r13, 8
__clib_vfmt_strl:
    xor rax, rax
    mov al, [rbx]
    cmp rax, 0
    je __clib_vfmt_next
    mov [r15+r12], al
    inc r12
    inc rbx
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_strl
__clib_vfmt_num:
    mov rax, [r13]
    add r13, 8
    cmp rax, 0
    jge __clib_vfmt_pos
    neg rax
    mov bl, 0x2d                 ; '-'
    mov [r15+r12], bl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
__clib_vfmt_pos:
    lea rbx, [rip+__clib_digits]
    xor rcx, rcx                 ; digit count
__clib_vfmt_dv:
    xor rdx, rdx
    mov r11, 10
    idiv r11                     ; rax = quotient, rdx = remainder
    add rdx, 0x30
    mov [rbx+rcx], dl
    inc rcx
    cmp rax, 0
    jne __clib_vfmt_dv
    jmp __clib_vfmt_em
__clib_vfmt_hex:
    mov rax, [r13]
    add r13, 8
    lea rbx, [rip+__clib_digits]
    xor rcx, rcx
__clib_vfmt_hv:
    xor rdx, rdx
    mov r11, 16
    idiv r11
    cmp rdx, 10
    jge __clib_vfmt_halpha
    add rdx, 0x30                ; '0'..'9'
    jmp __clib_vfmt_hsto
__clib_vfmt_halpha:
    add rdx, 0x57                ; 'a'..'f'
__clib_vfmt_hsto:
    mov [rbx+rcx], dl
    inc rcx
    cmp rax, 0
    jne __clib_vfmt_hv
    jmp __clib_vfmt_em          ; do not fall through into the %f handler
__clib_vfmt_flt:
    ; 8-byte va slot holds the IEEE-754 bits of the double
    mov rax, [r13]
    add r13, 8
    movq xmm0, rax
    cmp rax, 0                   ; sign bit set?
    jl __clib_vfmt_flt_neg
__clib_vfmt_flt_pos:
    cvttsd2si rax, xmm0          ; integer part (truncated toward zero)
    cvtsi2sd xmm1, rax
    subsd xmm0, xmm1             ; xmm0 = fractional part, 0 <= frac < 1
    lea rbx, [rip+__clib_digits]
    xor rcx, rcx                 ; digit count
__clib_vfmt_flt_dv:
    xor rdx, rdx
    mov r11, 10
    idiv r11
    add rdx, 0x30
    mov [rbx+rcx], dl
    inc rcx
    cmp rax, 0
    jne __clib_vfmt_flt_dv
__clib_vfmt_flt_em:
    cmp rcx, 0
    je __clib_vfmt_flt_dot
    dec rcx
    mov dl, [rbx+rcx]
    mov [r15+r12], dl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_flt_em
__clib_vfmt_flt_dot:
    mov bl, 0x2e                 ; '.'
    mov [r15+r12], bl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    mov r9, 6                    ; six fractional digits
__clib_vfmt_flt_fr:
    mulsd xmm0, [rip+__clib_f10]
    cvttsd2si rcx, xmm0          ; next digit
    cvtsi2sd xmm1, rcx
    subsd xmm0, xmm1
    mov rdx, 0x30
    add rdx, rcx
    mov [r15+r12], dl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    dec r9
    jne __clib_vfmt_flt_fr
    jmp __clib_vfmt_next
__clib_vfmt_flt_neg:
    mov bl, 0x2d                 ; '-'
    mov [r15+r12], bl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    mov rax, [rip+__clib_fneg]   ; clear the sign bit: |x| = x xor sign
    movq xmm1, rax
    xorpd xmm0, xmm1
    jmp __clib_vfmt_flt_pos
__clib_vfmt_em:
    cmp rcx, 0
    je __clib_vfmt_next
    dec rcx
    mov dl, [rbx+rcx]
    mov [r15+r12], dl
    inc r12
    cmp r12, r14
    jge __clib_vfmt_done
    jmp __clib_vfmt_em
__clib_vfmt_next:
    inc rsi
    jmp __clib_vfmt_loop
__clib_vfmt_done:
    mov rax, r12
    add rsp, 8
    pop r15
    pop r14
    pop r13
    pop r12
    pop rbx
    mov rsp, rbp
    pop rbp
    ret
; @end

; @func printf
; @deps __clib_vfmt __clib_write
section .text
printf:
    ; rdi = fmt, varargs in rsi,rdx,rcx,r8,r9 then the stack
    push rbp
    mov rbp, rsp
    push rbx
    push r12
    push r13
    push r14
    push r15
    sub rsp, 8
    mov r13, rdi                 ; fmt
    lea rbx, [rip+__clib_va]
    mov [rbx], rsi
    mov [rbx+8], rdx
    mov [rbx+16], rcx
    mov [rbx+24], r8
    mov [rbx+32], r9
    mov rax, [rbp+16]
    mov [rbx+40], rax
    mov rax, [rbp+24]
    mov [rbx+48], rax
    mov rax, [rbp+32]
    mov [rbx+56], rax
    lea rdi, [rip+__clib_buf]
    mov rsi, r13
    lea rdx, [rip+__clib_va]
    mov rcx, 512
    call __clib_vfmt
    mov r12, rax
    lea rdi, [rip+__clib_buf]
    mov rsi, r12
    call __clib_write
    mov rax, r12
    add rsp, 8
    pop r15
    pop r14
    pop r13
    pop r12
    pop rbx
    mov rsp, rbp
    pop rbp
    ret
; @end

; @func sprintf
; @deps __clib_vfmt
section .text
sprintf:
    ; rdi = dst, rsi = fmt, varargs in rdx,rcx,r8,r9 then the stack
    push rbp
    mov rbp, rsp
    push rbx
    push r12
    push r13
    push r14
    push r15
    sub rsp, 8
    mov r13, rdi                 ; dst
    mov r14, rsi                 ; fmt
    lea rbx, [rip+__clib_va]
    mov [rbx], rdx
    mov [rbx+8], rcx
    mov [rbx+16], r8
    mov [rbx+24], r9
    mov rax, [rbp+16]
    mov [rbx+32], rax
    mov rax, [rbp+24]
    mov [rbx+40], rax
    mov rax, [rbp+32]
    mov [rbx+48], rax
    mov rdi, r13
    mov rsi, r14
    lea rdx, [rip+__clib_va]
    mov rcx, 0x7fffffff
    call __clib_vfmt
    mov r12, rax
    mov rdi, r13
    add rdi, r12
    xor rbx, rbx
    mov [rdi], bl                ; NUL-terminate
    mov rax, r12
    add rsp, 8
    pop r15
    pop r14
    pop r13
    pop r12
    pop rbx
    mov rsp, rbp
    pop rbp
    ret
; @end

; @func puts
; @deps __clib_write strlen
section .text
puts:
    ; rdi = string; prints it plus a newline
    push r12
    push r13
    sub rsp, 8
    mov r12, rdi
    call strlen                  ; rdi is still the string
    mov r13, rax
    mov rdi, r12
    mov rsi, r13
    call __clib_write
    lea rdi, [rip+__clib_nl]
    mov rsi, 1
    call __clib_write
    mov rax, r13
    add rsp, 8
    pop r13
    pop r12
    ret
; @end

; @func putchar
; @deps __clib_write
section .text
putchar:
    ; rdi = character
    push r12
    sub rsp, 8
    mov r12, rdi
    mov [rip+__clib_ch], dil
    lea rdi, [rip+__clib_ch]
    mov rsi, 1
    call __clib_write
    mov rax, r12
    add rsp, 8
    pop r12
    ret
; @end
; clib/stdlib.asm -- <stdlib.h> subset for Linux.
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

; --- platform primitives carved out for the C version (clib/c0lib.c) ---------
; These mirror the inline heap/IO logic already used by malloc/getchar but are
; exposed under the __clib_ prefix so clib/c0lib.c can call them directly.

; @data __clib_heap_alloc,malloc
section .data
__clib_brk    dq 0              ; cached program break (0 = not initialised)
; @end

; @func __clib_heap_alloc
; @extern brk
section .text
__clib_heap_alloc:
    ; rdi = size -> rax = pointer (or 0 on failure)
    push rbx
    push r12
    sub rsp, 8
    mov rbx, rdi                 ; size
    mov rax, [rip+__clib_brk]
    cmp rax, 0
    jne __clib_ha_have
    xor rdi, rdi
    call brk                     ; brk(0) -> current break
    mov [rip+__clib_brk], rax
__clib_ha_have:
    mov r12, [rip+__clib_brk]    ; this block starts here
    mov rax, r12
    add rax, rbx                 ; new break
    add rax, 15
    and rax, -16                 ; keep blocks 16-byte aligned
    mov rdi, rax
    call brk
    cmp rax, 0
    jl __clib_ha_fail
    mov [rip+__clib_brk], rax
    mov rax, r12
    jmp __clib_ha_done
__clib_ha_fail:
    xor rax, rax
__clib_ha_done:
    add rsp, 8
    pop r12
    pop rbx
    ret
; @end

; @func __clib_heap_free
section .text
__clib_heap_free:
    ; Bump allocator: nothing to reclaim.
    ret
; @end

; @func __clib_read
; @extern read
section .text
__clib_read:
    ; rdi = buf, rsi = len -> rax = bytes read (or <=0 on error)
    push rbx
    push r12
    sub rsp, 8
    mov rbx, rdi                 ; buf
    mov r12, rsi                 ; len
    mov rdi, 0                   ; fd = stdin
    mov rsi, rbx                 ; buf
    mov rdx, r12                 ; len
    call read
    add rsp, 8
    pop r12
    pop rbx
    ret
; @end

; @func malloc
; @extern brk
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
; clib/string.asm -- <string.h> subset, SysV argument order
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
; clib/extra.asm -- additional <stdio.h>/<stdlib.h>/<string.h> functions.
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

#endif
