package crypto

import (
	"testing"
)

func TestSecretCodecRoundTrip(t *testing.T) {
	key := []byte("this-is-a-32-byte-key-for-aes!!!")
	codec, err := NewSecretCodec(key)
	if err != nil {
		t.Fatal(err)
	}

	tests := []string{
		"AKIAIOSFODNN7EXAMPLE",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"",
		"hello world with special chars !@#$%^&*()",
	}

	for _, plaintext := range tests {
		encrypted, err := codec.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if plaintext != "" && encrypted == plaintext {
			t.Fatalf("Encrypt(%q) returned plaintext unchanged", plaintext)
		}
		decrypted, err := codec.Decrypt(encrypted)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", encrypted, err)
		}
		if decrypted != plaintext {
			t.Fatalf("round-trip failed: got %q, want %q", decrypted, plaintext)
		}
	}
}

func TestSecretCodecEmptyString(t *testing.T) {
	key := []byte("this-is-a-32-byte-key-for-aes!!!")
	codec, _ := NewSecretCodec(key)

	enc, err := codec.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("Encrypt empty: enc=%q err=%v", enc, err)
	}
	dec, err := codec.Decrypt("")
	if err != nil || dec != "" {
		t.Fatalf("Decrypt empty: dec=%q err=%v", dec, err)
	}
}

func TestNewSecretCodecWrongKeyLength(t *testing.T) {
	_, err := NewSecretCodec([]byte("short"))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestDecryptInvalidCiphertext(t *testing.T) {
	key := []byte("this-is-a-32-byte-key-for-aes!!!")
	codec, _ := NewSecretCodec(key)

	_, err := codec.Decrypt("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}

	_, err = codec.Decrypt("dG9vc2hvcnQ=") // "tooshort" in base64
	if err == nil {
		t.Fatal("expected error for short ciphertext")
	}
}
