package httpclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type rotationCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newRotationCA(t *testing.T) rotationCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "disposable agent rotation CA"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return rotationCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func rotationLeaf(t *testing.T, ca rotationCA, serial int64, client bool) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
	}
	if client {
		uri, err := url.Parse("spiffe://platform.example/cluster/cluster-a/agent")
		if err != nil {
			t.Fatal(err)
		}
		template.URIs = []*url.URL{uri}
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
}

func writeRotationPair(t *testing.T, certPath, keyPath string, cert, key []byte) {
	t.Helper()
	if err := os.WriteFile(certPath, cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
}

// The receiver must see a freshly authenticated identity on the next request,
// not a reused HTTP keep-alive session or a cached certificate on read error.
func TestMTLSClientCertificateRotationAndFailClosedReload(t *testing.T) {
	ca := newRotationCA(t)
	serverPEM, serverKey := rotationLeaf(t, ca, 10, false)
	serverPair, err := tls.X509KeyPair(serverPEM, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca.pem) {
		t.Fatal("test root not parsed")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 {
			http.Error(w, "missing verified Agent certificate", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(r.TLS.PeerCertificates[0].SerialNumber.String()))
	}))
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverPair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    roots,
	}
	server.StartTLS()
	defer server.Close()

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "agent.pem")
	keyPath := filepath.Join(dir, "agent-key.pem")
	if err := os.WriteFile(caPath, ca.pem, 0600); err != nil {
		t.Fatal(err)
	}
	firstCert, firstKey := rotationLeaf(t, ca, 21, true)
	nextCert, nextKey := rotationLeaf(t, ca, 22, true)
	writeRotationPair(t, certPath, keyPath, firstCert, firstKey)
	client, err := NewMTLSClient(caPath, certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || !transport.DisableKeepAlives {
		t.Fatal("Agent identity rotation must disable keep-alive reuse")
	}

	getIdentity := func() (string, error) {
		resp, err := client.Get(server.URL + "/agent-identity-probe")
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		return string(body), err
	}
	if got, err := getIdentity(); err != nil || got != "21" {
		t.Fatalf("first TLS identity=%q err=%v; want serial 21", got, err)
	}
	writeRotationPair(t, certPath, keyPath, nextCert, nextKey)
	if got, err := getIdentity(); err != nil || got != "22" {
		t.Fatalf("rotated TLS identity=%q err=%v; want serial 22", got, err)
	}
	// A transient, mismatched pair during rotation must fail instead of
	// silently using the previously loaded cert from an earlier handshake.
	if err := os.WriteFile(certPath, firstCert, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := getIdentity(); err == nil {
		t.Fatalf("mismatched client certificate/key accepted, identity=%q", got)
	}
	// A missing credential must fail closed; do not retry with cached key.
	if err := os.Remove(certPath); err != nil {
		t.Fatal(err)
	}
	if got, err := getIdentity(); err == nil {
		t.Fatalf("missing client certificate accepted, identity=%q", got)
	}
	writeRotationPair(t, certPath, keyPath, nextCert, nextKey)
	if got, err := getIdentity(); err != nil || got != "22" {
		t.Fatalf("restored TLS identity=%q err=%v; want serial 22", got, err)
	}
}
