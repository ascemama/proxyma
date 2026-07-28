// Package tlsserver prepares and validates certificates for Proxyma's HTTPS listener.
package tlsserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	certificateLifetime = 397 * 24 * time.Hour
	renewalWindow       = 30 * 24 * time.Hour
)

// Options describes the certificate required by the HTTPS listener.
type Options struct {
	Mode            string
	CertificateFile string
	PrivateKeyFile  string
	ServerName      string
	ListenAddress   string
	Now             func() time.Time
	Hostname        func() (string, error)
}

// Info is safe certificate metadata suitable for logs and command output.
type Info struct {
	CertificateFile   string    `json:"certificate_file"`
	PrivateKeyFile    string    `json:"private_key_file"`
	NotBefore         time.Time `json:"not_before"`
	NotAfter          time.Time `json:"not_after"`
	FingerprintSHA256 string    `json:"fingerprint_sha256"`
	DNSNames          []string  `json:"dns_names,omitempty"`
	IPAddresses       []string  `json:"ip_addresses,omitempty"`
	Generated         bool      `json:"generated"`
}

// Prepare validates a provided pair or creates/renews a self-signed pair.
func Prepare(options Options) (Info, error) {
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	currentTime := now().UTC()
	if options.Mode == "provided" {
		info, err := ValidatePair(options.CertificateFile, options.PrivateKeyFile, currentTime)
		if err != nil {
			return Info{}, fmt.Errorf("validate provided TLS certificate: %w", err)
		}
		return info, nil
	}
	if options.Mode != "self_signed" {
		return Info{}, fmt.Errorf("unsupported TLS mode %q", options.Mode)
	}

	dnsNames, ipAddresses := requiredSANs(options)
	if info, err := ValidatePair(options.CertificateFile, options.PrivateKeyFile, currentTime); err == nil {
		certificate, parseErr := readLeafCertificate(options.CertificateFile)
		if parseErr == nil && certificate.NotAfter.After(currentTime.Add(renewalWindow)) &&
			matchesSANs(certificate, dnsNames, ipAddresses) {
			return info, nil
		}
	}

	if err := generateSelfSigned(options.CertificateFile, options.PrivateKeyFile, dnsNames, ipAddresses, currentTime); err != nil {
		return Info{}, err
	}
	info, err := ValidatePair(options.CertificateFile, options.PrivateKeyFile, currentTime)
	if err != nil {
		return Info{}, fmt.Errorf("validate generated TLS certificate: %w", err)
	}
	info.Generated = true
	return info, nil
}

// ValidatePair loads a PEM certificate chain and matching unencrypted private key.
func ValidatePair(certificateFile, privateKeyFile string, now time.Time) (Info, error) {
	if strings.TrimSpace(certificateFile) == "" || strings.TrimSpace(privateKeyFile) == "" {
		return Info{}, errors.New("certificate and private key files are required")
	}
	if _, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile); err != nil {
		return Info{}, err
	}
	certificate, err := readLeafCertificate(certificateFile)
	if err != nil {
		return Info{}, err
	}
	now = now.UTC()
	if now.Before(certificate.NotBefore) {
		return Info{}, fmt.Errorf("certificate is not valid before %s", certificate.NotBefore.Format(time.RFC3339))
	}
	if !now.Before(certificate.NotAfter) {
		return Info{}, fmt.Errorf("certificate expired at %s", certificate.NotAfter.Format(time.RFC3339))
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	ips := make([]string, 0, len(certificate.IPAddresses))
	for _, ip := range certificate.IPAddresses {
		ips = append(ips, ip.String())
	}
	return Info{
		CertificateFile:   certificateFile,
		PrivateKeyFile:    privateKeyFile,
		NotBefore:         certificate.NotBefore.UTC(),
		NotAfter:          certificate.NotAfter.UTC(),
		FingerprintSHA256: strings.ToUpper(hex.EncodeToString(fingerprint[:])),
		DNSNames:          append([]string(nil), certificate.DNSNames...),
		IPAddresses:       ips,
	}, nil
}

func requiredSANs(options Options) ([]string, []net.IP) {
	dnsSet := map[string]struct{}{"localhost": {}}
	ipSet := map[string]net.IP{
		net.ParseIP("127.0.0.1").String(): net.ParseIP("127.0.0.1"),
		net.ParseIP("::1").String():       net.ParseIP("::1"),
	}
	addName := func(value string) {
		value = strings.TrimSpace(strings.Trim(value, "[]"))
		if value == "" || value == "0.0.0.0" || value == "::" {
			return
		}
		if ip := net.ParseIP(strings.Split(value, "%")[0]); ip != nil {
			ipSet[ip.String()] = ip
			return
		}
		dnsSet[strings.ToLower(value)] = struct{}{}
	}
	addName(options.ServerName)
	host, _, err := net.SplitHostPort(options.ListenAddress)
	if err == nil {
		addName(host)
	}
	hostname := os.Hostname
	if options.Hostname != nil {
		hostname = options.Hostname
	}
	if value, err := hostname(); err == nil {
		addName(value)
	}

	dnsNames := make([]string, 0, len(dnsSet))
	for value := range dnsSet {
		dnsNames = append(dnsNames, value)
	}
	sort.Strings(dnsNames)
	ipKeys := make([]string, 0, len(ipSet))
	for value := range ipSet {
		ipKeys = append(ipKeys, value)
	}
	sort.Strings(ipKeys)
	ipAddresses := make([]net.IP, 0, len(ipKeys))
	for _, value := range ipKeys {
		ipAddresses = append(ipAddresses, ipSet[value])
	}
	return dnsNames, ipAddresses
}

func matchesSANs(certificate *x509.Certificate, dnsNames []string, ipAddresses []net.IP) bool {
	if len(certificate.DNSNames) != len(dnsNames) || len(certificate.IPAddresses) != len(ipAddresses) {
		return false
	}
	for _, name := range dnsNames {
		found := false
		for _, existing := range certificate.DNSNames {
			if strings.EqualFold(name, existing) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, ip := range ipAddresses {
		found := false
		for _, existing := range certificate.IPAddresses {
			if ip.Equal(existing) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func generateSelfSigned(certificateFile, privateKeyFile string, dnsNames []string, ipAddresses []net.IP, now time.Time) error {
	if filepath.Clean(filepath.Dir(certificateFile)) != filepath.Clean(filepath.Dir(privateKeyFile)) {
		return errors.New("self-signed certificate and private key must use the same directory")
	}
	directory := filepath.Dir(certificateFile)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create TLS directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure TLS directory: %w", err)
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate TLS private key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return fmt.Errorf("generate certificate serial: %w", err)
	}
	commonName := "Proxyma"
	if len(dnsNames) > 0 {
		commonName = dnsNames[0]
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"Proxyma"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(certificateLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("create self-signed certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("encode TLS private key: %w", err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := writePair(certificateFile, privateKeyFile, certificatePEM, privateKeyPEM); err != nil {
		return err
	}
	return nil
}

func writePair(certificateFile, privateKeyFile string, certificatePEM, privateKeyPEM []byte) error {
	certificateTemp, err := os.CreateTemp(filepath.Dir(certificateFile), ".proxyma-cert-*")
	if err != nil {
		return fmt.Errorf("create temporary certificate: %w", err)
	}
	certificateTempName := certificateTemp.Name()
	defer os.Remove(certificateTempName)
	privateKeyTemp, err := os.CreateTemp(filepath.Dir(privateKeyFile), ".proxyma-key-*")
	if err != nil {
		certificateTemp.Close()
		return fmt.Errorf("create temporary private key: %w", err)
	}
	privateKeyTempName := privateKeyTemp.Name()
	defer os.Remove(privateKeyTempName)
	if err := certificateTemp.Chmod(0o644); err != nil {
		return err
	}
	if _, err := certificateTemp.Write(certificatePEM); err != nil {
		return err
	}
	if err := certificateTemp.Close(); err != nil {
		return err
	}
	if err := privateKeyTemp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := privateKeyTemp.Write(privateKeyPEM); err != nil {
		return err
	}
	if err := privateKeyTemp.Close(); err != nil {
		return err
	}
	if err := replaceFile(certificateTempName, certificateFile); err != nil {
		return fmt.Errorf("install self-signed certificate: %w", err)
	}
	if err := replaceFile(privateKeyTempName, privateKeyFile); err != nil {
		return fmt.Errorf("install self-signed private key: %w", err)
	}
	return nil
}

func replaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}

func readLeafCertificate(path string) (*x509.Certificate, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("certificate file does not begin with a PEM CERTIFICATE block")
	}
	return x509.ParseCertificate(block.Bytes)
}
