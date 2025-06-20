package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func VerifySignature(signingSecret string, headers http.Header, body []byte) bool {
	timestamp := headers.Get("X-Slack-Request-Timestamp")
	if timestamp == "" {
		return false
	}

	// Check if timestamp is within 5 minutes to prevent replay attacks
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}

	if time.Now().Unix()-ts > 300 {
		return false
	}

	// Create signature base string
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))

	// Calculate expected signature
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expectedSignature := "v0=" + hex.EncodeToString(mac.Sum(nil))

	// Compare with received signature
	receivedSignature := headers.Get("X-Slack-Signature")
	return hmac.Equal([]byte(expectedSignature), []byte(receivedSignature))
}
