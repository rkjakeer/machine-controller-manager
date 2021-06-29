// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

<<<<<<< HEAD
//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris
=======
//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || zos
// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris zos
>>>>>>> 73054235125ff300ec7fee2a3c76a4e756cc9ebf

package unix

// Set adds fd to the set fds.
func (fds *FdSet) Set(fd int) {
	fds.Bits[fd/NFDBITS] |= (1 << (uintptr(fd) % NFDBITS))
}

// Clear removes fd from the set fds.
func (fds *FdSet) Clear(fd int) {
	fds.Bits[fd/NFDBITS] &^= (1 << (uintptr(fd) % NFDBITS))
}

// IsSet returns whether fd is in the set fds.
func (fds *FdSet) IsSet(fd int) bool {
	return fds.Bits[fd/NFDBITS]&(1<<(uintptr(fd)%NFDBITS)) != 0
}

// Zero clears the set fds.
func (fds *FdSet) Zero() {
	for i := range fds.Bits {
		fds.Bits[i] = 0
	}
}
