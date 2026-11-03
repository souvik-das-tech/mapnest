package crypto

import (
	"bytes"
	"testing"
)

func TestSealOpen(t *testing.T) {
	b, err := New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b.Seal("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("s3cret")) {
		t.Fatal("plaintext visible in sealed value")
	}
	got, err := b.Open(sealed)
	if err != nil || got != "s3cret" {
		t.Fatalf("Open = %q, %v", got, err)
	}

	other, _ := New(bytes.Repeat([]byte{2}, 32))
	if _, err := other.Open(sealed); err == nil {
		t.Error("wrong key should fail")
	}

	if s, _ := b.Seal(""); s != nil {
		t.Error("empty secret should seal to nil")
	}
}
