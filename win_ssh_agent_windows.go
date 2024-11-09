//go:build windows
// +build windows

package main

import (
	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	// Windows does expostthe ssh agent on this pipe
	sshAgentPipe = `\\.\pipe\openssh-ssh-agent`
)

func WindowsSSHAgent() ssh.AuthMethod {

	if sshAgent, err := winio.DialPipe(sshAgentPipe, nil); err == nil {
		return ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers)
	}
	return nil
}
