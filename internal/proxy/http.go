package proxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/http-doppelganger/internal/config"
)

type HTTPProxy struct {
	config       *config.Config
	reverseProxy *httputil.ReverseProxy
	rewriter     *URLRewriter
}

func NewHTTPProxy(cfg *config.Config) *HTTPProxy {
	useHTTPS := cfg.GitLab.UseHTTPS
	scheme := "http"
	targetHost := cfg.GitLabHTTPAddr()
	if useHTTPS {
		scheme = "https"
		targetHost = cfg.GitLabHTTPSAddr()
	}

	targetURL := &url.URL{
		Scheme: scheme,
		Host:   targetHost,
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	gitlabHost := cfg.GitLab.Host
	gitlabHTTPAddr := cfg.GitLabHTTPAddr()
	gitlabHTTPSAddr := cfg.GitLabHTTPSAddr()

	var rewriter *URLRewriter
	if len(cfg.GitLab.RewriteURLs) > 0 || cfg.GitLab.ExternalURL != "" {
		var urlsToRewrite []string
		if cfg.GitLab.ExternalURL != "" {
			urlsToRewrite = append(urlsToRewrite, cfg.GitLab.ExternalURL)
		}
		urlsToRewrite = append(urlsToRewrite, cfg.GitLab.RewriteURLs...)

		proxyURL := fmt.Sprintf("http://%s", cfg.TLS.Domain)
		if cfg.TLS.Domain == "" {
			proxyURL = fmt.Sprintf("http://%s", gitlabHost)
		}
		rewriter = NewURLRewriter(urlsToRewrite, proxyURL)
		log.Printf("HTTP URL rewriter configured: %v -> %s", urlsToRewrite, proxyURL)
	}

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
		req.Header.Set("X-Forwarded-Proto", "http")
		req.Header.Set("X-Real-IP", strings.Split(req.RemoteAddr, ":")[0])

		req.Host = gitlabHost
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		originalHost := resp.Request.Header.Get("X-Forwarded-Host")
		if originalHost == "" {
			originalHost = gitlabHost
		}

		location := resp.Header.Get("Location")
		if location != "" {
			newLocation := rewriteLocation(location, gitlabHost, gitlabHTTPAddr, gitlabHTTPSAddr, originalHost, "http")
			if rewriter != nil {
				newLocation = string(rewriter.RewriteBody([]byte(newLocation)))
			}
			if newLocation != location {
				resp.Header.Set("Location", newLocation)
				log.Printf("Rewrote Location: %s -> %s", location, newLocation)
			}
		}

		if rewriter != nil {
			if err := rewriter.RewriteResponse(resp); err != nil {
				log.Printf("Body rewrite error: %v", err)
			}
		}

		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("HTTP proxy error: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("Bad Gateway"))
	}

	transport := &http.Transport{
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
	if useHTTPS {
		transport.TLSClientConfig = &tls.Config{
			ServerName: gitlabHost,
		}
	}
	proxy.Transport = transport

	log.Printf("HTTP proxy configured: upstream=%s://%s", targetURL.Scheme, targetURL.Host)

	return &HTTPProxy{
		config:       cfg,
		reverseProxy: proxy,
		rewriter:     rewriter,
	}
}

func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWebSocketRequest(r) {
		p.handleWebSocket(w, r)
		return
	}

	p.reverseProxy.ServeHTTP(w, r)
}

func isWebSocketRequest(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Upgrade")) == "websocket" &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func (p *HTTPProxy) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	targetAddr := p.config.GitLabUpstreamAddr()

	var targetConn net.Conn
	var err error

	if p.config.GitLab.UseHTTPS {
		dialer := &net.Dialer{Timeout: 30 * time.Second}
		targetConn, err = tls.DialWithDialer(dialer, "tcp", targetAddr, &tls.Config{
			ServerName: p.config.GitLab.Host,
		})
	} else {
		targetConn, err = net.DialTimeout("tcp", targetAddr, 30*time.Second)
	}
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

	copyBidirectional(clientConn, targetConn)
}

func copyBidirectional(client, target net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		io.Copy(target, client)
		if tc, ok := target.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()

	go func() {
		io.Copy(client, target)
		if tc, ok := client.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}

func rewriteLocation(location, gitlabHost, gitlabHTTPAddr, gitlabHTTPSAddr, proxyHost, proxyScheme string) string {
	parsed, err := url.Parse(location)
	if err != nil {
		return location
	}

	if parsed.Host == "" {
		return location
	}

	locationHost := parsed.Host

	if locationHost == gitlabHost || locationHost == gitlabHTTPAddr || locationHost == gitlabHTTPSAddr {
		parsed.Host = proxyHost
		if proxyScheme != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
			parsed.Scheme = proxyScheme
		}
		return parsed.String()
	}

	hostOnly := extractHost(locationHost)
	if hostOnly == gitlabHost {
		parsed.Host = proxyHost
		if proxyScheme != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
			parsed.Scheme = proxyScheme
		}
		return parsed.String()
	}

	return location
}

func extractHost(hostPort string) string {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		return hostPort
	}
	return host
}
