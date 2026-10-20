package mitm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type TargetHost struct {
	Host     string
	Provider string
}

var InterceptedHosts = map[string]string{
	"api.anthropic.com":                  "anthropic",
	"api.openai.com":                     "openai",
	"api.githubcopilot.com":              "copilot",
	"copilot-proxy.githubusercontent.com": "copilot",
	"cloudcode-pa.googleapis.com":        "antigravity",
}

type TunnelServer struct {
	caCert     *x509.Certificate
	caKey      *rsa.PrivateKey
	tlsConfig  *tls.Config
	certCache  map[string]*tls.Certificate
	cacheMu    sync.RWMutex
	routerAddr string
}

func NewTunnelServer(routerAddr string) (*TunnelServer, error) {
	ts := &TunnelServer{
		certCache:  make(map[string]*tls.Certificate),
		routerAddr: routerAddr,
	}

	home, _ := os.UserHomeDir()
	certDir := filepath.Join(home, ".goroute", "certs")
	_ = os.MkdirAll(certDir, 0700)

	caCertPath := filepath.Join(certDir, "goroute-ca.crt")
	caKeyPath := filepath.Join(certDir, "goroute-ca.key")

	if err := ts.loadOrGenerateCA(caCertPath, caKeyPath); err != nil {
		return nil, fmt.Errorf("failed to init MITM CA: %w", err)
	}

	return ts, nil
}

func (ts *TunnelServer) loadOrGenerateCA(certPath, keyPath string) error {
	certPEM, err1 := os.ReadFile(certPath)
	keyPEM, err2 := os.ReadFile(keyPath)

	if err1 == nil && err2 == nil {
		tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err == nil {
			parsed, err := x509.ParseCertificate(tlsCert.Certificate[0])
			if err == nil {
				ts.caCert = parsed
				ts.caKey = tlsCert.PrivateKey.(*rsa.PrivateKey)
				log.Printf("[GoRoute MITM] Loaded existing root CA from %s", certPath)
				return nil
			}
		}
	}

	// Generate new root CA
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization:  []string{"GoRoute AI Gateway"},
			CommonName:    "GoRoute Root CA",
			Country:       []string{"US"},
			Province:      []string{"CA"},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{"Local"},
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return err
	}

	parsedCA, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return err
	}
	ts.caCert = parsedCA
	ts.caKey = privKey

	// Save to disk
	certOut, _ := os.Create(certPath)
	_ = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	certOut.Close()

	keyOut, _ := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	_ = pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})
	keyOut.Close()

	log.Printf("[GoRoute MITM] Generated new root CA certificate at %s", certPath)
	return nil
}

// HandleConnect implements RFC 2817 HTTP CONNECT tunneling and transparent forwarding
func (ts *TunnelServer) HandleConnect(w http.ResponseWriter, r *http.Request) {
	destHost := r.Host
	if !strings.Contains(destHost, ":") {
		destHost = destHost + ":443"
	}

	hostOnly, _, _ := net.SplitHostPort(destHost)
	isIntercepted := false
	if _, ok := InterceptedHosts[strings.ToLower(hostOnly)]; ok {
		isIntercepted = true
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "HTTP Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()

	// Respond 200 Connection Established to HTTP CONNECT client
	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	if isIntercepted && ts.routerAddr != "" {
		// Forward to local router gateway directly
		destConn, err := net.DialTimeout("tcp", ts.routerAddr, 5*time.Second)
		if err != nil {
			log.Printf("[GoRoute MITM] Failed to dial local router %s: %v", ts.routerAddr, err)
			return
		}
		defer destConn.Close()

		go io.Copy(destConn, clientConn)
		io.Copy(clientConn, destConn)
		return
	}

	// Transparent Passthrough for non-AI hosts
	destConn, err := net.DialTimeout("tcp", destHost, 10*time.Second)
	if err != nil {
		return
	}
	defer destConn.Close()

	go io.Copy(destConn, clientConn)
	io.Copy(clientConn, destConn)
}
