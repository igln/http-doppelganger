package proxy

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/http-doppelganger/internal/config"
)

type SSHProxy struct {
	config   *config.Config
	listener net.Listener
}

func NewSSHProxy(cfg *config.Config) *SSHProxy {
	return &SSHProxy{
		config: cfg,
	}
}

func (p *SSHProxy) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	p.listener = listener

	log.Printf("SSH proxy listening on %s", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}

		go p.handleConnection(conn)
	}
}

func (p *SSHProxy) Close() error {
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}

func (p *SSHProxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	targetAddr := p.config.GitLabSSHAddr()
	targetConn, err := net.DialTimeout("tcp", targetAddr, 30*time.Second)
	if err != nil {
		log.Printf("SSH proxy dial error to %s: %v", targetAddr, err)
		return
	}
	defer targetConn.Close()

	log.Printf("SSH connection established: %s -> %s", clientConn.RemoteAddr(), targetAddr)

	p.pipe(clientConn, targetConn)
}

func (p *SSHProxy) pipe(client, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copyWithClose := func(dst, src net.Conn) {
		defer wg.Done()
		written, _ := io.Copy(dst, src)
		log.Printf("SSH pipe: copied %d bytes", written)
		if tcpConn, ok := dst.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}

	go copyWithClose(target, client)
	go copyWithClose(client, target)

	wg.Wait()
}
