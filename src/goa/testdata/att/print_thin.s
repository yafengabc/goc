	.def	@feat.00;
	.scl	3;
	.type	0;
	.endef
	.globl	@feat.00
@feat.00 = 0
	.att_syntax
	.file	"goc.ll"
	.def	main;
	.scl	2;
	.type	32;
	.endef
	.text
	.globl	main
	.p2align	4
main:
.seh_proc main
	pushq	%rbp
	.seh_pushreg %rbp
	pushq	%rsi
	.seh_pushreg %rsi
	subq	$40, %rsp
	.seh_stackalloc 40
	leaq	32(%rsp), %rbp
	.seh_setframe %rbp, 32
	.seh_endprologue
	callq	__main
	leaq	.str.0(%rip), %rcx
	callq	str_print
	movl	$42, %ecx
	callq	long_print
	leaq	.str.1(%rip), %rsi
	movq	%rsi, %rcx
	callq	str_print
	movabsq	$-1234567890123456789, %rcx
	callq	long_print
	movl	$8, %ecx
	callq	long_print
	movq	%rsi, %rcx
	callq	str_print
	movabsq	$9223372036854775807, %rcx
	callq	long_print
	movl	$65, %ecx
	callq	long_print
	movq	$-1, %rcx
	callq	long_print
	movq	%rsi, %rcx
	callq	str_print
	xorl	%eax, %eax
	.seh_startepilogue
	addq	$40, %rsp
	popq	%rsi
	popq	%rbp
	.seh_endepilogue
	retq
	.seh_endproc

	.def	__goclib_write;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_write
	.p2align	4
__goclib_write:
.seh_proc __goclib_write
	pushq	%rsi
	.seh_pushreg %rsi
	pushq	%rdi
	.seh_pushreg %rdi
	pushq	%rbx
	.seh_pushreg %rbx
	subq	$48, %rsp
	.seh_stackalloc 48
	.seh_endprologue
	movq	%rdx, %rdi
	movq	%rcx, %rbx
	movq	$0, 40(%rsp)
	movl	$-11, %ecx
	callq	GetStdHandle
	movq	$-1, %rsi
	testq	%rax, %rax
	je	.LBB1_4
	testq	%rdi, %rdi
	jle	.LBB1_3
	movq	$0, 32(%rsp)
	leaq	40(%rsp), %r9
	movq	%rax, %rcx
	movq	%rbx, %rdx
	movl	%edi, %r8d
	callq	WriteFile
	testl	%eax, %eax
	je	.LBB1_4
.LBB1_3:
	movq	40(%rsp), %rsi
.LBB1_4:
	movq	%rsi, %rax
	.seh_startepilogue
	addq	$48, %rsp
	popq	%rbx
	popq	%rdi
	popq	%rsi
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

	.def	__goclib_long_to_buf;
	.scl	2;
	.type	32;
	.endef
	.globl	__goclib_long_to_buf
	.p2align	4
__goclib_long_to_buf:
	movq	%rdx, %r9
	xorl	%r8d, %r8d
	testq	%rdx, %rdx
	jns	.LBB3_2
	negq	%r9
	movb	$45, (%rcx)
	movl	$1, %r8d
.LBB3_2:
	movabsq	$-3689348814741910323, %r10
	.p2align	4
.LBB3_3:
	movslq	%r8d, %r8
	movq	%r9, %rax
	mulq	%r10
	shrq	$3, %rdx
	leal	(%rdx,%rdx), %eax
	leal	(%rax,%rax,4), %eax
	movl	%r9d, %r11d
	subl	%eax, %r11d
	orb	$48, %r11b
	movb	%r11b, (%r8,%rcx)
	incl	%r8d
	cmpq	$9, %r9
	movq	%rdx, %r9
	ja	.LBB3_3
	movzbl	(%rcx), %edx
	xorl	%eax, %eax
	cmpb	$45, %dl
	sete	%al
	leal	-1(%r8), %r9d
	cmpl	%eax, %r9d
	jle	.LBB3_7
	xorl	%eax, %eax
	cmpb	$45, %dl
	sete	%al
	leaq	(%rcx,%rax), %rdx
	movslq	%r9d, %r9
	addq	%r9, %rcx
	decq	%r9
	.p2align	4
.LBB3_6:
	movzbl	(%rdx), %r10d
	movzbl	(%rcx), %r11d
	movb	%r11b, (%rdx)
	movb	%r10b, (%rcx)
	incq	%rax
	incq	%rdx
	decq	%rcx
	cmpq	%r9, %rax
	leaq	-1(%r9), %r9
	jl	.LBB3_6
.LBB3_7:
	movl	%r8d, %eax
	retq

	.def	long_print;
	.scl	2;
	.type	32;
	.endef
	.globl	long_print
	.p2align	4
long_print:
.seh_proc long_print
	pushq	%rsi
	.seh_pushreg %rsi
	pushq	%rdi
	.seh_pushreg %rdi
	subq	$72, %rsp
	.seh_stackalloc 72
	.seh_endprologue
	xorl	%esi, %esi
	testq	%rcx, %rcx
	jns	.LBB4_2
	negq	%rcx
	movb	$45, 51(%rsp)
	movl	$1, %esi
.LBB4_2:
	movabsq	$-3689348814741910323, %r9
	leaq	51(%rsp), %r10
	.p2align	4
.LBB4_3:
	movslq	%esi, %r8
	movq	%rcx, %rax
	mulq	%r9
	shrq	$3, %rdx
	leal	(%rdx,%rdx), %eax
	leal	(%rax,%rax,4), %eax
	movl	%ecx, %r11d
	subl	%eax, %r11d
	orb	$48, %r11b
	movb	%r11b, (%r8,%r10)
	leal	1(%r8), %esi
	cmpq	$9, %rcx
	movq	%rdx, %rcx
	ja	.LBB4_3
	movzbl	51(%rsp), %ecx
	xorl	%eax, %eax
	cmpb	$45, %cl
	sete	%al
	leal	-1(%rsi), %edi
	cmpl	%eax, %edi
	jle	.LBB4_7
	xorl	%eax, %eax
	cmpb	$45, %cl
	sete	%al
	.p2align	4
.LBB4_6:
	movzbl	51(%rsp,%rax), %ecx
	movzbl	51(%rsp,%r8), %edx
	movb	%dl, 51(%rsp,%rax)
	movb	%cl, 51(%rsp,%r8)
	incq	%rax
	decq	%r8
	cmpq	%r8, %rax
	jl	.LBB4_6
.LBB4_7:
	movq	$0, 40(%rsp)
	movl	$-11, %ecx
	callq	GetStdHandle
	testq	%rax, %rax
	sete	%cl
	cmpl	$2147483647, %edi
	setae	%dl
	orb	%cl, %dl
	jne	.LBB4_9
	movq	$0, 32(%rsp)
	leaq	51(%rsp), %rdx
	leaq	40(%rsp), %r9
	movq	%rax, %rcx
	movl	%esi, %r8d
	callq	WriteFile
.LBB4_9:
	movq	$0, 40(%rsp)
	movl	$-11, %ecx
	callq	GetStdHandle
	testq	%rax, %rax
	je	.LBB4_11
	movq	$0, 32(%rsp)
	leaq	.str.2(%rip), %rdx
	leaq	40(%rsp), %r9
	movq	%rax, %rcx
	movl	$1, %r8d
	callq	WriteFile
.LBB4_11:
	incl	%esi
	movl	%esi, %eax
	.seh_startepilogue
	addq	$72, %rsp
	popq	%rdi
	popq	%rsi
	.seh_endepilogue
	retq
	.seh_endproc

	.def	int_print;
	.scl	2;
	.type	32;
	.endef
	.globl	int_print
	.p2align	4
int_print:
	movslq	%ecx, %rcx
	jmp	long_print

	.def	str_print;
	.scl	2;
	.type	32;
	.endef
	.globl	str_print
	.p2align	4
str_print:
.seh_proc str_print
	pushq	%rsi
	.seh_pushreg %rsi
	pushq	%rdi
	.seh_pushreg %rdi
	subq	$56, %rsp
	.seh_stackalloc 56
	.seh_endprologue
	testq	%rcx, %rcx
	leaq	.str.3(%rip), %rdi
	cmovneq	%rcx, %rdi
	xorl	%esi, %esi
	.p2align	4
.LBB6_1:
	cmpb	$0, (%rdi,%rsi)
	leaq	1(%rsi), %rsi
	jne	.LBB6_1
	movq	$0, 48(%rsp)
	movl	$-11, %ecx
	callq	GetStdHandle
	testq	%rax, %rax
	sete	%cl
	leaq	-1(%rsi), %rdx
	testq	%rdx, %rdx
	setle	%dl
	orb	%cl, %dl
	jne	.LBB6_4
	leal	-1(%rsi), %r8d
	movq	$0, 32(%rsp)
	leaq	48(%rsp), %r9
	movq	%rax, %rcx
	movq	%rdi, %rdx
	callq	WriteFile
.LBB6_4:
	movq	$0, 48(%rsp)
	movl	$-11, %ecx
	callq	GetStdHandle
	testq	%rax, %rax
	je	.LBB6_6
	movq	$0, 32(%rsp)
	leaq	.str.2(%rip), %rdx
	leaq	48(%rsp), %r9
	movq	%rax, %rcx
	movl	$1, %r8d
	callq	WriteFile
.LBB6_6:
	movl	%esi, %eax
	.seh_startepilogue
	addq	$56, %rsp
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
	.asciz	"hello world"

	.bss
	.globl	.str.1
.str.1:
	.zero	1

	.data
	.globl	.str.2
.str.2:
	.asciz	"\n"

	.globl	.str.3
.str.3:
	.asciz	"(null)"

