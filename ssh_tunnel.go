package main

import (
	"fmt"
	"io"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type SSHConnConfig struct {
	Host     string
	Port     string
	User     string
	Password string
}

type SSHTunnelServer struct {
	client     *ssh.Client
	listener   net.Listener
	localHost  string
	localPort  string
	remoteHost string
	remotePort string
}

func NewSSHClient(config *SSHConnConfig) (*ssh.Client, error) {
	sshConfig := &ssh.ClientConfig{
		User: config.User,
		Auth: []ssh.AuthMethod{SSHAgent()},
	}

	if config.Password != "" {
		sshConfig.Auth = append(sshConfig.Auth, ssh.Password(config.Password))
	}

	return ssh.Dial("tcp", net.JoinHostPort(config.Host, config.Port), sshConfig)
}

func NewSSHTunnelServer(client *ssh.Client, remoteHost string, remotePort string) (*SSHTunnelServer, error) {
	s := SSHTunnelServer{client: client, remoteHost: remoteHost, remotePort: remotePort}

	var err error
	s.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	s.localHost, s.localPort, err = net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return nil, err
	}

	go s.listen()

	return &s, nil
}

func (t *SSHTunnelServer) Host() string {
	return t.localHost
}

func (t *SSHTunnelServer) Port() string {
	return t.localPort
}

func (t *SSHTunnelServer) listen() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Accept error: %v\n", err)
			os.Exit(2)
		}
		go t.handleConn(conn)
	}
}

func (t *SSHTunnelServer) handleConn(localConn net.Conn) {
	remoteConn, err := t.client.Dial("tcp", net.JoinHostPort(t.remoteHost, t.remotePort))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Dial error: %v\n", err)
		os.Exit(2)
	}

	copyConn := func(w, r net.Conn) {
		_, err := io.Copy(w, r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Copy error: %v\n", err)
			os.Exit(2)
		}
	}

	go copyConn(localConn, remoteConn)
	go copyConn(remoteConn, localConn)
}

func SSHAgent() ssh.AuthMethod {
	if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		return ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers)
	}
	return nil
}
