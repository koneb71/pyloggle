package tenant

import (
	"crypto/sha256"
	"encoding/hex"
)

func APIKeyHash(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}
