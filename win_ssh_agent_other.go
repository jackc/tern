//go:build !windows
// +build !windows

package main

import "golang.org/x/crypto/ssh"

func WindowsSSHAgent() ssh.AuthMethod {
	return nil
}
