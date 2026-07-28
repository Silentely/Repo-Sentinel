package githubx

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAppJWTAndConfigured(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	c := NewAppClient(12345, path)
	if !c.Configured() {
		t.Fatal("should be configured")
	}
	token, err := c.AppJWT()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("jwt invalid: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("claims type")
	}
	if claims["iss"] != "12345" {
		t.Fatalf("iss=%v", claims["iss"])
	}
	exp := int64(claims["exp"].(float64))
	if time.Until(time.Unix(exp, 0)) > 10*time.Minute || time.Until(time.Unix(exp, 0)) < time.Minute {
		t.Fatalf("unexpected exp window: %v", time.Until(time.Unix(exp, 0)))
	}
}

func TestAppClientNotConfigured(t *testing.T) {
	c := NewAppClient(0, "")
	if c.Configured() {
		t.Fatal("empty should not be configured")
	}
	if _, err := c.AppJWT(); err == nil {
		t.Fatal("expected error")
	}
}
