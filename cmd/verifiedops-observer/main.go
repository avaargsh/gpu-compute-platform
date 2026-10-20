// verifiedops-observer is a one-shot, independently authorized read-only
// Kubernetes Observer. It has no HTTP listener, database client, mutation API
// or key-generation path. Production dispatcher integration is not enabled.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const maxTicketBytes = 1 << 20
const maxPrivateKeyBytes = 8 << 10
const maxPublicKeyBytes = 1 << 10

type observerConfig struct {
	ticketFile              string
	dispatcherPublicKeyFile string
	dispatcherKeyID         string
	observerPrivateKeyFile  string
	observerKeyID           string
}

func configFromEnv(getenv func(string) string) (observerConfig, error) {
	cfg := observerConfig{
		ticketFile:              getenv("VERIFIEDOPS_TICKET_FILE"),
		dispatcherPublicKeyFile: getenv("VERIFIEDOPS_DISPATCHER_PUBKEY_FILE"),
		dispatcherKeyID:         getenv("VERIFIEDOPS_DISPATCHER_KEY_ID"),
		observerPrivateKeyFile:  getenv("VERIFIEDOPS_OBSERVER_SIGNING_KEY_FILE"),
		observerKeyID:           getenv("VERIFIEDOPS_OBSERVER_SIGNING_KEY_ID"),
	}
	for _, p := range []string{cfg.ticketFile, cfg.dispatcherPublicKeyFile, cfg.observerPrivateKeyFile} {
		if p == "" || !filepath.IsAbs(p) {
			return observerConfig{}, errors.New("observer input and key file paths must be absolute")
		}
	}
	if cfg.dispatcherKeyID == "" || cfg.observerKeyID == "" ||
		len(cfg.dispatcherKeyID) > 128 || len(cfg.observerKeyID) > 128 {
		return observerConfig{}, errors.New("both pinned signing key identifiers are required")
	}
	if filepath.Clean(cfg.ticketFile) == filepath.Clean(cfg.observerPrivateKeyFile) ||
		filepath.Clean(cfg.dispatcherPublicKeyFile) == filepath.Clean(cfg.observerPrivateKeyFile) {
		return observerConfig{}, errors.New("Observer signing key cannot also be an input file")
	}
	return cfg, nil
}

// O_NOFOLLOW prohibits symlink escape while opening the file; regular file,
// size and permissions are checked on the already-open file descriptor.
func readFileNoSymlink(path string, limit int64, secret bool) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open isolated Observer input: %w", err)
	}
	f := os.NewFile(uintptr(fd), "observer-input")
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return nil, errors.New("Observer input must be a regular file")
	}
	if stat.Size() > limit {
		return nil, errors.New("Observer input exceeds limit")
	}
	if secret && stat.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("Observer private key must not be readable by group or others")
	}
	content, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read isolated Observer input: %w", err)
	}
	if len(content) == 0 || int64(len(content)) > limit {
		return nil, errors.New("Observer input is empty or oversized")
	}
	return content, nil
}

// Refuse hard-link aliases as well as symlinks. A key that shares an inode
// with the ticket or pinned public root must not be loaded as a signer.
func rejectSignerInputAlias(cfg observerConfig) error {
	key, err := os.Stat(cfg.observerPrivateKeyFile)
	if err != nil {
		return err
	}
	for _, path := range []string{cfg.ticketFile, cfg.dispatcherPublicKeyFile} {
		input, err := os.Stat(path)
		if err != nil {
			return err
		}
		if os.SameFile(key, input) {
			return errors.New("Observer signing key aliases a work input")
		}
	}
	return nil
}

func loadObserverPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := readFileNoSymlink(path, maxPrivateKeyBytes, true)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("Observer signer must be a single PKCS#8 Ed25519 private PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid Observer signer")
	}
	signer, ok := key.(ed25519.PrivateKey)
	if !ok || len(signer) != ed25519.PrivateKeySize {
		return nil, errors.New("Observer signer must be Ed25519")
	}
	return signer, nil
}

func loadDispatcherRoot(path, keyID string) (verifiedops.ObserverTrust, error) {
	data, err := readFileNoSymlink(path, maxPublicKeyBytes, false)
	if err != nil {
		return verifiedops.ObserverTrust{}, err
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return verifiedops.ObserverTrust{}, errors.New("invalid pinned dispatcher Ed25519 public key")
	}
	return verifiedops.ObserverTrust{KeyID: keyID, PublicKey: ed25519.PublicKey(decoded)}, nil
}

// The only output on stdout is the signed DSSE evidence line. Never log
// input tickets, tokens, signer bytes or private material.
func runOnce(ctx context.Context, cfg observerConfig, client kubernetes.Interface, output io.Writer) error {
	raw, err := readFileNoSymlink(cfg.ticketFile, maxTicketBytes, false)
	if err != nil {
		return err
	}
	root, err := loadDispatcherRoot(cfg.dispatcherPublicKeyFile, cfg.dispatcherKeyID)
	if err != nil {
		return err
	}
	approved, err := verifiedops.ApproveObserverWorkTicket(raw, root)
	if err != nil {
		return err
	}
	if err := rejectSignerInputAlias(cfg); err != nil {
		return err
	}
	signer, err := loadObserverPrivateKey(cfg.observerPrivateKeyFile)
	if err != nil {
		return err
	}
	envelope, _, err := verifiedops.RunApprovedObserverWork(ctx, client, approved,
		verifiedops.ObserverSigner{KeyID: cfg.observerKeyID, Signer: signer})
	if err != nil {
		return err
	}
	if _, err := output.Write(append(envelope, '\n')); err != nil {
		return err
	}
	return nil
}

func main() {
	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid Observer configuration:", err)
		os.Exit(2)
	}
	// No KUBECONFIG or external admin credential fallback is permitted.
	k8s, err := rest.InClusterConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dedicated projected Kubernetes identity unavailable")
		os.Exit(1)
	}
	k8s.Timeout = 15 * time.Second
	client, err := kubernetes.NewForConfig(k8s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Kubernetes observer client unavailable")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := runOnce(ctx, cfg, client, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Observer refused to emit signed evidence:", err)
		os.Exit(1)
	}
}
