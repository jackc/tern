package main

import (
	"fmt"
	"net"
	"os"

	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type SSHConnConfig struct {
	Host     string
	Port     string
	User     string
	Password string
}

const (
	// Windows does expostthe ssh agent on this pipe
	sshAgentPipe = `\\.\pipe\openssh-ssh-agent`
)

func NewSSHClient(config *SSHConnConfig) (*ssh.Client, error) {
	sshConfig := &ssh.ClientConfig{
		User: config.User,
	}

	if auth := SSHAgent(); auth != nil {
		sshConfig.Auth = append(sshConfig.Auth, auth)
	}

	if auth := WindowsSSHAgent(); auth != nil {
		sshConfig.Auth = append(sshConfig.Auth, auth)
	}

	if config.Password != "" {
		sshConfig.Auth = append(sshConfig.Auth, ssh.Password(config.Password))
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		if hostKeyCallback, err := knownhosts.New(fmt.Sprintf("%s/.ssh/known_hosts", homeDir)); err == nil {
			sshConfig.HostKeyCallback = hostKeyCallback
		}
		if auth := PrivateKey(fmt.Sprintf("%s/.ssh/id_rsa", homeDir)); auth != nil {
			sshConfig.Auth = append(sshConfig.Auth, auth)
		}
	}

	return ssh.Dial("tcp", net.JoinHostPort(config.Host, config.Port), sshConfig)
}

func WindowsSSHAgent() ssh.AuthMethod {

	if sshAgent, err := winio.DialPipe(sshAgentPipe, nil); err == nil {
		return ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers)
	} else {
		fmt.Printf("Error opening windows ssh agent: %v", err)
	}
	return nil
}

func SSHAgent() ssh.AuthMethod {
	if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		return ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers)
	}
	return nil
}

func PrivateKey(path string) ssh.AuthMethod {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil
	}
	return ssh.PublicKeys(signer)
}
