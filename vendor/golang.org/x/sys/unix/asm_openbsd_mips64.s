// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

<<<<<<< HEAD:vendor/golang.org/x/sys/unix/asm_openbsd_mips64.s
=======
//go:build gc
>>>>>>> 73054235125ff300ec7fee2a3c76a4e756cc9ebf:vendor/golang.org/x/sys/unix/asm_openbsd_mips64.s
// +build gc

#include "textflag.h"

//
// System call support for mips64, OpenBSD
//

// Just jump to package syscall's implementation for all these functions.
// The runtime may know about them.

TEXT	·Syscall(SB),NOSPLIT,$0-56
	JMP	syscall·Syscall(SB)

TEXT	·Syscall6(SB),NOSPLIT,$0-80
	JMP	syscall·Syscall6(SB)

TEXT	·Syscall9(SB),NOSPLIT,$0-104
	JMP	syscall·Syscall9(SB)

TEXT	·RawSyscall(SB),NOSPLIT,$0-56
	JMP	syscall·RawSyscall(SB)

TEXT	·RawSyscall6(SB),NOSPLIT,$0-80
	JMP	syscall·RawSyscall6(SB)
