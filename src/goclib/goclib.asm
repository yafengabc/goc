; goclib/goclib.asm -- single, conditionally-compiled C-library backend for goc.
;
; One source carries BOTH platforms. goc selects the right branch at load time
; with #if defined(_WIN64) / #else (see selectPlatform in codegen.go). This is
; the same trick a normal compiler uses for inline assembly: write the
; cross-platform parts once and isolate the OS-specific bits behind #ifdef.
;
; The Windows branch mirrors the old goclib/windows/* (kernel32 WriteFile,
; GetProcessHeap/HeapAlloc); the Linux branch mirrors goclib/linux/* (raw
; write/brk/exit/read syscalls, SysV argument order). Same C names.
;
; Public functions: printf sprintf puts putchar getchar strlen strcpy strcmp
; strcat strchr memset memcpy memmove memcmp strncmp malloc free calloc atoi
; abs strtol rand srand exit.  Platform primitives (the only bits that truly
; cannot be cross-platform) are __goclib_write __goclib_exit __goclib_heap_alloc
; __goclib_heap_free __goclib_read and the formatter __goclib_vfmt.

#if defined(_WIN64)
; goclib/stdio.asm -- <stdio.h> subset, built on kernel32 WriteFile only.
;
; Layout of a formatted call on Windows x64:
;   printf(fmt, ...)      rcx = fmt, varargs start at rdx
;   sprintf(dst, fmt, ...) rcx = dst, rdx = fmt, varargs start at r8
; Varargs land in registers first (3 of them), then on the stack, which the
; callee sees at [rbp+48], [rbp+56], ... (above the 32-byte home space).
; We spill them all into __goclib_va once, up front, before any call.

; Static data is tagged with the functions that need it, so a program that
; only calls putchar() does not carry printf()'s 512-byte buffer around.

; @data __goclib_write
section .data
__goclib_out    dq 0               ; stdout handle, lazily initialised
__goclib_bytes  dq 0               ; WriteFile scratch
; @end

; @data putchar
section .data
__goclib_ch     db 0               ; putchar scratch
; @end

; @data puts
section .data
__goclib_nl     db 10, 0           ; puts() newline
; @end

; @data printf,sprintf
section .data
__goclib_va     dq 0, 0, 0, 0, 0, 0, 0, 0
__goclib_digits db 32 dup(0)       ; reversed digits while converting
__goclib_buf    db 512 dup(0)      ; printf() output buffer
__goclib_f10    dq 10.0            ; %f: fractional-digit scaling factor
__goclib_fneg   dq -0.0            ; %f: sign bit (0x8000000000000000)
; @end

; @func __goclib_write
; @extern GetStdHandle WriteFile
section .text
__goclib_write:
    ; rcx = buffer, rdx = length -> rax = bytes written
    push r12
    push r13
    sub rsp, 40                  ; 32 shadow + 8 for the 5th arg (2 pushes = even)
    mov r12, rcx
    mov r13, rdx
    mov rcx, [rip+__goclib_out]
    cmp rcx, 0
    jne __goclib_write_have
    mov rcx, -11                 ; STD_OUTPUT_HANDLE
    call GetStdHandle
    mov [rip+__goclib_out], rax
__goclib_write_have:
    mov rcx, [rip+__goclib_out]
    mov rdx, r12
    mov r8, r13
    lea r9, [rip+__goclib_bytes]
    mov [rsp+32], 0              ; lpOverlapped = NULL
    call WriteFile
    add rsp, 40
    pop r13
    pop r12
    ret
; @end

; @func __goclib_vfmt
section .text
__goclib_vfmt:
    ; rcx = dst, rdx = fmt, r8 = va array, r9 = limit -> rax = chars written
    ; Supports %d %s %c %x %f %%. Stops as soon as the buffer is full.
    ; %f honours an optional ".precision": %f == %.6f, %.Nf prints N digits,
    ; %.0f prints none. Field width is parsed and ignored; output is truncated
    ; (no rounding), matching the existing default-precision behaviour.
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
    jg __goclib_vfmt_loop
    jmp __goclib_vfmt_done
__goclib_vfmt_loop:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0
    je __goclib_vfmt_done
    cmp rax, 0x25                ; '%'
    je __goclib_vfmt_pct
    mov [rdi+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_loop
__goclib_vfmt_pct:
    inc rsi
    ; Parse optional field width (ignored), then ".precision", then any length
    ; modifier, so %f / %.6f / %.15f / %10.2f all reach the specifier with r9
    ; holding the fractional-digit count (default 6 when no '.' appears).
    mov r9, 6                    ; default precision
__goclib_vfmt_pf_width:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0x30
    jb __goclib_vfmt_pf_dot
    cmp rax, 0x39
    ja __goclib_vfmt_pf_dot
    inc rsi                      ; skip a width digit (width unsupported)
    jmp __goclib_vfmt_pf_width
__goclib_vfmt_pf_dot:
    cmp rax, 0x2e                ; '.'
    jne __goclib_vfmt_pf_len
    inc rsi
    xor r9, r9                   ; '.' seen: reset precision accumulator
__goclib_vfmt_pf_prec:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0x30
    jb __goclib_vfmt_pf_len
    cmp rax, 0x39
    ja __goclib_vfmt_pf_len
    imul r9, 10
    add r9, rax
    sub r9, 0x30
    inc rsi
    jmp __goclib_vfmt_pf_prec
__goclib_vfmt_pf_len:
    cmp rax, 0x6c                ; 'l'
    je __goclib_vfmt_pf_skip_len
    cmp rax, 0x4c                ; 'L'
    je __goclib_vfmt_pf_skip_len
    cmp rax, 0x68                ; 'h'
    je __goclib_vfmt_pf_skip_len
    jmp __goclib_vfmt_pf_done
__goclib_vfmt_pf_skip_len:
    inc rsi
    xor rax, rax
    mov al, [rsi]
    jmp __goclib_vfmt_pf_len
__goclib_vfmt_pf_done:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0                   ; trailing '%' -> stop
    je __goclib_vfmt_done
    cmp rax, 0x64                ; 'd'
    je __goclib_vfmt_num
    cmp rax, 0x73                ; 's'
    je __goclib_vfmt_str
    cmp rax, 0x63                ; 'c'
    je __goclib_vfmt_chr
    cmp rax, 0x78                ; 'x'
    je __goclib_vfmt_hex
    cmp rax, 0x66                ; 'f'
    je __goclib_vfmt_flt
    cmp rax, 0x75                ; 'u' (also %lu / %llu / %hu)
    je __goclib_vfmt_uns
    cmp rax, 0x25                ; '%'
    je __goclib_vfmt_esc
    mov bl, 0x25                 ; unknown spec: emit it verbatim
    mov [rdi+r12], bl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    mov [rdi+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_loop
__goclib_vfmt_esc:
    mov [rdi+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_loop
__goclib_vfmt_chr:
    mov rbx, [r13]
    add r13, 8
    mov [rdi+r12], bl
    inc r12
    inc rsi
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_loop
__goclib_vfmt_str:
    mov rbx, [r13]
    add r13, 8
__goclib_vfmt_strl:
    xor rax, rax
    mov al, [rbx]
    cmp rax, 0
    je __goclib_vfmt_next
    mov [rdi+r12], al
    inc r12
    inc rbx
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_strl
__goclib_vfmt_num:
    mov rax, [r13]
    add r13, 8
    cmp rax, 0
    jge __goclib_vfmt_pos
    neg rax
    mov bl, 0x2d                 ; '-'
    mov [rdi+r12], bl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
__goclib_vfmt_pos:
    lea r15, [rip+__goclib_digits]
    xor rcx, rcx                 ; digit count
__goclib_vfmt_dv:
    xor rdx, rdx
    mov rbx, 10
    idiv rbx                     ; rax = quotient, rdx = remainder
    add rdx, 0x30
    mov [r15+rcx], dl
    inc rcx
    cmp rax, 0
    jne __goclib_vfmt_dv
__goclib_vfmt_em:
    cmp rcx, 0
    je __goclib_vfmt_next
    dec rcx
    mov dl, [r15+rcx]
    mov [rdi+r12], dl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_em
__goclib_vfmt_uns:
    ; %u / %lu / %llu / %hu: the va slot is always 8 bytes, print unsigned.
    ; Must use unsigned div (not idiv) or values with the high bit set get
    ; treated as negative.
    mov rax, [r13]
    add r13, 8
    lea r15, [rip+__goclib_digits]
    xor rcx, rcx                 ; digit count
__goclib_vfmt_udv:
    xor rdx, rdx
    mov rbx, 10
    div rbx                      ; unsigned division
    add rdx, 0x30
    mov [r15+rcx], dl
    inc rcx
    cmp rax, 0
    jne __goclib_vfmt_udv
    jmp __goclib_vfmt_em
__goclib_vfmt_hex:
    mov rax, [r13]
    add r13, 8
    lea r15, [rip+__goclib_digits]
    xor rcx, rcx
__goclib_vfmt_hv:
    xor rdx, rdx
    mov rbx, 16
    idiv rbx
    cmp rdx, 10
    jge __goclib_vfmt_halpha
    add rdx, 0x30                ; '0'..'9'
    jmp __goclib_vfmt_hsto
__goclib_vfmt_halpha:
    add rdx, 0x57                ; 'a'..'f'  ('a' - 10 == 0x57)
__goclib_vfmt_hsto:
    mov [r15+rcx], dl
    inc rcx
    cmp rax, 0
    jne __goclib_vfmt_hv
    jmp __goclib_vfmt_em
__goclib_vfmt_flt:
    ; 8-byte va slot holds the IEEE-754 bits of the double
    mov rax, [r13]
    add r13, 8
    movq xmm0, rax
    cmp rax, 0                   ; sign bit set?
    jl __goclib_vfmt_flt_neg
__goclib_vfmt_flt_pos:
    cvttsd2si rax, xmm0          ; integer part (truncated toward zero)
    cvtsi2sd xmm1, rax
    subsd xmm0, xmm1             ; xmm0 = fractional part, 0 <= frac < 1
    lea r15, [rip+__goclib_digits]
    xor rcx, rcx                 ; digit count
__goclib_vfmt_flt_dv:
    xor rdx, rdx
    mov rbx, 10
    idiv rbx
    add rdx, 0x30
    mov [r15+rcx], dl
    inc rcx
    cmp rax, 0
    jne __goclib_vfmt_flt_dv
__goclib_vfmt_flt_em:
    cmp rcx, 0
    je __goclib_vfmt_flt_dot
    dec rcx
    mov dl, [r15+rcx]
    mov [rdi+r12], dl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_flt_em
__goclib_vfmt_flt_dot:
    cmp r9, 0
    je __goclib_vfmt_flt_nodot
    mov bl, 0x2e                 ; '.'
    mov [rdi+r12], bl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
__goclib_vfmt_flt_nodot:
    jmp __goclib_vfmt_flt_fr
__goclib_vfmt_flt_fr:
    cmp r9, 0                    ; no more fractional digits?
    je __goclib_vfmt_next
    dec r9
    mulsd xmm0, [rip+__goclib_f10]
    cvttsd2si rcx, xmm0          ; next digit
    cvtsi2sd xmm1, rcx
    subsd xmm0, xmm1
    mov rdx, 0x30
    add rdx, rcx
    mov [rdi+r12], dl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_flt_fr
__goclib_vfmt_flt_neg:
    mov bl, 0x2d                 ; '-'
    mov [rdi+r12], bl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    mov rax, [rip+__goclib_fneg]   ; clear the sign bit: |x| = x xor sign
    movq xmm1, rax
    xorpd xmm0, xmm1
    jmp __goclib_vfmt_flt_pos
__goclib_vfmt_next:
    inc rsi
    jmp __goclib_vfmt_loop
__goclib_vfmt_done:
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
; @deps __goclib_vfmt __goclib_write
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
    lea rbx, [rip+__goclib_va]
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
    lea rcx, [rip+__goclib_buf]
    mov rdx, rsi
    lea r8, [rip+__goclib_va]
    mov r9, 512
    call __goclib_vfmt
    mov r12, rax
    lea rcx, [rip+__goclib_buf]
    mov rdx, r12
    call __goclib_write
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
; @deps __goclib_vfmt
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
    lea rbx, [rip+__goclib_va]
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
    lea r8, [rip+__goclib_va]
    mov r9, 0x7fffffff
    call __goclib_vfmt
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
; @deps __goclib_write strlen
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
    call __goclib_write
    lea rcx, [rip+__goclib_nl]
    mov rdx, 1
    call __goclib_write
    mov rax, r13
    add rsp, 40
    pop r13
    pop r12
    ret
; @end

; @func putchar
; @deps __goclib_write
section .text
putchar:
    ; rcx = character
    push r12
    push r13
    sub rsp, 40
    mov r12, rcx
    mov [rip+__goclib_ch], cl
    lea rcx, [rip+__goclib_ch]
    mov rdx, 1
    call __goclib_write
    mov rax, r12
    add rsp, 40
    pop r13
    pop r12
    ret
; @end
; goclib/stdlib.asm -- <stdlib.h> subset.
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

; --- platform primitives carved out for the C version (goclib/goclib.c) ---------
; These mirror the inline heap/IO logic already used by malloc/getchar but are
; exposed under the __goclib_ prefix so goclib/goclib.c can call them directly.

; @func __goclib_exit
; @extern ExitProcess
section .text
__goclib_exit:
    sub rsp, 40                  ; shadow space for ExitProcess
    call ExitProcess             ; rcx already holds the exit code
    ret                          ; never reached
; @end

; @func __goclib_heap_alloc
; @extern GetProcessHeap HeapAlloc
section .text
__goclib_heap_alloc:
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

; @func __goclib_heap_free
; @extern GetProcessHeap HeapFree
section .text
__goclib_heap_free:
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

; @data __goclib_read
section .data
__goclib_rdh    dq 0               ; stdin handle, lazily initialised
__goclib_rdn    dq 0               ; bytes-read scratch
; @end

; @func __goclib_read
; @extern GetStdHandle ReadFile
section .text
__goclib_read:
    push r12
    push r13
    sub rsp, 32
    mov r12, rcx                 ; buf
    mov r13, rdx                 ; len
    mov rdx, [rip+__goclib_rdh]
    cmp rdx, 0
    jne __goclib_read_have
    mov rcx, -10                 ; STD_INPUT_HANDLE
    call GetStdHandle
    mov [rip+__goclib_rdh], rax
__goclib_read_have:
    mov rcx, [rip+__goclib_rdh]
    mov rdx, r12                 ; buf
    mov r8, r13                  ; len
    lea r9, [rip+__goclib_rdn]
    mov [rsp+32], 0              ; lpOverlapped = NULL
    call ReadFile
    mov rax, [rip+__goclib_rdn]
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
    jne __goclib_atoi_loop
    mov r8, 1
    inc rcx
__goclib_atoi_loop:
    xor rdx, rdx
    mov dl, [rcx]
    cmp rdx, 0
    je __goclib_atoi_done
    cmp rdx, 0x30
    jl __goclib_atoi_done          ; stop at the first non-digit
    cmp rdx, 0x39
    jg __goclib_atoi_done
    sub rdx, 0x30
    mov r9, 10
    imul rax, r9
    add rax, rdx
    inc rcx
    jmp __goclib_atoi_loop
__goclib_atoi_done:
    cmp r8, 0
    je __goclib_atoi_pos
    neg rax
__goclib_atoi_pos:
    ret
; @end

; @func abs
section .text
abs:
    mov rax, rcx
    cmp rax, 0
    jge __goclib_abs_pos
    neg rax
__goclib_abs_pos:
    ret
; @end
; goclib/string.asm -- <string.h> subset.
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
__goclib_strlen_loop:
    xor rdx, rdx
    mov dl, [rcx+rax]
    cmp rdx, 0
    je __goclib_strlen_done
    inc rax
    jmp __goclib_strlen_loop
__goclib_strlen_done:
    ret
; @end

; @func strcpy
section .text
strcpy:
    mov r8, rcx                  ; remember dst
__goclib_strcpy_loop:
    xor rax, rax
    mov al, [rdx]
    mov [rcx], al                ; copies the NUL too
    inc rcx
    inc rdx
    cmp rax, 0
    jne __goclib_strcpy_loop
    mov rax, r8
    ret
; @end

; @func strcmp
section .text
strcmp:
    push rbx
__goclib_strcmp_loop:
    xor rax, rax
    mov al, [rcx]
    xor rbx, rbx
    mov bl, [rdx]
    cmp rax, rbx                 ; both zero-extended: full-width compare is fine
    jne __goclib_strcmp_diff
    cmp rax, 0
    je __goclib_strcmp_eq
    inc rcx
    inc rdx
    jmp __goclib_strcmp_loop
__goclib_strcmp_diff:
    sub rax, rbx
    pop rbx
    ret
__goclib_strcmp_eq:
    xor rax, rax
    pop rbx
    ret
; @end

; @func strcat
section .text
strcat:
    mov r8, rcx                  ; remember dst
__goclib_strcat_scan:
    xor rax, rax
    mov al, [rcx]
    cmp rax, 0
    je __goclib_strcat_copy
    inc rcx
    jmp __goclib_strcat_scan
__goclib_strcat_copy:
    xor rax, rax
    mov al, [rdx]
    mov [rcx], al
    inc rcx
    inc rdx
    cmp rax, 0
    jne __goclib_strcat_copy
    mov rax, r8
    ret
; @end

; @func memset
section .text
memset:
    mov r9, rcx                  ; remember dst
__goclib_memset_loop:
    cmp r8, 0
    je __goclib_memset_done
    mov [rcx], dl
    inc rcx
    dec r8
    jmp __goclib_memset_loop
__goclib_memset_done:
    mov rax, r9
    ret
; @end

; @func memcpy
section .text
memcpy:
    mov r9, rcx                  ; remember dst
__goclib_memcpy_loop:
    cmp r8, 0
    je __goclib_memcpy_done
    xor rax, rax
    mov al, [rdx]
    mov [rcx], al
    inc rcx
    inc rdx
    dec r8
    jmp __goclib_memcpy_loop
__goclib_memcpy_done:
    mov rax, r9
    ret
; @end
; goclib/extra.asm -- additional <stdio.h>/<stdlib.h>/<string.h> functions.
;
; All implemented on top of kernel32, matching the rest of the Win goclib:
;   arguments rcx, rdx, r8, r9 ; 32-byte shadow space ; at a call RSP%16 == 0.
;   odd pushes + sub rsp,32 (two pushes + sub rsp,40) keep the stack aligned.

; --- getchar -------------------------------------------------------------
; @data getchar
section .data
__goclib_in    dq 0               ; stdin handle, lazily initialised
__goclib_inb   db 0               ; read scratch byte
__goclib_inr   dq 0               ; bytes-read scratch
; @end

; @func getchar
; @extern GetStdHandle ReadFile
section .text
getchar:
    push r12
    sub rsp, 32
    mov r12, [rip+__goclib_in]
    cmp r12, 0
    jne __goclib_gc_have
    mov rcx, -10                ; STD_INPUT_HANDLE
    call GetStdHandle
    mov [rip+__goclib_in], rax
__goclib_gc_have:
    mov rcx, [rip+__goclib_in]
    lea rdx, [rip+__goclib_inb]
    mov r8, 1
    lea r9, [rip+__goclib_inr]
    mov [rsp+32], 0             ; lpOverlapped = NULL
    call ReadFile
    mov rax, [rip+__goclib_inr]
    cmp rax, 1
    jne __goclib_gc_eof
    xor rax, rax
    mov al, [rip+__goclib_inb]
    add rsp, 32
    pop r12
    ret
__goclib_gc_eof:
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
    je __goclib_calloc_done
    mov rcx, rax
    mov rdx, 0
    mov r8, rbx
    call memset
__goclib_calloc_done:
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
__goclib_sl_ws:
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0x20
    je __goclib_sl_ws_adv
    cmp rdx, 0x09
    je __goclib_sl_ws_adv
    jmp __goclib_sl_sign
__goclib_sl_ws_adv:
    inc r10
    jmp __goclib_sl_ws
__goclib_sl_sign:
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0x2d               ; '-'
    jne __goclib_sl_sign_p
    mov rcx, 1
    inc r10
    jmp __goclib_sl_base
__goclib_sl_sign_p:
    cmp rdx, 0x2b               ; '+'
    jne __goclib_sl_base
    inc r10
__goclib_sl_base:
    cmp r11, 0
    jne __goclib_sl_parse
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0x30               ; '0'
    jne __goclib_sl_dec
    mov dl, [r10+1]
    cmp rdx, 0x78               ; 'x'
    je __goclib_sl_hex
    cmp rdx, 0x58               ; 'X'
    je __goclib_sl_hex
    mov r11, 8                  ; leading 0 (not 0x) => octal
    jmp __goclib_sl_parse
__goclib_sl_hex:
    mov r11, 16
    add r10, 2
    jmp __goclib_sl_parse
__goclib_sl_dec:
    mov r11, 10
__goclib_sl_parse:
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0
    je __goclib_sl_end
    cmp rdx, 0x30
    jl __goclib_sl_end
    cmp rdx, 0x39
    jg __goclib_sl_a1
    sub rdx, 0x30
    jmp __goclib_sl_check
__goclib_sl_a1:
    cmp rdx, 0x41               ; 'A'
    jl __goclib_sl_end
    cmp rdx, 0x5a               ; 'Z'
    jg __goclib_sl_a2
    sub rdx, 0x41
    add rdx, 10
    jmp __goclib_sl_check
__goclib_sl_a2:
    cmp rdx, 0x61               ; 'a'
    jl __goclib_sl_end
    cmp rdx, 0x7a               ; 'z'
    jg __goclib_sl_end
    sub rdx, 0x61
    add rdx, 10
__goclib_sl_check:
    cmp rdx, r11
    jge __goclib_sl_end
    mov r8, r11
    imul r8, rax                ; r8 = base * result
    add r8, rdx                 ; + digit
    mov rax, r8
    inc r10
    jmp __goclib_sl_parse
__goclib_sl_end:
    cmp rcx, 1
    jne __goclib_sl_store
    neg rax
__goclib_sl_store:
    cmp r9, 0
    je __goclib_sl_ret
    mov [r9], r10               ; *endp = first unconverted position
__goclib_sl_ret:
    ret
; @end

; --- rand / srand (LCG) ----------------------------------------------------
; @data rand,srand
section .data
__goclib_rand_state    dq 1
; @end

; @func rand
section .text
rand:
    mov rax, [rip+__goclib_rand_state]
    imul rax, 1103515245
    add rax, 12345
    mov [rip+__goclib_rand_state], rax
    shr rax, 16
    and rax, 0x7fff
    ret
; @end

; @func srand
section .text
srand:
    mov [rip+__goclib_rand_state], rcx
    ret
; @end

; --- strchr(s, c) ---------------------------------------------------------
; @func strchr
section .text
strchr:
    and rdx, 0xff                ; mask the search byte
__goclib_strchr_loop:
    xor rax, rax
    mov al, [rcx]
    cmp rax, rdx
    je __goclib_strchr_found
    cmp rax, 0
    je __goclib_strchr_notfound
    inc rcx
    jmp __goclib_strchr_loop
__goclib_strchr_found:
    mov rax, rcx
    ret
__goclib_strchr_notfound:
    xor rax, rax
    ret
; @end

; --- strncmp(s1, s2, n) ---------------------------------------------------
; @func strncmp
section .text
strncmp:
    mov r10, r8                 ; n
__goclib_strncmp_loop:
    cmp r10, 0
    je __goclib_strncmp_eq
    xor rax, rax
    mov al, [rcx]
    xor r9, r9
    mov r9b, [rdx]
    cmp rax, r9
    jne __goclib_strncmp_diff
    cmp rax, 0
    je __goclib_strncmp_eq
    inc rcx
    inc rdx
    dec r10
    jmp __goclib_strncmp_loop
__goclib_strncmp_diff:
    sub rax, r9
    ret
__goclib_strncmp_eq:
    xor rax, rax
    ret
; @end

; --- memcmp(s1, s2, n) ----------------------------------------------------
; @func memcmp
section .text
memcmp:
    mov r10, r8                 ; n
__goclib_memcmp_loop:
    cmp r10, 0
    je __goclib_memcmp_eq
    xor rax, rax
    mov al, [rcx]
    xor r9, r9
    mov r9b, [rdx]
    cmp rax, r9
    jne __goclib_memcmp_diff
    inc rcx
    inc rdx
    dec r10
    jmp __goclib_memcmp_loop
__goclib_memcmp_diff:
    sub rax, r9
    ret
__goclib_memcmp_eq:
    xor rax, rax
    ret
; @end

; --- memmove(dest, src, n) ------------------------------------------------
; @func memmove
section .text
memmove:
    cmp rcx, rdx
    jg __goclib_memmove_bwd
    mov r9, rcx                 ; dest
    mov r10, rdx                ; src
    mov r11, r8                 ; n
__goclib_memmove_fwd_loop:
    cmp r11, 0
    je __goclib_memmove_done
    xor rax, rax
    mov al, [r10]
    mov [r9], al
    inc r9
    inc r10
    dec r11
    jmp __goclib_memmove_fwd_loop
__goclib_memmove_bwd:
    mov r9, rcx
    add r9, r8
    mov r10, rdx
    add r10, r8
    mov r11, r8
__goclib_memmove_bwd_loop:
    cmp r11, 0
    je __goclib_memmove_done
    dec r9
    dec r10
    xor rax, rax
    mov al, [r10]
    mov [r9], al
    dec r11
    jmp __goclib_memmove_bwd_loop
__goclib_memmove_done:
    mov rax, rcx
    ret
; @end

#else
; goclib/stdio.asm -- <stdio.h> subset for Linux, raw syscalls only.
;
; SysV AMD64 argument order: rdi, rsi, rdx, rcx, r8, r9, then the stack at
; [rbp+16], [rbp+24], ... (there is no shadow space).
;
;   printf(fmt, ...)        rdi = fmt, varargs start at rsi
;   sprintf(dst, fmt, ...)  rdi = dst, rsi = fmt, varargs start at rdx
;
; goa turns `extern write` into a `mov rax,1; syscall; ret` stub, so there is
; no libc and no dynamic linker anywhere in the pipeline.

; @data __goclib_write
section .data
__goclib_bytes  dq 0               ; write() scratch (unused, kept for parity)
; @end

; @data putchar
section .data
__goclib_ch     db 0               ; putchar scratch
; @end

; @data puts
section .data
__goclib_nl     db 10, 0           ; puts() newline
; @end

; @data printf,sprintf
section .data
__goclib_va     dq 0, 0, 0, 0, 0, 0, 0, 0
__goclib_digits db 32 dup(0)       ; reversed digits while converting
__goclib_buf    db 512 dup(0)      ; printf() output buffer
__goclib_f10    dq 10.0            ; %f: fractional-digit scaling factor
__goclib_fneg   dq -0.0            ; %f: sign bit (0x8000000000000000)
; @end

; @func __goclib_write
; @extern write
section .text
__goclib_write:
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

; @func __goclib_vfmt
section .text
__goclib_vfmt:
    ; rdi = dst, rsi = fmt, rdx = va array, rcx = limit -> rax = chars written
    ; Supports %d %s %c %x %f %%. Stops as soon as the buffer is full.
    ; %f honours an optional ".precision": %f == %.6f, %.Nf prints N digits,
    ; %.0f prints none. Field width is parsed and ignored; output is truncated
    ; (no rounding), matching the existing default-precision behaviour.
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
    jg __goclib_vfmt_loop
    jmp __goclib_vfmt_done
__goclib_vfmt_loop:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0
    je __goclib_vfmt_done
    cmp rax, 0x25                ; '%'
    je __goclib_vfmt_pct
    mov [r15+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_loop
__goclib_vfmt_pct:
    inc rsi
    ; Parse optional field width (ignored), then ".precision", then any length
    ; modifier, so %f / %.6f / %.15f / %10.2f all reach the specifier with r9
    ; holding the fractional-digit count (default 6 when no '.' appears).
    mov r9, 6                    ; default precision
__goclib_vfmt_pf_width:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0x30
    jb __goclib_vfmt_pf_dot
    cmp rax, 0x39
    ja __goclib_vfmt_pf_dot
    inc rsi                      ; skip a width digit (width unsupported)
    jmp __goclib_vfmt_pf_width
__goclib_vfmt_pf_dot:
    cmp rax, 0x2e                ; '.'
    jne __goclib_vfmt_pf_len
    inc rsi
    xor r9, r9                   ; '.' seen: reset precision accumulator
__goclib_vfmt_pf_prec:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0x30
    jb __goclib_vfmt_pf_len
    cmp rax, 0x39
    ja __goclib_vfmt_pf_len
    imul r9, 10
    add r9, rax
    sub r9, 0x30
    inc rsi
    jmp __goclib_vfmt_pf_prec
__goclib_vfmt_pf_len:
    cmp rax, 0x6c                ; 'l'
    je __goclib_vfmt_pf_skip_len
    cmp rax, 0x4c                ; 'L'
    je __goclib_vfmt_pf_skip_len
    cmp rax, 0x68                ; 'h'
    je __goclib_vfmt_pf_skip_len
    jmp __goclib_vfmt_pf_done
__goclib_vfmt_pf_skip_len:
    inc rsi
    xor rax, rax
    mov al, [rsi]
    jmp __goclib_vfmt_pf_len
__goclib_vfmt_pf_done:
    xor rax, rax
    mov al, [rsi]
    cmp rax, 0                   ; trailing '%' -> stop
    je __goclib_vfmt_done
    cmp rax, 0x64                ; 'd'
    je __goclib_vfmt_num
    cmp rax, 0x73                ; 's'
    je __goclib_vfmt_str
    cmp rax, 0x63                ; 'c'
    je __goclib_vfmt_chr
    cmp rax, 0x78                ; 'x'
    je __goclib_vfmt_hex
    cmp rax, 0x66                ; 'f'
    je __goclib_vfmt_flt
    cmp rax, 0x75                ; 'u' (also %lu / %llu / %hu)
    je __goclib_vfmt_uns
    cmp rax, 0x25                ; '%'
    je __goclib_vfmt_esc
    mov bl, 0x25                 ; unknown spec: emit it verbatim
    mov [r15+r12], bl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    mov [r15+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_loop
__goclib_vfmt_esc:
    mov [r15+r12], al
    inc r12
    inc rsi
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_loop
__goclib_vfmt_chr:
    mov rbx, [r13]
    add r13, 8
    mov [r15+r12], bl
    inc r12
    inc rsi
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_loop
__goclib_vfmt_str:
    mov rbx, [r13]
    add r13, 8
__goclib_vfmt_strl:
    xor rax, rax
    mov al, [rbx]
    cmp rax, 0
    je __goclib_vfmt_next
    mov [r15+r12], al
    inc r12
    inc rbx
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_strl
__goclib_vfmt_num:
    mov rax, [r13]
    add r13, 8
    cmp rax, 0
    jge __goclib_vfmt_pos
    neg rax
    mov bl, 0x2d                 ; '-'
    mov [r15+r12], bl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
__goclib_vfmt_pos:
    lea rbx, [rip+__goclib_digits]
    xor rcx, rcx                 ; digit count
__goclib_vfmt_dv:
    xor rdx, rdx
    mov r11, 10
    idiv r11                     ; rax = quotient, rdx = remainder
    add rdx, 0x30
    mov [rbx+rcx], dl
    inc rcx
    cmp rax, 0
    jne __goclib_vfmt_dv
    jmp __goclib_vfmt_em
__goclib_vfmt_uns:
    ; %u / %lu / %llu / %hu: the va slot is always 8 bytes, print unsigned.
    ; Must use unsigned div (not idiv) or values with the high bit set get
    ; treated as negative.
    mov rax, [r13]
    add r13, 8
    lea rbx, [rip+__goclib_digits]
    xor rcx, rcx                 ; digit count
__goclib_vfmt_udv:
    xor rdx, rdx
    mov r11, 10
    div r11                      ; unsigned division
    add rdx, 0x30
    mov [rbx+rcx], dl
    inc rcx
    cmp rax, 0
    jne __goclib_vfmt_udv
    jmp __goclib_vfmt_em
__goclib_vfmt_hex:
    mov rax, [r13]
    add r13, 8
    lea rbx, [rip+__goclib_digits]
    xor rcx, rcx
__goclib_vfmt_hv:
    xor rdx, rdx
    mov r11, 16
    idiv r11
    cmp rdx, 10
    jge __goclib_vfmt_halpha
    add rdx, 0x30                ; '0'..'9'
    jmp __goclib_vfmt_hsto
__goclib_vfmt_halpha:
    add rdx, 0x57                ; 'a'..'f'
__goclib_vfmt_hsto:
    mov [rbx+rcx], dl
    inc rcx
    cmp rax, 0
    jne __goclib_vfmt_hv
    jmp __goclib_vfmt_em          ; do not fall through into the %f handler
__goclib_vfmt_flt:
    ; 8-byte va slot holds the IEEE-754 bits of the double
    mov rax, [r13]
    add r13, 8
    movq xmm0, rax
    cmp rax, 0                   ; sign bit set?
    jl __goclib_vfmt_flt_neg
__goclib_vfmt_flt_pos:
    cvttsd2si rax, xmm0          ; integer part (truncated toward zero)
    cvtsi2sd xmm1, rax
    subsd xmm0, xmm1             ; xmm0 = fractional part, 0 <= frac < 1
    lea rbx, [rip+__goclib_digits]
    xor rcx, rcx                 ; digit count
__goclib_vfmt_flt_dv:
    xor rdx, rdx
    mov r11, 10
    idiv r11
    add rdx, 0x30
    mov [rbx+rcx], dl
    inc rcx
    cmp rax, 0
    jne __goclib_vfmt_flt_dv
__goclib_vfmt_flt_em:
    cmp rcx, 0
    je __goclib_vfmt_flt_dot
    dec rcx
    mov dl, [rbx+rcx]
    mov [r15+r12], dl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_flt_em
__goclib_vfmt_flt_dot:
    cmp r9, 0
    je __goclib_vfmt_flt_nodot
    mov bl, 0x2e                 ; '.'
    mov [r15+r12], bl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
__goclib_vfmt_flt_nodot:
    jmp __goclib_vfmt_flt_fr
__goclib_vfmt_flt_fr:
    cmp r9, 0                    ; no more fractional digits?
    je __goclib_vfmt_next
    dec r9
    mulsd xmm0, [rip+__goclib_f10]
    cvttsd2si rcx, xmm0          ; next digit
    cvtsi2sd xmm1, rcx
    subsd xmm0, xmm1
    mov rdx, 0x30
    add rdx, rcx
    mov [r15+r12], dl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_flt_fr
__goclib_vfmt_flt_neg:
    mov bl, 0x2d                 ; '-'
    mov [r15+r12], bl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    mov rax, [rip+__goclib_fneg]   ; clear the sign bit: |x| = x xor sign
    movq xmm1, rax
    xorpd xmm0, xmm1
    jmp __goclib_vfmt_flt_pos
__goclib_vfmt_em:
    cmp rcx, 0
    je __goclib_vfmt_next
    dec rcx
    mov dl, [rbx+rcx]
    mov [r15+r12], dl
    inc r12
    cmp r12, r14
    jge __goclib_vfmt_done
    jmp __goclib_vfmt_em
__goclib_vfmt_next:
    inc rsi
    jmp __goclib_vfmt_loop
__goclib_vfmt_done:
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
; @deps __goclib_vfmt __goclib_write
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
    lea rbx, [rip+__goclib_va]
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
    lea rdi, [rip+__goclib_buf]
    mov rsi, r13
    lea rdx, [rip+__goclib_va]
    mov rcx, 512
    call __goclib_vfmt
    mov r12, rax
    lea rdi, [rip+__goclib_buf]
    mov rsi, r12
    call __goclib_write
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
; @deps __goclib_vfmt
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
    lea rbx, [rip+__goclib_va]
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
    lea rdx, [rip+__goclib_va]
    mov rcx, 0x7fffffff
    call __goclib_vfmt
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
; @deps __goclib_write strlen
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
    call __goclib_write
    lea rdi, [rip+__goclib_nl]
    mov rsi, 1
    call __goclib_write
    mov rax, r13
    add rsp, 8
    pop r13
    pop r12
    ret
; @end

; @func putchar
; @deps __goclib_write
section .text
putchar:
    ; rdi = character
    push r12
    sub rsp, 8
    mov r12, rdi
    mov [rip+__goclib_ch], dil
    lea rdi, [rip+__goclib_ch]
    mov rsi, 1
    call __goclib_write
    mov rax, r12
    add rsp, 8
    pop r12
    ret
; @end
; goclib/stdlib.asm -- <stdlib.h> subset for Linux.
;
; malloc grows the program break with the brk syscall (goa turns
; `extern brk` into `mov rax,12; syscall; ret`). free is a no-op: this is a
; bump allocator, which is honest for a toy libc -- the process gives
; everything back at exit anyway.

; @func __goclib_exit
; @extern exit
section .text
; NOTE: this is deliberately not named `exit`. On an ELF target goa defines a
; syscall stub with the extern's own name, so a goclib function called `exit`
; would overwrite that stub's symbol and recurse forever. goc maps the C name
; `exit` to this symbol (see goclibAlias).
__goclib_exit:
    sub rsp, 8                   ; keep the stack 16-aligned across the call
    call exit                    ; rdi already holds the status
    ret                          ; never reached
; @end

; --- platform primitives carved out for the C version (goclib/goclib.c) ---------
; These mirror the inline heap/IO logic already used by malloc/getchar but are
; exposed under the __goclib_ prefix so goclib/goclib.c can call them directly.

; @data __goclib_heap_alloc,malloc
section .data
__goclib_brk    dq 0              ; cached program break (0 = not initialised)
; @end

; @func __goclib_heap_alloc
; @extern brk
section .text
__goclib_heap_alloc:
    ; rdi = size -> rax = pointer (or 0 on failure)
    push rbx
    push r12
    sub rsp, 8
    mov rbx, rdi                 ; size
    mov rax, [rip+__goclib_brk]
    cmp rax, 0
    jne __goclib_ha_have
    xor rdi, rdi
    call brk                     ; brk(0) -> current break
    mov [rip+__goclib_brk], rax
__goclib_ha_have:
    mov r12, [rip+__goclib_brk]    ; this block starts here
    mov rax, r12
    add rax, rbx                 ; new break
    add rax, 15
    and rax, -16                 ; keep blocks 16-byte aligned
    mov rdi, rax
    call brk
    cmp rax, 0
    jl __goclib_ha_fail
    mov [rip+__goclib_brk], rax
    mov rax, r12
    jmp __goclib_ha_done
__goclib_ha_fail:
    xor rax, rax
__goclib_ha_done:
    add rsp, 8
    pop r12
    pop rbx
    ret
; @end

; @func __goclib_heap_free
section .text
__goclib_heap_free:
    ; Bump allocator: nothing to reclaim.
    ret
; @end

; @func __goclib_read
; @extern read
section .text
__goclib_read:
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
    mov rax, [rip+__goclib_brk]
    cmp rax, 0
    jne __goclib_malloc_have
    xor rdi, rdi
    call brk                     ; brk(0) -> current break
    mov [rip+__goclib_brk], rax
__goclib_malloc_have:
    mov r12, [rip+__goclib_brk]    ; this block starts here
    mov rax, r12
    add rax, rbx                 ; new break
    add rax, 15
    and rax, -16                 ; keep blocks 16-byte aligned
    mov rdi, rax
    call brk
    cmp rax, 0
    jl __goclib_malloc_fail
    mov [rip+__goclib_brk], rax
    mov rax, r12
    jmp __goclib_malloc_done
__goclib_malloc_fail:
    xor rax, rax
__goclib_malloc_done:
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
    jne __goclib_atoi_loop
    mov r8, 1
    inc rdi
__goclib_atoi_loop:
    xor rdx, rdx
    mov dl, [rdi]
    cmp rdx, 0
    je __goclib_atoi_done
    cmp rdx, 0x30
    jl __goclib_atoi_done          ; stop at the first non-digit
    cmp rdx, 0x39
    jg __goclib_atoi_done
    sub rdx, 0x30
    mov r9, 10
    imul rax, r9
    add rax, rdx
    inc rdi
    jmp __goclib_atoi_loop
__goclib_atoi_done:
    cmp r8, 0
    je __goclib_atoi_pos
    neg rax
__goclib_atoi_pos:
    ret
; @end

; @func abs
section .text
abs:
    mov rax, rdi
    cmp rax, 0
    jge __goclib_abs_pos
    neg rax
__goclib_abs_pos:
    ret
; @end
; goclib/string.asm -- <string.h> subset, SysV argument order
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
__goclib_strlen_loop:
    xor rdx, rdx
    mov dl, [rdi+rax]
    cmp rdx, 0
    je __goclib_strlen_done
    inc rax
    jmp __goclib_strlen_loop
__goclib_strlen_done:
    ret
; @end

; @func strcpy
section .text
strcpy:
    mov r8, rdi                  ; remember dst
__goclib_strcpy_loop:
    xor rax, rax
    mov al, [rsi]
    mov [rdi], al                ; copies the NUL too
    inc rdi
    inc rsi
    cmp rax, 0
    jne __goclib_strcpy_loop
    mov rax, r8
    ret
; @end

; @func strcmp
section .text
strcmp:
    push rbx
__goclib_strcmp_loop:
    xor rax, rax
    mov al, [rdi]
    xor rbx, rbx
    mov bl, [rsi]
    cmp rax, rbx                 ; both zero-extended: full-width compare is fine
    jne __goclib_strcmp_diff
    cmp rax, 0
    je __goclib_strcmp_eq
    inc rdi
    inc rsi
    jmp __goclib_strcmp_loop
__goclib_strcmp_diff:
    sub rax, rbx
    pop rbx
    ret
__goclib_strcmp_eq:
    xor rax, rax
    pop rbx
    ret
; @end

; @func strcat
section .text
strcat:
    mov r8, rdi                  ; remember dst
__goclib_strcat_scan:
    xor rax, rax
    mov al, [rdi]
    cmp rax, 0
    je __goclib_strcat_copy
    inc rdi
    jmp __goclib_strcat_scan
__goclib_strcat_copy:
    xor rax, rax
    mov al, [rsi]
    mov [rdi], al
    inc rdi
    inc rsi
    cmp rax, 0
    jne __goclib_strcat_copy
    mov rax, r8
    ret
; @end

; @func memset
section .text
memset:
    mov r9, rdi                  ; remember dst
__goclib_memset_loop:
    cmp rdx, 0
    je __goclib_memset_done
    mov [rdi], sil
    inc rdi
    dec rdx
    jmp __goclib_memset_loop
__goclib_memset_done:
    mov rax, r9
    ret
; @end

; @func memcpy
section .text
memcpy:
    mov r9, rdi                  ; remember dst
__goclib_memcpy_loop:
    cmp rdx, 0
    je __goclib_memcpy_done
    xor rax, rax
    mov al, [rsi]
    mov [rdi], al
    inc rdi
    inc rsi
    dec rdx
    jmp __goclib_memcpy_loop
__goclib_memcpy_done:
    mov rax, r9
    ret
; @end
; goclib/extra.asm -- additional <stdio.h>/<stdlib.h>/<string.h> functions.
;
; Raw syscalls only (goa turns extern read into a stub), SysV AMD64 conventions:
;   arguments rdi, rsi, rdx, rcx, r8, r9 ; no shadow space ; at a call RSP%16 == 0.
;   zero pushes + sub rsp,8 (one push + sub rsp,16) keep the stack aligned.

; --- getchar -------------------------------------------------------------
; @data getchar
section .data
__goclib_inb   db 0
; @end

; @func getchar
; @extern read
section .text
getchar:
    sub rsp, 8
    mov rdi, 0                  ; fd = stdin
    lea rsi, [rip+__goclib_inb]
    mov rdx, 1
    call read
    cmp rax, 1
    jne __goclib_gc_eof
    xor rax, rax
    mov al, [rip+__goclib_inb]
    add rsp, 8
    ret
__goclib_gc_eof:
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
    je __goclib_calloc_done
    mov rdi, rax
    mov rsi, 0
    mov rdx, rbx
    call memset
__goclib_calloc_done:
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
__goclib_sl_ws:
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0x20
    je __goclib_sl_ws_adv
    cmp rdx, 0x09
    je __goclib_sl_ws_adv
    jmp __goclib_sl_sign
__goclib_sl_ws_adv:
    inc r10
    jmp __goclib_sl_ws
__goclib_sl_sign:
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0x2d               ; '-'
    jne __goclib_sl_sign_p
    mov rcx, 1
    inc r10
    jmp __goclib_sl_base
__goclib_sl_sign_p:
    cmp rdx, 0x2b               ; '+'
    jne __goclib_sl_base
    inc r10
__goclib_sl_base:
    cmp r11, 0
    jne __goclib_sl_parse
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0x30               ; '0'
    jne __goclib_sl_dec
    mov dl, [r10+1]
    cmp rdx, 0x78               ; 'x'
    je __goclib_sl_hex
    cmp rdx, 0x58               ; 'X'
    je __goclib_sl_hex
    mov r11, 8                  ; leading 0 (not 0x) => octal
    jmp __goclib_sl_parse
__goclib_sl_hex:
    mov r11, 16
    add r10, 2
    jmp __goclib_sl_parse
__goclib_sl_dec:
    mov r11, 10
__goclib_sl_parse:
    xor rdx, rdx
    mov dl, [r10]
    cmp rdx, 0
    je __goclib_sl_end
    cmp rdx, 0x30
    jl __goclib_sl_end
    cmp rdx, 0x39
    jg __goclib_sl_a1
    sub rdx, 0x30
    jmp __goclib_sl_check
__goclib_sl_a1:
    cmp rdx, 0x41               ; 'A'
    jl __goclib_sl_end
    cmp rdx, 0x5a               ; 'Z'
    jg __goclib_sl_a2
    sub rdx, 0x41
    add rdx, 10
    jmp __goclib_sl_check
__goclib_sl_a2:
    cmp rdx, 0x61               ; 'a'
    jl __goclib_sl_end
    cmp rdx, 0x7a               ; 'z'
    jg __goclib_sl_end
    sub rdx, 0x61
    add rdx, 10
__goclib_sl_check:
    cmp rdx, r11
    jge __goclib_sl_end
    mov r8, r11
    imul r8, rax                ; r8 = base * result
    add r8, rdx                 ; + digit
    mov rax, r8
    inc r10
    jmp __goclib_sl_parse
__goclib_sl_end:
    cmp rcx, 1
    jne __goclib_sl_store
    neg rax
__goclib_sl_store:
    cmp r9, 0
    je __goclib_sl_ret
    mov [r9], r10               ; *endp = first unconverted position
__goclib_sl_ret:
    ret
; @end

; --- rand / srand (LCG) ----------------------------------------------------
; @data rand,srand
section .data
__goclib_rand_state    dq 1
; @end

; @func rand
section .text
rand:
    mov rax, [rip+__goclib_rand_state]
    imul rax, 1103515245
    add rax, 12345
    mov [rip+__goclib_rand_state], rax
    shr rax, 16
    and rax, 0x7fff
    ret
; @end

; @func srand
section .text
srand:
    mov [rip+__goclib_rand_state], rdi
    ret
; @end

; --- strchr(s, c) ---------------------------------------------------------
; @func strchr
section .text
strchr:
    and rsi, 0xff               ; mask the search byte
__goclib_strchr_loop:
    xor rax, rax
    mov al, [rdi]
    cmp rax, rsi
    je __goclib_strchr_found
    cmp rax, 0
    je __goclib_strchr_notfound
    inc rdi
    jmp __goclib_strchr_loop
__goclib_strchr_found:
    mov rax, rdi
    ret
__goclib_strchr_notfound:
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
__goclib_strncmp_loop:
    cmp r10, 0
    je __goclib_strncmp_eq
    xor rax, rax
    mov al, [r8]
    xor r11, r11
    mov r11b, [r9]
    cmp rax, r11
    jne __goclib_strncmp_diff
    cmp rax, 0
    je __goclib_strncmp_eq
    inc r8
    inc r9
    dec r10
    jmp __goclib_strncmp_loop
__goclib_strncmp_diff:
    sub rax, r11
    ret
__goclib_strncmp_eq:
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
__goclib_memcmp_loop:
    cmp r10, 0
    je __goclib_memcmp_eq
    xor rax, rax
    mov al, [r8]
    xor r11, r11
    mov r11b, [r9]
    cmp rax, r11
    jne __goclib_memcmp_diff
    inc r8
    inc r9
    dec r10
    jmp __goclib_memcmp_loop
__goclib_memcmp_diff:
    sub rax, r11
    ret
__goclib_memcmp_eq:
    xor rax, rax
    ret
; @end

; --- memmove(dest, src, n) ------------------------------------------------
; @func memmove
section .text
memmove:
    cmp rdi, rsi
    jg __goclib_memmove_bwd
    mov r8, rdi                 ; dest
    mov r9, rsi                 ; src
    mov r10, rdx                ; n
__goclib_memmove_fwd_loop:
    cmp r10, 0
    je __goclib_memmove_done
    xor rax, rax
    mov al, [r9]
    mov [r8], al
    inc r8
    inc r9
    dec r10
    jmp __goclib_memmove_fwd_loop
__goclib_memmove_bwd:
    mov r8, rdi
    add r8, rdx
    mov r9, rsi
    add r9, rdx
    mov r10, rdx
__goclib_memmove_bwd_loop:
    cmp r10, 0
    je __goclib_memmove_done
    dec r8
    dec r9
    xor rax, rax
    mov al, [r9]
    mov [r8], al
    dec r10
    jmp __goclib_memmove_bwd_loop
__goclib_memmove_done:
    mov rax, rdi
    ret
; @end

#endif
