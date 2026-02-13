package credentials

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"testing"
	"time"
)

func generateCACert(notBefore, notAfter time.Time) []byte {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		IsCA:         true,
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func TestCheckCACertExpiration_WarnsWhenExpiringSoon(t *testing.T) {
	// 10-year cert with 30 days remaining → 20% threshold is 2 years → should warn
	notBefore := time.Now().Add(-10*365*24*time.Hour + 30*24*time.Hour)
	cert := generateCACert(notBefore, time.Now().Add(30*24*time.Hour))

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	checkCACertExpiration("test-cluster", cert)

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("WARNING")) {
		t.Errorf("expected warning log, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("test-cluster")) {
		t.Errorf("expected cluster name in log, got: %s", output)
	}
}

func TestCheckCACertExpiration_NoWarningWhenFarFromExpiry(t *testing.T) {
	// 10-year cert issued now → 20% threshold is 2 years, remaining ~10 years → no warning
	cert := generateCACert(time.Now(), time.Now().Add(10*365*24*time.Hour))

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	checkCACertExpiration("test-cluster", cert)

	if buf.Len() > 0 {
		t.Errorf("expected no log output, got: %s", buf.String())
	}
}

func TestCheckCACertExpiration_InvalidPEM(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	checkCACertExpiration("test-cluster", []byte("not a pem"))

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("failed to decode")) {
		t.Errorf("expected decode error log, got: %s", output)
	}
}

func TestCheckCACertExpiration_EmptyCert(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	checkCACertExpiration("test-cluster", nil)

	if buf.Len() > 0 {
		t.Errorf("expected no log output, got: %s", buf.String())
	}
}
