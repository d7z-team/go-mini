package randlib

import (
	"bytes"
	"math/rand"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestNewDefaultRandDoesNotUseFixedSeedOne(t *testing.T) {
	fixedFirst := rand.New(rand.NewSource(1)).Int63()

	for i := 0; i < 4; i++ {
		if got := newDefaultRand().Int63(); got != fixedFirst {
			return
		}
	}

	t.Fatalf("newDefaultRand matched the first Int63 from seed=1 on every attempt")
}

func TestRandHostStateIsIsolatedPerInstance(t *testing.T) {
	first := rand.New(rand.NewSource(1)).Int63()
	secondSrc := rand.New(rand.NewSource(1))
	secondSrc.Int63()
	second := secondSrc.Int63()

	h1 := NewRandHost()
	h2 := NewRandHost()
	h1.Seed(1)
	h2.Seed(1)

	if got := h1.Int63(); got != first {
		t.Fatalf("host1 first Int63 = %d, want %d", got, first)
	}
	if got := h2.Int63(); got != first {
		t.Fatalf("host2 first Int63 = %d, want %d", got, first)
	}
	if got := h1.Int63(); got != second {
		t.Fatalf("host1 second Int63 = %d, want %d", got, second)
	}
	if got := h2.Int63(); got != second {
		t.Fatalf("host2 second Int63 = %d, want %d", got, second)
	}
}

func TestRandHostReadUsesEntropySource(t *testing.T) {
	orig := entropyRead
	t.Cleanup(func() { entropyRead = orig })

	entropyRead = func(p []byte) (int, error) {
		for i := range p {
			p[i] = byte(i + 1)
		}
		return len(p), nil
	}

	h := NewRandHost()
	h.Seed(1)
	buf := []byte{0, 0, 0, 0}
	n, err := h.Read(&ffigo.BytesRef{Value: buf})
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if n != len(buf) {
		t.Fatalf("Read returned n=%d, want %d", n, len(buf))
	}
	if !bytes.Equal(buf, []byte{1, 2, 3, 4}) {
		t.Fatalf("Read filled %v, want [1 2 3 4]", buf)
	}
}
