package spellings

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

func hexHalfSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(random)[:16]) // want `hexHalfSessionID minted from wall-clock time is enumerable`
}

func hexOffsetSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(random)[8:24]) // want `hexOffsetSessionID minted from wall-clock time is enumerable`
}

func hexFullOffsetSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(random)[8:40])
}

func fmtHexHalfSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), fmt.Sprintf("%x", random)[:16]) // want `fmtHexHalfSessionID minted from wall-clock time is enumerable`
}

func fmtHexFullSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), fmt.Sprintf("%x", random)[:32])
}

func base64ShortSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), base64.StdEncoding.EncodeToString(random)[:21]) // want `base64ShortSessionID minted from wall-clock time is enumerable`
}

func base64FullSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), base64.StdEncoding.EncodeToString(random)[:22])
}

func base64URLShortSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), base64.URLEncoding.EncodeToString(random)[3:24]) // want `base64URLShortSessionID minted from wall-clock time is enumerable`
}

func base64URLFullSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), base64.URLEncoding.EncodeToString(random)[3:25])
}

func base64RawShortSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), base64.RawStdEncoding.EncodeToString(random)[:21]) // want `base64RawShortSessionID minted from wall-clock time is enumerable`
}

func base64RawFullSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), base64.RawStdEncoding.EncodeToString(random)[:22])
}

func base64RawURLShortSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), base64.RawURLEncoding.EncodeToString(random)[:21]) // want `base64RawURLShortSessionID minted from wall-clock time is enumerable`
}

func base64RawURLFullSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), base64.RawURLEncoding.EncodeToString(random)[:22])
}

func opaqueEncoding(random []byte) string { return hex.EncodeToString(random) }

func opaqueEncodedSessionID() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), opaqueEncoding(random)[:32]) // want `opaqueEncodedSessionID minted from wall-clock time is enumerable`
}
