; clib_linux/stdio.asm -- <stdio.h> subset for Linux, raw syscalls only.
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
