package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

func main() {
	// 1. Cargar el certificado y la clave privada de la CA
	caCertPEM, err := os.ReadFile("ca.crt")
	if err != nil {
		panic("Falla al leer ca.crt")
	}
	caKeyPEM, err := os.ReadFile("ca.key")
	if err != nil {
		panic("Falla al leer ca.key")
	}

	block, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		panic("Falla al analizar el certificado de la CA")
	}

	block, _ = pem.Decode(caKeyPEM)
	caKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		panic("Falla al analizar la clave de la CA")
	}

	// 2. Generar la clave privada para el cliente
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(clientKey),
	})
	os.WriteFile("client.key", clientKeyPEM, 0600)
	fmt.Println("client.key generado.")

	// 3. Crear una plantilla de certificado para el cliente
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Distribuido Client"},
			CommonName:    "client",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// 4. Firmar el certificado del cliente con la clave de la CA
	clientCertDER, err := x509.CreateCertificate(rand.Reader, &clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		panic(err)
	}

	clientCertPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: clientCertDER,
	})
	os.WriteFile("client.crt", clientCertPEM, 0644)
	fmt.Println("client.crt generado.")
}
