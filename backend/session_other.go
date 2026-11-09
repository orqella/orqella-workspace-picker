//go:build !windows

package main

import "os"

func sessionID() int {
	return -1
}

func currentUID() int {
	return os.Getuid()
}
