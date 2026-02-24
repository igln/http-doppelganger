package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/http-doppelganger/internal/config"
	"github.com/http-doppelganger/internal/proxy"
)

type Server struct {
	config               *config.Config
	httpServer           *http.Server
	httpProxy            *proxy.HTTPProxy
	tlsPassthroughProxy  *proxy.TLSPassthroughProxy
	httpsTerminationProxy *proxy.HTTPSTerminationProxy
	sshProxy             *proxy.SSHProxy
}

func New(cfg *config.Config) *Server {
	httpProxy := proxy.NewHTTPProxy(cfg)
	sshProxy := proxy.NewSSHProxy(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/.well-known/acme-challenge/", acmeChallengeHandler)
	mux.Handle("/", httpProxy)

	httpServer := &http.Server{
		Addr:         cfg.Proxy.HTTPListen,
		Handler:      mux,
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	server := &Server{
		config:     cfg,
		httpServer: httpServer,
		httpProxy:  httpProxy,
		sshProxy:   sshProxy,
	}

	if cfg.TLS.Enabled {
		server.httpsTerminationProxy = proxy.NewHTTPSTerminationProxy(cfg)
	} else {
		server.tlsPassthroughProxy = proxy.NewTLSPassthroughProxy(cfg)
	}

	return server
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

func acmeChallengeHandler(w http.ResponseWriter, r *http.Request) {
	challengePath := "/var/www/certbot" + r.URL.Path
	http.ServeFile(w, r, challengePath)
}

func (s *Server) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error, 3)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("HTTP proxy listening on %s -> %s://%s",
			s.config.Proxy.HTTPListen, s.config.GitLabUpstreamScheme(), s.config.GitLabUpstreamAddr())
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if s.config.TLS.Enabled {
			log.Printf("HTTPS termination proxy listening on %s -> %s",
				s.config.Proxy.HTTPSListen, s.config.GitLabHTTPAddr())
			if err := s.httpsTerminationProxy.ListenAndServeTLS(
				s.config.Proxy.HTTPSListen,
				s.config.TLS.CertFile,
				s.config.TLS.KeyFile,
			); err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					errChan <- fmt.Errorf("HTTPS termination proxy error: %w", err)
				}
			}
		} else {
			if err := s.tlsPassthroughProxy.ListenAndServe(s.config.Proxy.HTTPSListen); err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					errChan <- fmt.Errorf("TLS passthrough proxy error: %w", err)
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.sshProxy.ListenAndServe(s.config.Proxy.SSHListen); err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				errChan <- fmt.Errorf("SSH proxy error: %w", err)
			}
		}
	}()

	log.Printf("GitLab proxy started")
	log.Printf("  HTTP:  %s -> %s://%s", s.config.Proxy.HTTPListen, s.config.GitLabUpstreamScheme(), s.config.GitLabUpstreamAddr())
	if s.config.TLS.Enabled {
		log.Printf("  HTTPS: %s -> %s (TLS termination with cert: %s)",
			s.config.Proxy.HTTPSListen, s.config.GitLabHTTPAddr(), s.config.TLS.CertFile)
	} else {
		log.Printf("  HTTPS: %s -> %s (TLS passthrough)", s.config.Proxy.HTTPSListen, s.config.GitLabHTTPSAddr())
	}
	log.Printf("  SSH:   %s -> %s", s.config.Proxy.SSHListen, s.config.GitLabSSHAddr())

	select {
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down...", sig)
		return s.shutdown(ctx)
	case err := <-errChan:
		log.Printf("Server error: %v", err)
		s.shutdown(ctx)
		return err
	}
}

func (s *Server) shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var shutdownErr error

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
		shutdownErr = err
	}

	if s.tlsPassthroughProxy != nil {
		if err := s.tlsPassthroughProxy.Close(); err != nil {
			log.Printf("TLS passthrough proxy shutdown error: %v", err)
			shutdownErr = err
		}
	}

	if s.httpsTerminationProxy != nil {
		if err := s.httpsTerminationProxy.Close(); err != nil {
			log.Printf("HTTPS termination proxy shutdown error: %v", err)
			shutdownErr = err
		}
	}

	if err := s.sshProxy.Close(); err != nil {
		log.Printf("SSH proxy shutdown error: %v", err)
		shutdownErr = err
	}

	log.Printf("Server shutdown complete")
	return shutdownErr
}
