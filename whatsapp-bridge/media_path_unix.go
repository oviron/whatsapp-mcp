//go:build !windows

package main

import "syscall"

const mediaOpenFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
