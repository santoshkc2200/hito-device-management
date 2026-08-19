package tokens_test

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/tokens"
)

type fixtureCase struct {
	Token string `json:"token"`
	Valid bool   `json:"valid"`
	Case  string `json:"case"`
}

// fixturePath locates fixtures/token-fixtures.json relative to this test
// file rather than the working directory, so `go test ./...` from any
// directory finds the same golden fixture the TypeScript twin reads.
func fixturePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("tokens_test: could not determine caller for fixture path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "fixtures", "token-fixtures.json")
}

func loadFixture(t *testing.T) []fixtureCase {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []fixtureCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return cases
}

func TestGoldenFixture(t *testing.T) {
	cases := loadFixture(t)

	var valid, invalid int
	for _, tc := range cases {
		if tc.Valid {
			valid++
		} else {
			invalid++
		}
	}
	if valid != 200 || invalid != 200 {
		t.Fatalf("fixture: want 200 valid + 200 invalid, got %d valid + %d invalid", valid, invalid)
	}

	for _, tc := range cases {
		t.Run(tc.Case+"/"+tc.Token, func(t *testing.T) {
			got := tokens.Validate(tc.Token)
			if got != tc.Valid {
				t.Errorf("Validate(%q) = %v, want %v (case %q)", tc.Token, got, tc.Valid, tc.Case)
			}
		})
	}
}

func TestGenerateRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))

	const n = 100_000
	for range n {
		hint := tokens.HintUser
		if rng.Intn(2) == 1 {
			hint = tokens.HintDevice
		}

		tok, err := tokens.Generate(hint)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}

		parsed, err := tokens.Parse(tok.String())
		if err != nil {
			t.Fatalf("Parse(Generate()) failed on %q: %v", tok.String(), err)
		}
		if parsed != tok {
			t.Fatalf("Parse(Generate()) = %+v, want %+v", parsed, tok)
		}
	}
}

// TestSingleCharacterMutationRejected exercises the mod-37 checksum's
// error-detecting power directly: corrupt exactly one character of the
// checksum-protected span (the 10-character payload, or the check character
// itself) to a different character from that position's own alphabet, and
// confirm Parse rejects it. Namespace, separators and the subject hint are
// excluded here — they are validated by exact structural match, which is a
// separate, trivially-100%-reliable guarantee covered by TestParseErrors
// and TestHintIsNotAuthoritative, not a property of the checksum.
const checkAlphabetForTest = "0123456789ABCDEFGHJKMNPQRSTVWXYZ*~$=U"

func TestSingleCharacterMutationRejected(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	const payloadAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

	const n = 100_000
	var rejected int
	for range n {
		hint := tokens.HintUser
		if rng.Intn(2) == 1 {
			hint = tokens.HintDevice
		}
		tok, err := tokens.Generate(hint)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		original := tok.String()

		// Position within "HD-U-XXXXXXXXXX-C": payload occupies indices
		// 5-14, the check character is index 16 (15 is the hyphen).
		const payloadStart = 5
		var pos int
		alphabet := payloadAlphabet
		if idx := rng.Intn(len(tok.Payload) + 1); idx < len(tok.Payload) {
			pos = payloadStart + idx
		} else {
			pos = payloadStart + len(tok.Payload) + 1
			alphabet = checkAlphabetForTest
		}

		var replacement byte
		for {
			replacement = alphabet[rng.Intn(len(alphabet))]
			if replacement != original[pos] {
				break
			}
		}
		mutated := original[:pos] + string(replacement) + original[pos+1:]

		if !tokens.Validate(mutated) {
			rejected++
		}
	}

	rate := float64(rejected) / float64(n)
	if rate <= 0.999 {
		t.Fatalf("single-character mutation rejection rate = %.5f, want > 0.999 (%d/%d accepted)", rate, n-rejected, n)
	}
	t.Logf("rejected %d/%d (%.5f%%) single-character mutations", rejected, n, rate*100)
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want error
	}{
		{"empty", "", tokens.ErrInvalidFormat},
		{"no separators", "HDU7K3M9QXA24", tokens.ErrInvalidFormat},
		{"too many parts", "HD-U-7K3M9QXA2F-4-X", tokens.ErrInvalidFormat},
		{"wrong namespace", "XX-U-7K3M9QXA2F-4", tokens.ErrInvalidNamespace},
		{"wrong hint", "HD-X-7K3M9QXA2F-4", tokens.ErrInvalidHint},
		{"empty hint", "HD--7K3M9QXA2F-4", tokens.ErrInvalidHint},
		{"short payload", "HD-U-7K3M9QXA2-4", tokens.ErrInvalidPayload},
		{"long payload", "HD-U-7K3M9QXA2FF-4", tokens.ErrInvalidPayload},
		{"payload has excluded char", "HD-U-7K3M9QXAUF-4", tokens.ErrInvalidPayload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tokens.Parse(tc.raw)
			if err != tc.want {
				t.Fatalf("Parse(%q) error = %v, want %v", tc.raw, err, tc.want)
			}
		})
	}
}

func TestHintIsNotAuthoritative(t *testing.T) {
	// The hint routes but does not authorize (docs/05): a token whose hint
	// was flipped to the other valid subject still parses, because only
	// Resolve's stored subject_type is authoritative.
	tok, err := tokens.Generate(tokens.HintUser)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	flipped := strings.Replace(tok.String(), "-U-", "-D-", 1)
	if !tokens.Validate(flipped) {
		t.Fatalf("Validate(%q) = false, want true (hint mutation alone must not fail validation)", flipped)
	}
}

func TestCaseInsensitiveAndAmbiguousSubstitution(t *testing.T) {
	tok, err := tokens.Generate(tokens.HintDevice)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	lower := strings.ToLower(tok.String())
	if !tokens.Validate(lower) {
		t.Fatalf("Validate(%q) = false, want true (lowercase must parse)", lower)
	}

	substituted := strings.NewReplacer("1", "I", "0", "O").Replace(tok.String())
	if !tokens.Validate(substituted) {
		t.Fatalf("Validate(%q) = false, want true (I/O substitution must normalize)", substituted)
	}
}
