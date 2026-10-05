	.def	@feat.00;
	.scl	3;
	.type	0;
	.endef
	.globl	@feat.00
@feat.00 = 0
	.att_syntax
	.file	"goc.ll"
	.def	putint;
	.scl	2;
	.type	32;
	.endef
	.text
	.globl	putint
	.p2align	4
putint:
.seh_proc putint
	pushq	%rsi
	.seh_pushreg %rsi
	subq	$32, %rsp
	.seh_stackalloc 32
	.seh_endprologue
	movl	%ecx, %esi
	testl	%ecx, %ecx
	js	.LBB0_1
	cmpl	$10, %esi
	jl	.LBB0_4
.LBB0_3:
	movl	%esi, %eax
	movl	$3435973837, %ecx
	imulq	%rax, %rcx
	shrq	$35, %rcx
	callq	putint
.LBB0_4:
	movslq	%esi, %rax
	imulq	$1717986919, %rax, %rax
	movq	%rax, %rcx
	shrq	$63, %rcx
	sarq	$34, %rax
	addl	%ecx, %eax
	addl	%eax, %eax
	leal	(%rax,%rax,4), %eax
	negl	%eax
	leal	48(%rsi,%rax), %esi
	callq	__goclib_init_streams
	leaq	G___goclib_stdout_file(%rip), %rdx
	movl	%esi, %ecx
	callq	fputc
	xorl	%eax, %eax
	.seh_startepilogue
	addq	$32, %rsp
	popq	%rsi
	.seh_endepilogue
	retq
.LBB0_1:
	callq	__goclib_init_streams
	leaq	G___goclib_stdout_file(%rip), %rdx
	movl	$45, %ecx
	callq	fputc
	negl	%esi
	cmpl	$10, %esi
	jge	.LBB0_3
	jmp	.LBB0_4
	.seh_endproc

	.def	my_printf;
	.scl	2;
	.type	32;
	.endef
	.globl	my_printf
	.p2align	4
my_printf:
.seh_proc my_printf
	pushq	%r15
	.seh_pushreg %r15
	pushq	%r14
	.seh_pushreg %r14
	pushq	%r12
	.seh_pushreg %r12
	pushq	%rsi
	.seh_pushreg %rsi
	pushq	%rdi
	.seh_pushreg %rdi
	pushq	%rbp
	.seh_pushreg %rbp
	pushq	%rbx
	.seh_pushreg %rbx
	subq	$64, %rsp
	.seh_stackalloc 64
	.seh_endprologue
	movq	%rcx, %rsi
	movq	%rdx, 136(%rsp)
	movq	%r8, 144(%rsp)
	movq	%r9, 152(%rsp)
	leaq	136(%rsp), %rax
	movq	%rax, 40(%rsp)
	xorl	%ebp, %ebp
	leaq	G___goclib_stdout_file(%rip), %rdi
	leaq	.LJTI1_0(%rip), %r14
	jmp	.LBB1_1
.LBB1_10:
	movq	40(%rsp), %rax
	movl	(%rax), %ecx
	addq	$8, %rax
	movq	%rax, 40(%rsp)
	callq	putint
	incl	%ebp
	.p2align	4
.LBB1_1:
	movslq	%ebp, %rax
	movzbl	(%rax,%rsi), %eax
	cmpl	$37, %eax
	je	.LBB1_4
	testl	%eax, %eax
	je	.LBB1_14
	movsbl	%al, %ebx
.LBB1_12:
	callq	__goclib_init_streams
	movl	%ebx, %ecx
	movq	%rdi, %rdx
	callq	fputc
.LBB1_13:
	incl	%ebp
	jmp	.LBB1_1
	.p2align	4
.LBB1_4:
	incl	%ebp
	movslq	%ebp, %rax
	movzbl	(%rax,%rsi), %eax
	leal	-99(%rax), %ecx
	cmpl	$16, %ecx
	ja	.LBB1_11
	movslq	(%r14,%rcx,4), %rcx
	addq	%r14, %rcx
	jmpq	*%rcx
.LBB1_9:
	movq	40(%rsp), %rax
	movl	(%rax), %ebx
	addq	$8, %rax
	movq	%rax, 40(%rsp)
	jmp	.LBB1_12
.LBB1_6:
	movq	40(%rsp), %rax
	movq	(%rax), %r15
	addq	$8, %rax
	movq	%rax, 40(%rsp)
	movzbl	(%r15), %eax
	testb	%al, %al
	je	.LBB1_13
	movl	$1, %r12d
	.p2align	4
.LBB1_8:
	movsbl	%al, %ebx
	callq	__goclib_init_streams
	movl	%ebx, %ecx
	movq	%rdi, %rdx
	callq	fputc
	movslq	%r12d, %r12
	movzbl	(%r12,%r15), %eax
	incl	%r12d
	testb	%al, %al
	jne	.LBB1_8
	jmp	.LBB1_13
.LBB1_11:
	movsbl	%al, %ebx
	jmp	.LBB1_12
.LBB1_14:
	xorl	%eax, %eax
	.seh_startepilogue
	addq	$64, %rsp
	popq	%rbx
	popq	%rbp
	popq	%rdi
	popq	%rsi
	popq	%r12
	popq	%r14
	popq	%r15
	.seh_endepilogue
	retq
	.section	.rdata,"dr"
	.p2align	2, 0x0
.LJTI1_0:
	.long	.LBB1_9-.LJTI1_0
	.long	.LBB1_10-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_10-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_11-.LJTI1_0
	.long	.LBB1_6-.LJTI1_0
	.text
	.seh_endproc

	.def	main;
	.scl	2;
	.type	32;
	.endef
	.globl	main
	.p2align	4
main:
.seh_proc main
	pushq	%rbp
	.seh_pushreg %rbp
	subq	$48, %rsp
	.seh_stackalloc 48
	leaq	48(%rsp), %rbp
	.seh_setframe %rbp, 48
	.seh_endprologue
	callq	__main
	movl	$-7, 40(%rsp)
	movq	$123456789, 32(%rsp)
	leaq	.str.0(%rip), %rcx
	leaq	.str.1(%rip), %rdx
	movl	$42, %r8d
	movl	$65, %r9d
	callq	my_printf
	leaq	.str.2(%rip), %rcx
	movl	$100, %edx
	movl	$200, %r8d
	movl	$300, %r9d
	callq	my_printf
	xorl	%eax, %eax
	.seh_startepilogue
	addq	$48, %rsp
	popq	%rbp
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_os_write_at;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_os_write_at
	.p2align	4
__goclib_os_write_at:
.seh_proc __goclib_os_write_at
	pushq	%r15
	.seh_pushreg %r15
	pushq	%r14
	.seh_pushreg %r14
	pushq	%rsi
	.seh_pushreg %rsi
	pushq	%rdi
	.seh_pushreg %rdi
	pushq	%rbx
	.seh_pushreg %rbx
	subq	$64, %rsp
	.seh_stackalloc 64
	.seh_endprologue
	movq	%r8, %rsi
	movq	%rdx, %rdi
	movq	%rcx, %rbx
	movq	$0, 56(%rsp)
	leaq	56(%rsp), %r8
	movl	%r9d, %edx
	xorl	%r9d, %r9d
	callq	SetFilePointer
	cmpl	$-1, %eax
	je	.LBB3_8
	xorl	%r14d, %r14d
	testq	%rsi, %rsi
	jle	.LBB3_9
	leaq	48(%rsp), %r15
	.p2align	4
.LBB3_3:
	movq	$0, 48(%rsp)
	movq	$0, 32(%rsp)
	movq	%rbx, %rcx
	movq	%rdi, %rdx
	movl	%esi, %r8d
	movq	%r15, %r9
	callq	WriteFile
	testl	%eax, %eax
	je	.LBB3_8
	movq	48(%rsp), %rax
	testq	%rax, %rax
	jle	.LBB3_8
	addq	%rax, %rdi
	addq	%rax, %r14
	subq	%rax, %rsi
	jg	.LBB3_3
	jmp	.LBB3_9
.LBB3_8:
	movq	$-1, %r14
.LBB3_9:
	movq	%r14, %rax
	.seh_startepilogue
	addq	$64, %rsp
	popq	%rbx
	popq	%rdi
	popq	%rsi
	popq	%r14
	popq	%r15
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_os_seek;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_os_seek
	.p2align	4
__goclib_os_seek:
.seh_proc __goclib_os_seek
	subq	$40, %rsp
	.seh_stackalloc 40
	.seh_endprologue
	movq	%r8, %r9
	movq	$0, 32(%rsp)
	leaq	32(%rsp), %r8
	callq	SetFilePointer
	cltq
	.seh_startepilogue
	addq	$40, %rsp
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_os_write_seq;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_os_write_seq
	.p2align	4
__goclib_os_write_seq:
.seh_proc __goclib_os_write_seq
	pushq	%r15
	.seh_pushreg %r15
	pushq	%r14
	.seh_pushreg %r14
	pushq	%rsi
	.seh_pushreg %rsi
	pushq	%rdi
	.seh_pushreg %rdi
	pushq	%rbx
	.seh_pushreg %rbx
	subq	$48, %rsp
	.seh_stackalloc 48
	.seh_endprologue
	testq	%r8, %r8
	jle	.LBB5_1
	movq	%r8, %rsi
	movq	%rdx, %rdi
	movq	%rcx, %rbx
	xorl	%r15d, %r15d
	leaq	40(%rsp), %r14
	.p2align	4
.LBB5_3:
	movq	$0, 40(%rsp)
	movq	$0, 32(%rsp)
	movq	%rbx, %rcx
	movq	%rdi, %rdx
	movl	%esi, %r8d
	movq	%r14, %r9
	callq	WriteFile
	movl	%eax, %ecx
	movq	$-1, %rax
	testl	%ecx, %ecx
	je	.LBB5_6
	movq	40(%rsp), %rcx
	testq	%rcx, %rcx
	jle	.LBB5_6
	addq	%rcx, %rdi
	addq	%rcx, %r15
	subq	%rcx, %rsi
	movq	%r15, %rax
	jg	.LBB5_3
	jmp	.LBB5_6
.LBB5_1:
	xorl	%eax, %eax
.LBB5_6:
	.seh_startepilogue
	addq	$48, %rsp
	popq	%rbx
	popq	%rdi
	popq	%rsi
	popq	%r14
	popq	%r15
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_file_write;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_file_write
	.p2align	4
__goclib_file_write:
.seh_proc __goclib_file_write
	pushq	%r15
	.seh_pushreg %r15
	pushq	%r14
	.seh_pushreg %r14
	pushq	%r12
	.seh_pushreg %r12
	pushq	%rsi
	.seh_pushreg %rsi
	pushq	%rdi
	.seh_pushreg %rdi
	pushq	%rbx
	.seh_pushreg %rbx
	subq	$56, %rsp
	.seh_stackalloc 56
	.seh_endprologue
	movq	%r8, %rsi
	movq	%rdx, %rdi
	movq	(%rcx), %rbx
	movq	64(%rcx), %rdx
	testq	%rdx, %rdx
	js	.LBB6_7
	movq	%rcx, %r15
	movq	$0, 40(%rsp)
	leaq	40(%rsp), %r8
	movq	%rbx, %rcx
	xorl	%r9d, %r9d
	callq	SetFilePointer
	cmpl	$-1, %eax
	je	.LBB6_12
	xorl	%r14d, %r14d
	testq	%rsi, %rsi
	jle	.LBB6_15
	leaq	48(%rsp), %r12
	.p2align	4
.LBB6_4:
	movq	$0, 48(%rsp)
	movq	$0, 32(%rsp)
	movq	%rbx, %rcx
	movq	%rdi, %rdx
	movl	%esi, %r8d
	movq	%r12, %r9
	callq	WriteFile
	testl	%eax, %eax
	je	.LBB6_14
	movq	48(%rsp), %rax
	testq	%rax, %rax
	jle	.LBB6_12
	addq	%rax, %rdi
	addq	%rax, %r14
	subq	%rax, %rsi
	jg	.LBB6_4
.LBB6_15:
	testq	%r14, %r14
	jg	.LBB6_16
	jmp	.LBB6_17
.LBB6_7:
	testq	%rsi, %rsi
	jle	.LBB6_13
	xorl	%r12d, %r12d
	leaq	40(%rsp), %r15
	.p2align	4
.LBB6_9:
	movq	$0, 40(%rsp)
	movq	$0, 32(%rsp)
	movq	%rbx, %rcx
	movq	%rdi, %rdx
	movl	%esi, %r8d
	movq	%r15, %r9
	callq	WriteFile
	movq	$-1, %r14
	testl	%eax, %eax
	je	.LBB6_17
	movq	40(%rsp), %rax
	testq	%rax, %rax
	jle	.LBB6_17
	addq	%rax, %rdi
	addq	%rax, %r12
	subq	%rax, %rsi
	movq	%r12, %r14
	jg	.LBB6_9
	jmp	.LBB6_17
.LBB6_12:
	movq	$-1, %r14
	testq	%r14, %r14
	jg	.LBB6_16
	jmp	.LBB6_17
.LBB6_13:
	xorl	%r14d, %r14d
	jmp	.LBB6_17
.LBB6_14:
	movq	$-1, %r14
	testq	%r14, %r14
	jle	.LBB6_17
.LBB6_16:
	addq	%r14, 64(%r15)
.LBB6_17:
	movq	%r14, %rax
	.seh_startepilogue
	addq	$56, %rsp
	popq	%rbx
	popq	%rdi
	popq	%rsi
	popq	%r12
	popq	%r14
	popq	%r15
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_file_flush;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_file_flush
	.p2align	4
__goclib_file_flush:
.seh_proc __goclib_file_flush
	pushq	%rsi
	.seh_pushreg %rsi
	subq	$48, %rsp
	.seh_stackalloc 48
	.seh_endprologue
	xorl	%eax, %eax
	cmpl	$0, 12(%rcx)
	je	.LBB7_7
	cmpq	$0, 48(%rcx)
	jle	.LBB7_7
	cmpq	$0, 32(%rcx)
	je	.LBB7_7
	cmpq	$0, 64(%rcx)
	js	.LBB7_9
	cmpl	$0, 16(%rcx)
	je	.LBB7_9
	movq	%rcx, %rsi
	movq	(%rcx), %rcx
	movq	$0, 40(%rsp)
	leaq	40(%rsp), %r8
	xorl	%edx, %edx
	movl	$2, %r9d
	callq	SetFilePointer
	testl	%eax, %eax
	js	.LBB7_6
	movl	%eax, %eax
	movq	%rsi, %rcx
	movq	%rax, 64(%rsi)
.LBB7_9:
	movq	32(%rcx), %rdx
	movq	48(%rcx), %r8
	movq	%rcx, %rsi
	callq	__goclib_file_write
	cmpq	48(%rsi), %rax
	jne	.LBB7_6
	movq	$0, 48(%rsi)
	xorl	%eax, %eax
	jmp	.LBB7_7
.LBB7_6:
	movl	$1, 24(%rsi)
	movl	$-1, %eax
.LBB7_7:
	.seh_startepilogue
	addq	$48, %rsp
	popq	%rsi
	.seh_endepilogue
	retq
	.seh_endproc

	.def	fputc;
	.scl	2;
	.type	32;
	.endef
	.globl	fputc
	.p2align	4
fputc:
.seh_proc fputc
	pushq	%rsi
	.seh_pushreg %rsi
	pushq	%rdi
	.seh_pushreg %rdi
	subq	$56, %rsp
	.seh_stackalloc 56
	.seh_endprologue
	movl	$-1, %eax
	testq	%rdx, %rdx
	je	.LBB8_3
	cmpl	$0, 12(%rdx)
	je	.LBB8_2
	movq	40(%rdx), %rax
	testq	%rax, %rax
	jle	.LBB8_17
	movq	48(%rdx), %r8
	cmpq	%rax, %r8
	setl	%al
	testq	%r8, %r8
	setle	%r8b
	orb	%al, %r8b
	jne	.LBB8_15
	cmpq	$0, 32(%rdx)
	je	.LBB8_15
	movl	%ecx, %esi
	cmpq	$0, 64(%rdx)
	js	.LBB8_12
	cmpl	$0, 16(%rdx)
	je	.LBB8_12
	movq	%rdx, %rdi
	movq	(%rdx), %rcx
	movq	$0, 48(%rsp)
	leaq	48(%rsp), %r8
	xorl	%edx, %edx
	movl	$2, %r9d
	callq	SetFilePointer
	testl	%eax, %eax
	js	.LBB8_10
	movl	%eax, %eax
	movq	%rdi, %rdx
	movq	%rax, 64(%rdi)
.LBB8_12:
	movq	32(%rdx), %rax
	movq	48(%rdx), %r8
	movq	%rdx, %rcx
	movq	%rdx, %rdi
	movq	%rax, %rdx
	callq	__goclib_file_write
	movq	%rdi, %rdx
	cmpq	48(%rdi), %rax
	jne	.LBB8_13
	movq	$0, 48(%rdx)
	movl	%esi, %ecx
.LBB8_15:
	movq	32(%rdx), %rax
	movq	48(%rdx), %r8
	leaq	1(%r8), %r9
	movq	%r9, 48(%rdx)
	movb	%cl, (%r8,%rax)
	jmp	.LBB8_16
.LBB8_2:
	movl	$1, 24(%rdx)
	jmp	.LBB8_3
.LBB8_17:
	movb	%cl, 47(%rsp)
	leaq	47(%rsp), %rax
	movl	$1, %r8d
	movl	%ecx, %edi
	movq	%rdx, %rsi
	movq	%rdx, %rcx
	movq	%rax, %rdx
	callq	__goclib_file_write
	movl	%edi, %ecx
	testq	%rax, %rax
	jle	.LBB8_18
.LBB8_16:
	movzbl	%cl, %eax
.LBB8_3:
	.seh_startepilogue
	addq	$56, %rsp
	popq	%rdi
	popq	%rsi
	.seh_endepilogue
	retq
.LBB8_18:
	movl	$1, 24(%rsi)
	movl	$-1, %eax
	jmp	.LBB8_3
.LBB8_13:
	movl	$1, 24(%rdx)
	movl	$-1, %eax
	jmp	.LBB8_3
.LBB8_10:
	movl	$1, 24(%rdi)
	movl	$-1, %eax
	jmp	.LBB8_3
	.seh_endproc

	.def	fflush;
	.scl	2;
	.type	32;
	.endef
	.globl	fflush
	.p2align	4
fflush:
.seh_proc fflush
	pushq	%rsi
	.seh_pushreg %rsi
	subq	$48, %rsp
	.seh_stackalloc 48
	.seh_endprologue
	testq	%rcx, %rcx
	je	.LBB9_12
	cmpl	$0, 12(%rcx)
	je	.LBB9_12
	cmpq	$0, 48(%rcx)
	jle	.LBB9_12
	cmpq	$0, 32(%rcx)
	je	.LBB9_12
	cmpq	$0, 64(%rcx)
	js	.LBB9_8
	cmpl	$0, 16(%rcx)
	je	.LBB9_8
	movq	%rcx, %rsi
	movq	(%rcx), %rcx
	movq	$0, 40(%rsp)
	leaq	40(%rsp), %r8
	xorl	%edx, %edx
	movl	$2, %r9d
	callq	SetFilePointer
	testl	%eax, %eax
	js	.LBB9_11
	movl	%eax, %eax
	movq	%rsi, %rcx
	movq	%rax, 64(%rsi)
.LBB9_8:
	movq	32(%rcx), %rdx
	movq	48(%rcx), %r8
	movq	%rcx, %rsi
	callq	__goclib_file_write
	cmpq	48(%rsi), %rax
	jne	.LBB9_11
	movq	$0, 48(%rsi)
	jmp	.LBB9_12
.LBB9_11:
	movl	$1, 24(%rsi)
.LBB9_12:
	xorl	%eax, %eax
	.seh_startepilogue
	addq	$48, %rsp
	popq	%rsi
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_init_streams;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_init_streams
	.p2align	4
__goclib_init_streams:
.seh_proc __goclib_init_streams
	subq	$40, %rsp
	.seh_stackalloc 40
	.seh_endprologue
	cmpl	$0, G___goclib_streams_inited(%rip)
	je	.LBB10_1
	.seh_startepilogue
	addq	$40, %rsp
	.seh_endepilogue
	retq
.LBB10_1:
	movl	$1, G___goclib_streams_inited(%rip)
	movl	$-10, %ecx
	callq	GetStdHandle
	movq	%rax, G___goclib_stdin_file(%rip)
	movl	$-11, %ecx
	callq	GetStdHandle
	movq	%rax, G___goclib_stdout_file(%rip)
	movl	$-12, %ecx
	callq	GetStdHandle
	movq	%rax, G___goclib_stderr_file(%rip)
	movq	$1, G___goclib_stdin_file+8(%rip)
	movabsq	$4294967296, %rax
	movq	%rax, G___goclib_stdout_file+8(%rip)
	movq	%rax, G___goclib_stderr_file+8(%rip)
	leaq	G___goclib_in_buf(%rip), %rax
	movq	%rax, G___goclib_stdin_file+32(%rip)
	movq	$4096, G___goclib_stdin_file+40(%rip)
	leaq	G___goclib_out_buf(%rip), %rax
	movq	%rax, G___goclib_stdout_file+32(%rip)
	movq	$0, G___goclib_stdout_file+40(%rip)
	leaq	G___goclib_err_buf(%rip), %rax
	movq	%rax, G___goclib_stderr_file+32(%rip)
	movq	$0, G___goclib_stderr_file+40(%rip)
	movl	$0, G___goclib_stdin_file+76(%rip)
	movl	$0, G___goclib_stdout_file+76(%rip)
	movl	$0, G___goclib_stderr_file+76(%rip)
	movq	$0, G___goclib_stdin_file+48(%rip)
	movq	$0, G___goclib_stdin_file+56(%rip)
	movq	$-1, G___goclib_stdin_file+64(%rip)
	movq	$0, G___goclib_stdout_file+48(%rip)
	movq	$0, G___goclib_stdout_file+56(%rip)
	movq	$-1, G___goclib_stdout_file+64(%rip)
	movq	$0, G___goclib_stderr_file+56(%rip)
	movq	$0, G___goclib_stderr_file+48(%rip)
	movq	$-1, G___goclib_stderr_file+64(%rip)
	movl	$-1, G___goclib_stdin_file+72(%rip)
	movl	$-1, G___goclib_stdout_file+72(%rip)
	movl	$-1, G___goclib_stderr_file+72(%rip)
	movq	$0, G___goclib_stdin_file+20(%rip)
	movq	$0, G___goclib_stdout_file+20(%rip)
	movq	$0, G___goclib_stderr_file+20(%rip)
	.seh_startepilogue
	addq	$40, %rsp
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_stdout;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_stdout
	.p2align	4
__goclib_stdout:
.seh_proc __goclib_stdout
	subq	$40, %rsp
	.seh_stackalloc 40
	.seh_endprologue
	callq	__goclib_init_streams
	leaq	G___goclib_stdout_file(%rip), %rax
	.seh_startepilogue
	addq	$40, %rsp
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_stderr;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_stderr
	.p2align	4
__goclib_stderr:
.seh_proc __goclib_stderr
	subq	$40, %rsp
	.seh_stackalloc 40
	.seh_endprologue
	callq	__goclib_init_streams
	leaq	G___goclib_stderr_file(%rip), %rax
	.seh_startepilogue
	addq	$40, %rsp
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_exit;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_exit
	.p2align	4
__goclib_exit:
	jmp	ExitProcess

	.def	putchar;
	.scl	2;
	.type	32;
	.endef
	.globl	putchar
	.p2align	4
putchar:
.seh_proc putchar
	pushq	%rsi
	.seh_pushreg %rsi
	subq	$32, %rsp
	.seh_stackalloc 32
	.seh_endprologue
	movl	%ecx, %esi
	callq	__goclib_init_streams
	leaq	G___goclib_stdout_file(%rip), %rdx
	movl	%esi, %ecx
	.seh_startepilogue
	addq	$32, %rsp
	popq	%rsi
	.seh_endepilogue
	jmp	fputc
	.seh_endproc

	.def	exit;
	.scl	2;
	.type	32;
	.endef
	.globl	exit
	.p2align	4
exit:
.seh_proc exit
	pushq	%rsi
	.seh_pushreg %rsi
	pushq	%rdi
	.seh_pushreg %rdi
	subq	$40, %rsp
	.seh_stackalloc 40
	.seh_endprologue
	movl	%ecx, %esi
	movl	G_atexit_n(%rip), %eax
	testl	%eax, %eax
	jle	.LBB15_3
	leaq	G_atexit_fns(%rip), %rdi
	.p2align	4
.LBB15_2:
	decl	%eax
	movq	(%rdi,%rax,8), %rcx
	movl	%eax, G_atexit_n(%rip)
	callq	*%rcx
	movl	G_atexit_n(%rip), %eax
	testl	%eax, %eax
	jg	.LBB15_2
.LBB15_3:
	callq	__goclib_init_streams
	cmpl	$0, G___goclib_stdout_file+12(%rip)
	je	.LBB15_14
	cmpq	$0, G___goclib_stdout_file+48(%rip)
	jle	.LBB15_14
	cmpq	$0, G___goclib_stdout_file+32(%rip)
	je	.LBB15_14
	cmpq	$0, G___goclib_stdout_file+64(%rip)
	js	.LBB15_10
	cmpl	$0, G___goclib_stdout_file+16(%rip)
	je	.LBB15_10
	movq	G___goclib_stdout_file(%rip), %rcx
	movq	$0, 32(%rsp)
	leaq	32(%rsp), %r8
	xorl	%edx, %edx
	movl	$2, %r9d
	callq	SetFilePointer
	testl	%eax, %eax
	js	.LBB15_13
	movl	%eax, %eax
	movq	%rax, G___goclib_stdout_file+64(%rip)
.LBB15_10:
	movq	G___goclib_stdout_file+32(%rip), %rdx
	leaq	G___goclib_stdout_file(%rip), %rcx
	movq	G___goclib_stdout_file+48(%rip), %r8
	callq	__goclib_file_write
	cmpq	G___goclib_stdout_file+48(%rip), %rax
	jne	.LBB15_13
	movq	$0, G___goclib_stdout_file+48(%rip)
	jmp	.LBB15_14
.LBB15_13:
	movl	$1, G___goclib_stdout_file+24(%rip)
.LBB15_14:
	callq	__goclib_init_streams
	cmpl	$0, G___goclib_stderr_file+12(%rip)
	je	.LBB15_25
	cmpq	$0, G___goclib_stderr_file+48(%rip)
	jle	.LBB15_25
	cmpq	$0, G___goclib_stderr_file+32(%rip)
	je	.LBB15_25
	cmpq	$0, G___goclib_stderr_file+64(%rip)
	js	.LBB15_21
	cmpl	$0, G___goclib_stderr_file+16(%rip)
	je	.LBB15_21
	movq	G___goclib_stderr_file(%rip), %rcx
	movq	$0, 32(%rsp)
	leaq	32(%rsp), %r8
	xorl	%edx, %edx
	movl	$2, %r9d
	callq	SetFilePointer
	testl	%eax, %eax
	js	.LBB15_24
	movl	%eax, %eax
	movq	%rax, G___goclib_stderr_file+64(%rip)
.LBB15_21:
	movq	G___goclib_stderr_file+32(%rip), %rdx
	leaq	G___goclib_stderr_file(%rip), %rcx
	movq	G___goclib_stderr_file+48(%rip), %r8
	callq	__goclib_file_write
	cmpq	G___goclib_stderr_file+48(%rip), %rax
	jne	.LBB15_24
	movq	$0, G___goclib_stderr_file+48(%rip)
	jmp	.LBB15_25
.LBB15_24:
	movl	$1, G___goclib_stderr_file+24(%rip)
.LBB15_25:
	movl	%esi, %ecx
	callq	ExitProcess
	nop
	.seh_startepilogue
	addq	$40, %rsp
	popq	%rdi
	popq	%rsi
	.seh_endepilogue
	retq
	.seh_endproc

	.bss
	.globl	G_bi_scr
	.p2align	3, 0x0
G_bi_scr:
	.zero	64

	.globl	G_bi_scrcap
	.p2align	3, 0x0
G_bi_scrcap:
	.zero	64

	.globl	G_bi_p10
	.p2align	3, 0x0
G_bi_p10:
	.zero	176

	.globl	G_bi_p10len
	.p2align	3, 0x0
G_bi_p10len:
	.zero	176

	.globl	G_bi_p10cnt
	.p2align	3, 0x0
G_bi_p10cnt:
	.quad	0

	.globl	G___goclib_errno_val
	.p2align	2, 0x0
G___goclib_errno_val:
	.long	0

	.globl	G___goclib_in_buf
G___goclib_in_buf:
	.zero	4096

	.globl	G___goclib_out_buf
G___goclib_out_buf:
	.zero	4096

	.globl	G___goclib_err_buf
G___goclib_err_buf:
	.zero	256

	.globl	G___goclib_stdin_file
	.p2align	3, 0x0
G___goclib_stdin_file:
	.zero	80

	.globl	G___goclib_stdout_file
	.p2align	3, 0x0
G___goclib_stdout_file:
	.zero	80

	.globl	G___goclib_stderr_file
	.p2align	3, 0x0
G___goclib_stderr_file:
	.zero	80

	.globl	G___goclib_streams_inited
	.p2align	2, 0x0
G___goclib_streams_inited:
	.long	0

	.data
	.globl	G_rand_state
	.p2align	3, 0x0
G_rand_state:
	.quad	1

	.bss
	.globl	G_atexit_fns
	.p2align	3, 0x0
G_atexit_fns:
	.zero	256

	.globl	G_atexit_n
	.p2align	2, 0x0
G_atexit_n:
	.long	0

	.globl	G_at_quick_exit_fns
	.p2align	3, 0x0
G_at_quick_exit_fns:
	.zero	256

	.globl	G_at_quick_exit_n
	.p2align	2, 0x0
G_at_quick_exit_n:
	.long	0

	.globl	G_envbuf
G_envbuf:
	.zero	1024

	.globl	G_tok_save
	.p2align	3, 0x0
G_tok_save:
	.quad	0

	.globl	G_tm_buf
	.p2align	3, 0x0
G_tm_buf:
	.zero	36

	.data
	.globl	.const.0
.const.0:
	.asciz	"UTC"

	.globl	G_tzname
	.p2align	3, 0x0
G_tzname:
	.quad	.const.0
	.quad	.const.0

	.bss
	.globl	G_timezone
	.p2align	3, 0x0
G_timezone:
	.quad	0

	.globl	G_daylight
	.p2align	3, 0x0
G_daylight:
	.quad	0

	.data
	.globl	.str.0
.str.0:
	.asciz	"hello %s count=%d char=%c big=%ld neg=%d\n"

	.globl	.str.1
.str.1:
	.asciz	"world"

	.globl	.str.2
.str.2:
	.asciz	"nums: %d %d %d\n"

