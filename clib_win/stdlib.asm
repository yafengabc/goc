; clib_win/stdlib.asm -- <stdlib.h> subset.
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
