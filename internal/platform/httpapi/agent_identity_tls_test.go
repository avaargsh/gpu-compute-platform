package httpapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test agent CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func newTestCertificate(t *testing.T, ca testCA, client bool, expired bool) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	if expired {
		notBefore = time.Now().Add(-3 * time.Hour)
		notAfter = time.Now().Add(-time.Hour)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
	}
	if client {
		uri, err := url.Parse(clusterAIdentity)
		if err != nil {
			t.Fatal(err)
		}
		tmpl.URIs = []*url.URL{uri}
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	leaf, err := tls.X509KeyPair(leafPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func tlsClientForTest(ca testCA, cert *tls.Certificate) *http.Client {
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca.pem)
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}
	if cert != nil {
		tlsConfig.Certificates = []tls.Certificate{*cert}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}, Timeout: 5 * time.Second}
}

func TestRealTLSHandshakeAndAgentScope(t *testing.T) {
	serverCA := newTestCA(t)
	agentCA := newTestCA(t)
	goodCert := newTestCertificate(t, agentCA, true, false)
	badCA := newTestCA(t)
	wrongCert := newTestCertificate(t, badCA, true, false)
	expiredCert := newTestCertificate(t, agentCA, true, true)
	serverCert := newTestCertificate(t, serverCA, false, false)

	handler, err := AgentMTLSRouter(NewRouterWithAgentStore(agentstore.NewMemory()), "platform.example")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	agentRoots := x509.NewCertPool()
	agentRoots.AppendCertsFromPEM(agentCA.pem)
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    agentRoots,
		Certificates: []tls.Certificate{serverCert},
	}
	server.StartTLS()
	defer server.Close()
	endpoint := server.URL + "/api/v1/agent/desired?clusterId=cluster-a"

	good := tlsClientForTest(serverCA, &goodCert)
	defer good.CloseIdleConnections()
	resp, err := good.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trusted certificate status=%d; want 200", resp.StatusCode)
	}
	resp, err = good.Get(server.URL + "/api/v1/agent/desired?clusterId=cluster-b")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-cluster status=%d; want 403", resp.StatusCode)
	}

	anonymous := tlsClientForTest(serverCA, nil)
	defer anonymous.CloseIdleConnections()
	req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("X-Forwarded-Client-Cert", clusterAIdentity)
	resp, err = anonymous.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous with spoofed header status=%d; want 401", resp.StatusCode)
	}

	for name, cert := range map[string]*tls.Certificate{
		"wrong-ca": &wrongCert,
		"expired":  &expiredCert,
	} {
		t.Run(name, func(t *testing.T) {
			client := tlsClientForTest(serverCA, cert)
			defer client.CloseIdleConnections()
			resp, err := client.Get(endpoint)
			if err == nil {
				resp.Body.Close()
				t.Fatalf("TLS accepted an untrusted/expired Agent certificate: status=%d", resp.StatusCode)
			}
		})
	}
	wrongServerCA := tlsClientForTest(agentCA, &goodCert)
	defer wrongServerCA.CloseIdleConnections()
	resp, err = wrongServerCA.Get(endpoint)
	if err == nil {
		resp.Body.Close()
		t.Fatal("client accepted HTTPS server using untrusted CA")
	}
}
