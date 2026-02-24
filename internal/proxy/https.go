package proxy

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/http-doppelganger/internal/config"
)

type HTTPSTerminationProxy struct {
	config       *config.Config
	reverseProxy *httputil.ReverseProxy
	listener     net.Listener
}

func NewHTTPSTerminationProxy(cfg *config.Config) *HTTPSTerminationProxy {
	targetURL := &url.URL{
		Scheme: "http",
		Host:   cfg.GitLabHTTPAddr(),
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	gitlabHost := cfg.GitLab.Host
	gitlabHTTPAddr := cfg.GitLabHTTPAddr()
	gitlabHTTPSAddr := cfg.GitLabHTTPSAddr()

	proxy.Director = func(req *http.Request) {
		originalHost := req.Host

		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host

		if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
			if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
				clientIP = prior + ", " + clientIP
			}
			req.Header.Set("X-Forwarded-For", clientIP)
		}

		req.Header.Set("X-Forwarded-Host", originalHost)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Real-IP", strings.Split(req.RemoteAddr, ":")[0])

		req.Host = gitlabHost
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		originalHost := resp.Request.Header.Get("X-Forwarded-Host")
		if originalHost == "" {
			return nil
		}

		location := resp.Header.Get("Location")
		if location != "" {
			newLocation := rewriteLocation(location, gitlabHost, gitlabHTTPAddr, gitlabHTTPSAddr, originalHost, "https")
			if newLocation != location {
				resp.Header.Set("Location", newLocation)
				log.Printf("Rewrote Location: %s -> %s", location, newLocation)
			}
		}

		for _, cookie := range resp.Cookies() {
			if cookie.Domain == gitlabHost {
				cookie.Domain = extractHost(originalHost)
			}
		}

		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("HTTPS proxy error: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("Bad Gateway"))
	}

	proxy.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 0,
	}

	return &HTTPSTerminationProxy{
		config:       cfg,
		reverseProxy: proxy,
	}
}

func (p *HTTPSTerminationProxy) ListenAndServeTLS(addr, certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	listener, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	p.listener = listener

	log.Printf("HTTPS termination proxy listening on %s", addr)

	server := &http.Server{
		Handler:      p,
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	return server.Serve(listener)
}

func (p *HTTPSTerminationProxy) Close() error {
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}

func (p *HTTPSTerminationProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWebSocketRequest(r) {
		p.handleWebSocket(w, r)
		return
	}

	p.reverseProxy.ServeHTTP(w, r)
}

func (p *HTTPSTerminationProxy) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	targetAddr := p.config.GitLabHTTPAddr()

	targetConn, err := net.DialTimeout("tcp", targetAddr, 30*time.Second)
	if err != nil {
		log.Printf("WebSocket dial error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Printf("WebSocket hijack not supported")
		http.Error(w, "WebSocket not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("WebSocket hijack error: %v", err)
		http.Error(w, "WebSocket error", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	r.Host = p.config.GitLab.Host
	if err := r.Write(targetConn); err != nil {
		log.Printf("WebSocket write request error: %v", err)
		return
	}

	p.copyBidirectional(clientConn, targetConn)
}

func (p *HTTPSTerminationProxy) copyBidirectional(client, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(target, client)
		if tc, ok := target.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(client, target)
		if tc, ok := client.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
}
