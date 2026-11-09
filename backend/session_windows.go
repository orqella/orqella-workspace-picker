//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

var processIdToSessionId = syscall.NewLazyDLL("kernel32.dll").NewProc("ProcessIdToSessionId")

func sessionID() int {
	var session uint32
	ok, _, _ := processIdToSessionId.Call(uintptr(os.Getpid()), uintptr(unsafe.Pointer(&session)))
	if ok == 0 {
		return -1
	}
	return int(session)
}

func currentUID() int {
	return -1
}
