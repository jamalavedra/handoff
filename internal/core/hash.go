package core

import "crypto/sha256"

func hashKey(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
