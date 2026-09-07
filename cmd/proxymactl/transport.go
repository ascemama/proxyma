package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"proxyma/internal/config"
	"proxyma/internal/tlsserver"
)

func dispatchTransportCommand(args []string) (any, error) {
	resource, action, rest := args[0], args[1], args[2:]
	switch {
	case resource == "certificates" && action == "validate":
		return validateCertificateArgs(rest)
	case resource == "certificates" && action == "install":
		return installCertificateArgs(rest)
	case resource == "config" && action == "update-transport":
		return updateTransportArgs(rest)
	default:
		return nil, fmt.Errorf("unsupported %s action %q", resource, action)
	}
}

func installCertificateArgs(args []string) (tlsserver.Info, error) {
	fs := flag.NewFlagSet("certificates install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	certificateFile := fs.String("cert-file", "", "source certificate PEM file")
	privateKeyFile := fs.String("key-file", "", "source private key PEM file")
	destinationCertificateFile := fs.String("dest-cert-file", "", "managed certificate destination")
	destinationPrivateKeyFile := fs.String("dest-key-file", "", "managed private key destination")
	if err := fs.Parse(args); err != nil {
		return tlsserver.Info{}, err
	}
	if fs.NArg() != 0 ||
		strings.TrimSpace(*certificateFile) == "" ||
		strings.TrimSpace(*privateKeyFile) == "" ||
		strings.TrimSpace(*destinationCertificateFile) == "" ||
		strings.TrimSpace(*destinationPrivateKeyFile) == "" {
		return tlsserver.Info{}, errors.New("certificates install requires --cert-file, --key-file, --dest-cert-file, and --dest-key-file")
	}
	return installCertificatePair(*certificateFile, *privateKeyFile, *destinationCertificateFile, *destinationPrivateKeyFile)
}

func installCertificatePair(sourceCertificate, sourceKey, destinationCertificate, destinationKey string) (tlsserver.Info, error) {
	if filepath.Clean(destinationCertificate) == filepath.Clean(destinationKey) {
		return tlsserver.Info{}, errors.New("certificate and private key destinations must be different files")
	}
	info, err := tlsserver.ValidatePair(sourceCertificate, sourceKey, time.Now())
	if err != nil {
		return tlsserver.Info{}, err
	}
	for _, path := range []string{destinationCertificate, destinationKey} {
		if existing, statErr := os.Lstat(path); statErr == nil {
			if existing.Mode()&os.ModeSymlink != 0 || !existing.Mode().IsRegular() {
				return tlsserver.Info{}, fmt.Errorf("refusing unsafe certificate destination %s", path)
			}
		} else if !os.IsNotExist(statErr) {
			return tlsserver.Info{}, statErr
		}
	}
	stagedCertificate, err := stageCertificateFile(sourceCertificate, destinationCertificate, 0o644)
	if err != nil {
		return tlsserver.Info{}, err
	}
	defer os.Remove(stagedCertificate)
	stagedKey, err := stageCertificateFile(sourceKey, destinationKey, 0o600)
	if err != nil {
		return tlsserver.Info{}, err
	}
	defer os.Remove(stagedKey)
	if _, err := tlsserver.ValidatePair(stagedCertificate, stagedKey, time.Now()); err != nil {
		return tlsserver.Info{}, fmt.Errorf("validate staged certificate pair: %w", err)
	}

	certificateBackup, err := moveAside(destinationCertificate)
	if err != nil {
		return tlsserver.Info{}, err
	}
	keyBackup, err := moveAside(destinationKey)
	if err != nil {
		restoreMovedAside(certificateBackup, destinationCertificate)
		return tlsserver.Info{}, err
	}
	committedCertificate := false
	if err := os.Rename(stagedCertificate, destinationCertificate); err != nil {
		restoreMovedAside(keyBackup, destinationKey)
		restoreMovedAside(certificateBackup, destinationCertificate)
		return tlsserver.Info{}, err
	}
	committedCertificate = true
	if err := os.Rename(stagedKey, destinationKey); err != nil {
		if committedCertificate {
			_ = os.Remove(destinationCertificate)
		}
		restoreMovedAside(keyBackup, destinationKey)
		restoreMovedAside(certificateBackup, destinationCertificate)
		return tlsserver.Info{}, err
	}
	removeMovedAside(certificateBackup)
	removeMovedAside(keyBackup)
	return info, nil
}

func stageCertificateFile(source, destination string, mode os.FileMode) (string, error) {
	content, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".proxyma-tls-*")
	if err != nil {
		return "", err
	}
	name := temp.Name()
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		os.Remove(name)
		return "", err
	}
	if err := temp.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func moveAside(path string) (string, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	backup, err := os.CreateTemp(filepath.Dir(path), ".proxyma-tls-backup-*")
	if err != nil {
		return "", err
	}
	backupName := backup.Name()
	if err := backup.Close(); err != nil {
		os.Remove(backupName)
		return "", err
	}
	if err := os.Remove(backupName); err != nil {
		return "", err
	}
	if err := os.Rename(path, backupName); err != nil {
		return "", err
	}
	return backupName, nil
}

func restoreMovedAside(backup, destination string) {
	if backup != "" {
		_ = os.Rename(backup, destination)
	}
}

func removeMovedAside(backup string) {
	if backup != "" {
		_ = os.Remove(backup)
	}
}

func validateCertificateArgs(args []string) (tlsserver.Info, error) {
	fs := flag.NewFlagSet("certificates validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	certificateFile := fs.String("cert-file", "", "certificate PEM file")
	privateKeyFile := fs.String("key-file", "", "private key PEM file")
	if err := fs.Parse(args); err != nil {
		return tlsserver.Info{}, err
	}
	if fs.NArg() != 0 || strings.TrimSpace(*certificateFile) == "" || strings.TrimSpace(*privateKeyFile) == "" {
		return tlsserver.Info{}, errors.New("certificates validate requires --cert-file and --key-file")
	}
	return tlsserver.ValidatePair(*certificateFile, *privateKeyFile, time.Now())
}

type transportUpdateResult struct {
	File            string `json:"file"`
	Scheme          string `json:"scheme,omitempty"`
	TLSMode         string `json:"tls_mode,omitempty"`
	CertificateFile string `json:"certificate_file,omitempty"`
	PrivateKeyFile  string `json:"private_key_file,omitempty"`
	ServerName      string `json:"server_name,omitempty"`
}

func updateTransportArgs(args []string) (transportUpdateResult, error) {
	fs := flag.NewFlagSet("config update-transport", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "Proxyma JSON config file")
	scheme := fs.String("scheme", "", "https or http")
	mode := fs.String("tls-mode", "", "self_signed or provided")
	certificateFile := fs.String("tls-cert-file", "", "managed certificate file")
	privateKeyFile := fs.String("tls-key-file", "", "managed private key file")
	serverName := fs.String("tls-server-name", "", "self-signed certificate DNS name or IP")
	if err := fs.Parse(args); err != nil {
		return transportUpdateResult{}, err
	}
	if fs.NArg() != 0 || strings.TrimSpace(*file) == "" {
		return transportUpdateResult{}, errors.New("config update-transport requires --file")
	}
	visited := map[string]bool{}
	fs.Visit(func(item *flag.Flag) { visited[item.Name] = true })
	if visited["scheme"] && *scheme != config.SchemeHTTPS && *scheme != config.SchemeHTTP {
		return transportUpdateResult{}, fmt.Errorf("--scheme must be %q or %q", config.SchemeHTTPS, config.SchemeHTTP)
	}
	if visited["tls-mode"] && *mode != config.TLSModeSelfSigned && *mode != config.TLSModeProvided {
		return transportUpdateResult{}, fmt.Errorf("--tls-mode must be %q or %q", config.TLSModeSelfSigned, config.TLSModeProvided)
	}
	if visited["tls-cert-file"] != visited["tls-key-file"] {
		return transportUpdateResult{}, errors.New("--tls-cert-file and --tls-key-file must be supplied together")
	}
	if visited["tls-cert-file"] &&
		(strings.TrimSpace(*certificateFile) == "" || strings.TrimSpace(*privateKeyFile) == "") {
		return transportUpdateResult{}, errors.New("--tls-cert-file and --tls-key-file must not be empty")
	}
	return updateTransportFile(*file, visited, *scheme, *mode, *certificateFile, *privateKeyFile, *serverName)
}

func updateTransportFile(path string, visited map[string]bool, scheme, mode, certificateFile, privateKeyFile, serverName string) (transportUpdateResult, error) {
	if info, err := os.Lstat(path); err != nil {
		return transportUpdateResult{}, err
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return transportUpdateResult{}, fmt.Errorf("refusing unsafe configuration file %s", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return transportUpdateResult{}, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(content, &root); err != nil {
		return transportUpdateResult{}, err
	}
	server := map[string]json.RawMessage{}
	if raw := root["server"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &server); err != nil {
			return transportUpdateResult{}, fmt.Errorf("decode server configuration: %w", err)
		}
	}
	tlsConfig := map[string]json.RawMessage{}
	if raw := server["tls"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &tlsConfig); err != nil {
			return transportUpdateResult{}, fmt.Errorf("decode TLS configuration: %w", err)
		}
	}
	setJSON := func(target map[string]json.RawMessage, key string, value string) error {
		raw, err := json.Marshal(value)
		if err == nil {
			target[key] = raw
		}
		return err
	}
	if visited["scheme"] {
		if err := setJSON(server, "scheme", scheme); err != nil {
			return transportUpdateResult{}, err
		}
	}
	if visited["tls-mode"] {
		if err := setJSON(tlsConfig, "mode", mode); err != nil {
			return transportUpdateResult{}, err
		}
	}
	if visited["tls-cert-file"] {
		if err := setJSON(tlsConfig, "certificate_file", certificateFile); err != nil {
			return transportUpdateResult{}, err
		}
		if err := setJSON(tlsConfig, "private_key_file", privateKeyFile); err != nil {
			return transportUpdateResult{}, err
		}
	}
	if visited["tls-server-name"] {
		if err := setJSON(tlsConfig, "server_name", serverName); err != nil {
			return transportUpdateResult{}, err
		}
	}
	rawTLS, err := json.Marshal(tlsConfig)
	if err != nil {
		return transportUpdateResult{}, err
	}
	server["tls"] = rawTLS
	rawServer, err := json.Marshal(server)
	if err != nil {
		return transportUpdateResult{}, err
	}
	root["server"] = rawServer
	if raw := root["security"]; len(raw) > 0 {
		var security map[string]json.RawMessage
		if err := json.Unmarshal(raw, &security); err != nil {
			return transportUpdateResult{}, fmt.Errorf("decode security configuration: %w", err)
		}
		delete(security, "allow_insecure_user_cookies")
		root["security"], err = json.Marshal(security)
		if err != nil {
			return transportUpdateResult{}, err
		}
	}
	updated, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return transportUpdateResult{}, err
	}
	updated = append(updated, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".proxyma-config-*")
	if err != nil {
		return transportUpdateResult{}, err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return transportUpdateResult{}, err
	}
	if _, err := temp.Write(updated); err != nil {
		temp.Close()
		return transportUpdateResult{}, err
	}
	if err := temp.Close(); err != nil {
		return transportUpdateResult{}, err
	}
	if err := os.Rename(tempName, path); err != nil {
		return transportUpdateResult{}, err
	}
	result := transportUpdateResult{File: path}
	decodeString := func(raw json.RawMessage) string {
		var value string
		_ = json.Unmarshal(raw, &value)
		return value
	}
	result.Scheme = decodeString(server["scheme"])
	result.TLSMode = decodeString(tlsConfig["mode"])
	result.CertificateFile = decodeString(tlsConfig["certificate_file"])
	result.PrivateKeyFile = decodeString(tlsConfig["private_key_file"])
	result.ServerName = decodeString(tlsConfig["server_name"])
	return result, nil
}
