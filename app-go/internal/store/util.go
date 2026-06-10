package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func NowMS() int64 {
	return time.Now().UnixMilli()
}

func NewHexID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(buf[:])
}

func trimStringRunes(value, fallback string, max int) string {
	if value == "" {
		value = fallback
	}
	runes := []rune(value)
	if len(runes) > max {
		runes = runes[:max]
	}
	return string(runes)
}
