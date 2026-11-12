package client

import "securespeak/internal/e2e"

func encryptForTest(body []byte, recips map[uint32][]byte) ([]byte, []byte, map[string][]byte, error) {
	return e2e.EncryptMessage(body, e2e.ChatAAD("channel", 1, 1), recips)
}
