//go:build linux

package main

import (
	"syscall"
	"unsafe"
)

const (
	ioctlReadTermios  = 0x5401 // TCGETS
	ioctlWriteTermios = 0x5402 // TCSETS
	ioctlWinSize      = 0x5413 // TIOCGWINSZ
)

func getTermios(fd int) (*syscall.Termios, error) {
	t := &syscall.Termios{}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), ioctlReadTermios, uintptr(unsafe.Pointer(t)))
	if errno != 0 {
		return nil, errno
	}
	return t, nil
}

func setTermios(fd int, t *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), ioctlWriteTermios, uintptr(unsafe.Pointer(t)))
	if errno != 0 {
		return errno
	}
	return nil
}

func termSize(fd int) (w, h int, err error) {
	ws := &struct {
		Row, Col, Xpixel, Ypixel uint16
	}{}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), ioctlWinSize, uintptr(unsafe.Pointer(ws)))
	if errno != 0 {
		return 0, 0, errno
	}
	return int(ws.Col), int(ws.Row), nil
}
