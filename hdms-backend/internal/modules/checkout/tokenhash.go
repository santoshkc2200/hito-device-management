package checkout

import "crypto/sha256"

// scanTokenHash is checkout's own hash of a raw token, used only to compare
// one scan against the next for server-side duplicate detection
// (2.3c: "the server compares the incoming token's hash against the
// session's last scan and timestamp"). It is unrelated to credentials'
// pepper-based token_hash — this one exists purely to avoid storing the
// raw token on scan_sessions, never to look anything up by it.
func scanTokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// tokenPreview returns the last four characters of a token, for
// scan_events.token_preview — the only fragment of a token ever persisted
// (docs/03-domain-model.md#checkout).
func tokenPreview(token string) string {
	if len(token) <= 4 {
		return token
	}
	return token[len(token)-4:]
}
