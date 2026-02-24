package proxy

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/http-doppelganger/internal/config"
)

type TLSPassthroughProxy struct {
	config   *config.Config
	listener net.Listener
}

func NewTLSPassthroughProxy(cfg *config.Config) *TLSPassthroughProxy {
	return &TLSPassthroughProxy{
		config: cfg,
	}
}

func (p *TLSPassthroughProxy) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	p.listener = listener

	log.Printf("TLS passthrough proxy listening on %s", addr)

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

func (p *TLSPassthroughProxy) Close() error {
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}

func (p *TLSPassthroughProxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	clientConn.SetReadDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)
	if err != nil {
		log.Printf("TLS proxy read error: %v", err)
		return
	}

	clientConn.SetReadDeadline(time.Time{})

	if n < 5 || buf[0] != 0x16 {
		log.Printf("Not a TLS connection, received non-TLS data")
		return
	}

	targetAddr := p.config.GitLabHTTPSAddr()
	targetConn, err := net.DialTimeout("tcp", targetAddr, 30*time.Second)
	if err != nil {
		log.Printf("TLS proxy dial error to %s: %v", targetAddr, err)
		return
	}
	defer targetConn.Close()

	if _, err := targetConn.Write(buf[:n]); err != nil {
		log.Printf("TLS proxy write error: %v", err)
		return
	}

	p.pipe(clientConn, targetConn)
}

func (p *TLSPassthroughProxy) pipe(client, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copyWithClose := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		if tcpConn, ok := dst.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}

	go copyWithClose(target, client)
	go copyWithClose(client, target)

	wg.Wait()
}
