package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"
)

// generateAndSaveKey crea y guarda una clave privada RSA
func generateAndSaveKey(path string) (*rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("fallo al generar la clave: %v", err)
	}
	keyFile, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("fallo al crear el archivo de clave: %v", err)
	}
	defer keyFile.Close()
	pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return key, nil
}

// createAndSignCert crea un certificado usando su propia clave privada y lo firma con la CA.
func createAndSignCert(caCert *x509.Certificate, caKey *rsa.PrivateKey, certKey *rsa.PrivateKey, commonName string, filePath string) error {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("fallo al generar el número de serie: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"My Distributed Directory"},
			CommonName:   commonName,
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour), // Válido por 1 año
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{"localhost"},
	}

	// Usar la clave pública del servidor para el certificado
	// Usar la clave privada de la CA para firmar
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, caCert, &certKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("fallo al crear el certificado: %v", err)
	}

	certFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("fallo al crear el archivo de certificado: %v", err)
	}
	defer certFile.Close()
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	return nil
}

func main() {
	// 1. Generar la clave privada y el certificado autofirmado de la CA
	caKey, err := generateAndSaveKey("ca.key")
	if err != nil {
		log.Fatal(err)
	}
	caTmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"My Distributed Directory"},
			CommonName:   "My Distributed Directory Root CA",
		},
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
	}
	caBytes, err := x509.CreateCertificate(rand.Reader, &caTmpl, &caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		log.Fatalf("Falla al crear el certificado de la CA: %v", err)
	}
	caFile, err := os.Create("ca.crt")
	if err != nil {
		log.Fatalf("Falla al crear ca.crt: %v", err)
	}
	defer caFile.Close()
	pem.Encode(caFile, &pem.Block{Type: "CERTIFICATE", Bytes: caBytes})
	fmt.Println("CA creada con éxito: ca.crt y ca.key")

	// 2. Generar la clave privada del servidor
	serverKey, err := generateAndSaveKey("server.key")
	if err != nil {
		log.Fatal(err)
	}

	// 3. Firmar el certificado del servidor con la clave de la CA y la clave del servidor
	caCert, _ := x509.ParseCertificate(caBytes)
	err = createAndSignCert(caCert, caKey, serverKey, "server.localhost", "server.crt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Certificado y clave del servidor creados con éxito: server.crt y server.key")
}
