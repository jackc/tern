package main

import (
	"errors"
	"fmt"
	"net"
	"os"

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

var sshKeyFiles = [...]string{
	".ssh/id_dsa",
	".ssh/id_rsa",
	".ssh/id_ed25519",
	".ssh/id_ecdsa",
}

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
		for _, f := range sshKeyFiles {
			keyFile := fmt.Sprintf("%s/%s", homeDir, f)
			if auth, err := PrivateKey(keyFile); auth != nil {
				sshConfig.Auth = append(sshConfig.Auth, auth)
			} else if err != nil {
				var pkErr *ssh.PassphraseMissingError
				if errors.As(err, &pkErr) {
					fmt.Printf("files encrypted with a passphrase are not supported (%q)\n", keyFile)
				} else if !os.IsNotExist(err) {
					fmt.Printf("error opening key file: %s", err)
				}
			}
		}
	}

	return ssh.Dial("tcp", net.JoinHostPort(config.Host, config.Port), sshConfig)
}

func SSHAgent() ssh.AuthMethod {
	if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		return ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers)
	}
	return nil
}

func PrivateKey(path string) (ssh.AuthMethod, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}
