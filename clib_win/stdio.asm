; clib_win/stdio.asm -- <stdio.h> subset, built on kernel32 WriteFile only.
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
    ; Supports %d %s %c %x %%. Stops as soon as the buffer is full.
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
