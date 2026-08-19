//go:build ignore

// This program regenerates fixtures/token-fixtures.json, the golden fixture
// shared by the Go tokens package and its TypeScript twin
// (packages/domain/src/token.ts). It is not part of the build or test
// binary — run it by hand with
// `go run internal/platform/tokens/testdata/generate.go` from
// hdms-backend/ whenever the token format itself changes, then commit the
// regenerated fixture. Its output is deterministic given the fixed seed
// below, so re-running without a format change produces no diff.
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/hito-hospital/hdms/internal/platform/tokens"
)

type fixtureCase struct {
	Token string `json:"token"`
	Valid bool   `json:"valid"`
	Case  string `json:"case"`
}

const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func main() {
	rng := rand.New(rand.NewSource(42))

	var cases []fixtureCase

	// --- 200 valid cases ---

	// 150 plain Generate() outputs, alternating subject hint.
	for i := 0; i < 150; i++ {
		hint := tokens.HintUser
		if i%2 == 1 {
			hint = tokens.HintDevice
		}
		tok, err := tokens.Generate(hint)
		must(err)
		cases = append(cases, fixtureCase{Token: tok.String(), Valid: true, Case: "generated"})
	}

	// 25 lowercase renderings of a fresh valid token, proving
	// case-insensitivity.
	for i := 0; i < 25; i++ {
		tok, err := tokens.Generate(tokens.HintUser)
		must(err)
		cases = append(cases, fixtureCase{Token: strings.ToLower(tok.String()), Valid: true, Case: "lowercase"})
	}

	// 25 tokens with '1' and '0' swapped for the ambiguous characters they
	// stand in for (I/L and O respectively), proving the transcription
	// substitution Parse applies.
	for i := 0; i < 25; i++ {
		tok, err := tokens.Generate(tokens.HintDevice)
		must(err)
		s := tok.String()
		s = strings.ReplaceAll(s, "1", pick(rng, "I", "L"))
		s = strings.ReplaceAll(s, "0", "O")
		cases = append(cases, fixtureCase{Token: s, Valid: true, Case: "ambiguous-substitution"})
	}

	// --- 200 invalid cases, 40 per category ---

	for i := 0; i < 40; i++ {
		tok, err := tokens.Generate(tokens.HintUser)
		must(err)
		s := tok.String()
		bad := flipCheckChar(s)
		cases = append(cases, fixtureCase{Token: bad, Valid: false, Case: "bad-checksum"})
	}

	for i := 0; i < 40; i++ {
		tok, err := tokens.Generate(tokens.HintDevice)
		must(err)
		s := "XX" + strings.TrimPrefix(tok.String(), tokens.Namespace)
		cases = append(cases, fixtureCase{Token: s, Valid: false, Case: "bad-namespace"})
	}

	for i := 0; i < 40; i++ {
		tok, err := tokens.Generate(tokens.HintUser)
		must(err)
		parts := strings.SplitN(tok.String(), "-", 4)
		parts[1] = "X"
		cases = append(cases, fixtureCase{Token: strings.Join(parts, "-"), Valid: false, Case: "bad-hint"})
	}

	for i := 0; i < 40; i++ {
		tok, err := tokens.Generate(tokens.HintDevice)
		must(err)
		parts := strings.SplitN(tok.String(), "-", 4)
		if i%2 == 0 {
			parts[2] = parts[2][:9] // one short
		} else {
			parts[2] = parts[2] + "A" // one long
		}
		cases = append(cases, fixtureCase{Token: strings.Join(parts, "-"), Valid: false, Case: "bad-length"})
	}

	for i := 0; i < 40; i++ {
		tok, err := tokens.Generate(tokens.HintUser)
		must(err)
		parts := strings.SplitN(tok.String(), "-", 4)
		// Inject a character that is never touched by Parse's I/L/O→1/0
		// normalization, so this is guaranteed to stay a genuinely
		// different (and invalid) payload rather than silently
		// round-tripping back to the original valid one.
		excluded := []byte{'U', '!', '@', '#', '%'}
		b := []byte(parts[2])
		b[3] = excluded[i%len(excluded)]
		parts[2] = string(b)
		cases = append(cases, fixtureCase{Token: strings.Join(parts, "-"), Valid: false, Case: "bad-payload-char"})
	}

	rng.Shuffle(len(cases), func(i, j int) { cases[i], cases[j] = cases[j], cases[i] })

	data, err := json.MarshalIndent(cases, "", "  ")
	must(err)
	data = append(data, '\n')

	for _, dest := range []string{
		filepath.FromSlash("../fixtures/token-fixtures.json"),
	} {
		must(os.WriteFile(dest, data, 0o644))
		fmt.Println("wrote", dest)
	}
}

// flipCheckChar changes the token's check character to a different valid
// check-alphabet symbol, corrupting the checksum without touching anything
// else.
func flipCheckChar(s string) string {
	const checkAlphabet = alphabet + "*~$=U"
	i := strings.LastIndex(s, "-") + 1
	cur := s[i]
	next := checkAlphabet[(strings.IndexByte(checkAlphabet, cur)+1)%len(checkAlphabet)]
	return s[:i] + string(next)
}

func pick(rng *rand.Rand, opts ...string) string {
	return opts[rng.Intn(len(opts))]
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
