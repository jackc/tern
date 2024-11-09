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
	Host       string
	Port       string
	User       string
	Password   string
	KeyFile    string
	Passphrase string
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
	}

	if config.KeyFile != "" {
		if auth, err := PrivateKey(config.KeyFile, config.Passphrase); auth != nil {
			sshConfig.Auth = append(sshConfig.Auth, auth)
		} else if err != nil {
			fmt.Printf("Can not read key file %q: %s\n", config.KeyFile, err)
		}
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		for _, f := range sshKeyFiles {
			keyFile := fmt.Sprintf("%s/%s", homeDir, f)
			if auth, err := PrivateKey(keyFile, config.Passphrase); auth != nil {
				sshConfig.Auth = append(sshConfig.Auth, auth)
			} else if err != nil {
				fmt.Printf("Can not read key file %q: %s\n", keyFile, err)
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

func PrivateKey(keyFile string, passphrase string) (ssh.AuthMethod, error) {
	key, err := os.ReadFile(keyFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	var pkErr *ssh.PassphraseMissingError
	if err != nil && errors.As(err, &pkErr) && passphrase != "" {
		// If the key is encrypted and we have a passphrase, we try to parse it using the passphrase
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
	}
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}
