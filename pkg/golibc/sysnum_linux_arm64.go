package golibc

import "syscall"

// The same syscall, spelled differently by the standard library per architecture.
const sysFstatat = syscall.SYS_FSTATAT
