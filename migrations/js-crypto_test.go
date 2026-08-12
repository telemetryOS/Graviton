package migrations

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// newTestRuntime wires the driver-agnostic built-ins the same way Evaluate does.
func newTestRuntime(t *testing.T) *goja.Runtime {
	t.Helper()
	jsvm := goja.New()
	jsvm.Set("env", JSEnv(jsvm))
	jsvm.Set("enc", JSEnc(jsvm))
	jsvm.Set("crypto", JSCrypto(jsvm))
	return jsvm
}

func mustRun(t *testing.T, jsvm *goja.Runtime, src string) goja.Value {
	t.Helper()
	val, err := jsvm.RunString(src)
	if err != nil {
		t.Fatalf("RunString(%s) error = %v", src, err)
	}
	return val
}

func runErr(t *testing.T, jsvm *goja.Runtime, src string) string {
	t.Helper()
	_, err := jsvm.RunString(src)
	if err == nil {
		t.Fatalf("RunString(%s) expected an error, got none", src)
	}
	return err.Error()
}

func Test_Enc_RoundTrips(t *testing.T) {
	jsvm := newTestRuntime(t)
	for _, encoding := range []string{"base64", "base64url", "hex", "utf8"} {
		got := mustRun(t, jsvm, `enc.encode("`+encoding+`", enc.decode("`+encoding+`", enc.encode("`+encoding+`", enc.decode("utf8", "round trip"))))`)
		if got.String() != mustRun(t, jsvm, `enc.encode("`+encoding+`", enc.decode("utf8", "round trip"))`).String() {
			t.Errorf("%s did not round trip: %v", encoding, got)
		}
	}
}

func Test_Enc_UnknownEncodingNamesAlternatives(t *testing.T) {
	jsvm := newTestRuntime(t)
	msg := runErr(t, jsvm, `enc.decode("base-64", "aaaa")`)
	if !strings.Contains(msg, "unknown encoding") || !strings.Contains(msg, "base64url") {
		t.Errorf("error should name the encoding and the alternatives, got %q", msg)
	}
}

func Test_Env_MissingVariableThrowsNamingIt(t *testing.T) {
	jsvm := newTestRuntime(t)
	msg := runErr(t, jsvm, `env.get("GRAVITON_DEFINITELY_UNSET_VAR")`)
	if !strings.Contains(msg, "GRAVITON_DEFINITELY_UNSET_VAR") {
		t.Errorf("error should name the missing variable, got %q", msg)
	}
}

func Test_Env_GetAndHas(t *testing.T) {
	t.Setenv("GRAVITON_TEST_VALUE", "present")
	jsvm := newTestRuntime(t)
	if got := mustRun(t, jsvm, `env.get("GRAVITON_TEST_VALUE")`).String(); got != "present" {
		t.Errorf("env.get = %q, want %q", got, "present")
	}
	if got := mustRun(t, jsvm, `env.has("GRAVITON_TEST_VALUE")`).ToBoolean(); !got {
		t.Error("env.has = false for a set variable")
	}
	if got := mustRun(t, jsvm, `env.has("GRAVITON_DEFINITELY_UNSET_VAR")`).ToBoolean(); got {
		t.Error("env.has = true for an unset variable")
	}
}

// Test_Crypto_DecryptsLegacyDeviceCiphertext is the case this API exists for:
// a 16-byte (AES-128) key like the legacy TelemetryOS device keys, base64
// ciphertext with the nonce prefixed, holding a JSON-encoded string.
func Test_Crypto_DecryptsLegacyDeviceCiphertext(t *testing.T) {
	key := []byte("0123456789abcdef") // 16 bytes, as the legacy keys are
	plaintext := []byte(`"https://example.com/media/item.mp4"`)

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("123456789012") // 12 bytes, fixed for determinism
	sealed := append(append([]byte{}, nonce...), gcm.Seal(nil, nonce, plaintext, nil)...)

	t.Setenv("TEST_AES_KEY_V1", base64.StdEncoding.EncodeToString(key))
	jsvm := newTestRuntime(t)
	jsvm.Set("ciphertext", base64.StdEncoding.EncodeToString(sealed))

	got := mustRun(t, jsvm, `
		var key = enc.decode("base64", env.get("TEST_AES_KEY_V1"));
		var raw = enc.decode("base64", ciphertext);
		JSON.parse(enc.encode("utf8", crypto.decrypt("aes-gcm", key, raw)))
	`)
	if got.String() != "https://example.com/media/item.mp4" {
		t.Errorf("decrypted = %q, want the embedded URL", got.String())
	}
}

func Test_Crypto_WrongKeyFailsLoudly(t *testing.T) {
	key := []byte("0123456789abcdef")
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := []byte("123456789012")
	sealed := append(append([]byte{}, nonce...), gcm.Seal(nil, nonce, []byte("secret"), nil)...)

	jsvm := newTestRuntime(t)
	jsvm.Set("ciphertext", base64.StdEncoding.EncodeToString(sealed))
	msg := runErr(t, jsvm, `crypto.decrypt("aes-gcm", enc.decode("utf8", "fedcba9876543210"), enc.decode("base64", ciphertext))`)
	if !strings.Contains(msg, "decryption failed") {
		t.Errorf("a wrong key must fail, not return garbage; got %q", msg)
	}
}

func Test_Crypto_UnknownAlgorithmThrows(t *testing.T) {
	jsvm := newTestRuntime(t)
	msg := runErr(t, jsvm, `crypto.decrypt("aes-256-gcm", enc.decode("utf8", "0123456789abcdef"), enc.decode("utf8", "whatever"))`)
	if !strings.Contains(msg, "unknown algorithm") || !strings.Contains(msg, "aes-gcm") {
		t.Errorf("error should name the bad algorithm and the supported set, got %q", msg)
	}
}

// Test_Crypto_EncryptRequiresNonce pins the convergence constraint: encryption
// without an explicit nonce is refused, because a random one would make a
// migration produce different output on every run.
func Test_Crypto_EncryptRequiresNonce(t *testing.T) {
	jsvm := newTestRuntime(t)
	msg := runErr(t, jsvm, `crypto.encrypt("aes-gcm", enc.decode("utf8", "0123456789abcdef"), enc.decode("utf8", "data"))`)
	if !strings.Contains(msg, "explicit") || !strings.Contains(msg, "nonce") {
		t.Errorf("error should explain the nonce requirement, got %q", msg)
	}
}

func Test_Crypto_EncryptDecryptRoundTripIsDeterministic(t *testing.T) {
	jsvm := newTestRuntime(t)
	src := `
		var key = enc.decode("utf8", "0123456789abcdef");
		var nonce = enc.decode("utf8", "123456789012");
		enc.encode("base64", crypto.encrypt("aes-gcm", key, enc.decode("utf8", "payload"), nonce))
	`
	first := mustRun(t, jsvm, src).String()
	second := mustRun(t, jsvm, src).String()
	if first != second {
		t.Errorf("same inputs produced different ciphertext:\n  %s\n  %s", first, second)
	}
	jsvm.Set("sealed", first)
	got := mustRun(t, jsvm, `
		enc.encode("utf8", crypto.decrypt("aes-gcm",
			enc.decode("utf8", "0123456789abcdef"), enc.decode("base64", sealed)))
	`)
	if got.String() != "payload" {
		t.Errorf("round trip = %q, want %q", got.String(), "payload")
	}
}

func Test_Crypto_HashAndHmac(t *testing.T) {
	jsvm := newTestRuntime(t)
	// SHA-256 of "abc", the standard test vector.
	got := mustRun(t, jsvm, `enc.encode("hex", crypto.hash("sha256", enc.decode("utf8", "abc")))`)
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got.String() != want {
		t.Errorf("sha256(abc) = %s, want %s", got.String(), want)
	}
	mac := mustRun(t, jsvm, `enc.encode("hex", crypto.hmac("sha256", enc.decode("utf8", "key"), enc.decode("utf8", "abc")))`)
	if len(mac.String()) != 64 {
		t.Errorf("hmac-sha256 should be 32 bytes / 64 hex chars, got %d", len(mac.String()))
	}
	if msg := runErr(t, jsvm, `crypto.hmac("md5", enc.decode("utf8", "k"), enc.decode("utf8", "d"))`); !strings.Contains(msg, "unknown algorithm") {
		t.Errorf("unknown hmac algorithm should throw, got %q", msg)
	}
}

func Test_Crypto_ShortCiphertextIsReported(t *testing.T) {
	jsvm := newTestRuntime(t)
	msg := runErr(t, jsvm, `crypto.decrypt("aes-gcm", enc.decode("utf8", "0123456789abcdef"), enc.decode("utf8", "short"))`)
	if !strings.Contains(msg, "nonce") {
		t.Errorf("a too-short ciphertext should mention the nonce, got %q", msg)
	}
}
