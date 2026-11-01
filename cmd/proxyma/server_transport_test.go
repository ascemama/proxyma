package main

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"proxyma/internal/config"
)

func TestServeConfiguredHTTPSAndHTTP(t *testing.T) {
	for _, scheme := range []string{config.SchemeHTTPS, config.SchemeHTTP} {
		t.Run(scheme, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			cfg := config.Default()
			cfg.Scheme = scheme
			cfg.ListenAddress = listener.Addr().String()
			cfg.TLSCertificateFile = filepath.Join(root, "server.crt")
			cfg.TLSPrivateKeyFile = filepath.Join(root, "server.key")
			server := &http.Server{
				Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					writer.WriteHeader(http.StatusNoContent)
				}),
				TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			}
			done := make(chan error, 1)
			go func() {
				done <- serveConfigured(server, listener, cfg, log.New(io.Discard, "", 0))
			}()

			client := &http.Client{Timeout: 5 * time.Second}
			protocol := "http"
			if scheme == config.SchemeHTTPS {
				protocol = "https"
				client.Transport = &http.Transport{TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // Test-only trust for the generated certificate.
					MinVersion:         tls.VersionTLS12,
				}}
			}
			response, err := client.Get(protocol + "://" + listener.Addr().String() + "/")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d", response.StatusCode)
			}
			_ = server.Close()
			<-done
		})
	}
}

func TestNewHTTPServerResourceLimits(t *testing.T) {
	cfg := config.Default()
	cfg.ListenAddress = "127.0.0.1:8080"
	server := newHTTPServer(cfg, http.NotFoundHandler())

	if server.ReadHeaderTimeout != httpReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, httpReadHeaderTimeout)
	}
	if server.ReadTimeout != httpReadTimeout {
		t.Fatalf("ReadTimeout = %s, want %s", server.ReadTimeout, httpReadTimeout)
	}
	if server.WriteTimeout != httpWriteTimeout {
		t.Fatalf("WriteTimeout = %s, want %s", server.WriteTimeout, httpWriteTimeout)
	}
	if server.IdleTimeout != httpIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, httpIdleTimeout)
	}
	if server.MaxHeaderBytes != httpMaxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, httpMaxHeaderBytes)
	}
}

func TestCookieModeDerivedOnlyFromScheme(t *testing.T) {
	if usesDevelopmentCookies(config.SchemeHTTPS) {
		t.Fatal("HTTPS unexpectedly selected development cookies")
	}
	if !usesDevelopmentCookies(config.SchemeHTTP) {
		t.Fatal("HTTP did not select development cookies")
	}
}
