package fusionsolar

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"
)

type PublicKeyData struct {
	EnableEncrypt bool   `json:"enableEncrypt"`
	PubKey        string `json:"pubKey"`
	Version       string `json:"version"`
	TimeStamp     any    `json:"timeStamp"`
}

func SecureRandomHex() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 32)
	for i, v := range b {
		out[i*2], out[i*2+1] = hex[v>>4], hex[v&15]
	}
	return string(out), nil
}

func EncryptPassword(k PublicKeyData, password string) (string, error) {
	if k.PubKey == "" || k.Version == "" {
		return "", fusionErr("Invalid key_data parameter")
	}
	if !k.EnableEncrypt {
		return password, nil
	}
	block, _ := pem.Decode([]byte(k.PubKey))
	if block == nil {
		return "", fusionErr("Failed to load public key for encryption")
	}
	var pub *rsa.PublicKey
	if p, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		pub, _ = p.(*rsa.PublicKey)
	}
	if pub == nil {
		if p, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
			pub = p
		}
	}
	if pub == nil {
		return "", fusionErr("Failed to load public key for encryption")
	}
	encoded := url.QueryEscape(password)
	// Python's urllib.parse.quote leaves '/' unescaped. QueryEscape does not match it,
	// so normalize the one meaningful difference for this API.
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "%2F", "/")
	const chunk = 270
	result := ""
	h := sha512.New384()
	for i := 0; i < len(encoded); i += chunk {
		end := i + chunk
		if end > len(encoded) {
			end = len(encoded)
		}
		c, err := rsa.EncryptOAEP(h, rand.Reader, pub, []byte(encoded[i:end]), nil)
		if err != nil {
			return "", fmt.Errorf("encrypt password: %w", err)
		}
		if result != "" {
			result += "00000001"
		}
		result += base64.StdEncoding.EncodeToString(c)
	}
	return result + k.Version, nil
}
