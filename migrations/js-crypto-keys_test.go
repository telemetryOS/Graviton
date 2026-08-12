package migrations

import (
	"strings"
	"testing"
)

func Test_Crypto_Random(t *testing.T) {
	jsvm := newTestRuntime(t)
	a := mustRun(t, jsvm, `enc.encode("hex", crypto.random(32))`).String()
	b := mustRun(t, jsvm, `enc.encode("hex", crypto.random(32))`).String()
	if len(a) != 64 {
		t.Errorf("random(32) produced %d hex chars, want 64", len(a))
	}
	if a == b {
		t.Error("two calls to random produced identical bytes")
	}
	if msg := runErr(t, jsvm, `crypto.random(0)`); !strings.Contains(msg, "between 1") {
		t.Errorf("random(0) should be rejected, got %q", msg)
	}
}

// Every key pair algorithm produces two distinct, non-empty values, which is
// the shape migrations store: a public key and a private key as separate bytes.
func Test_Crypto_GenerateKeyPair(t *testing.T) {
	jsvm := newTestRuntime(t)
	for _, algorithm := range []string{"ed25519", "x25519", "ecdsa-p256", "ecdsa-p384", "ecdsa-p521", "ecdh-p256", "ecdh-p384", "ecdh-p521", "rsa-2048"} {
		pub := mustRun(t, jsvm, `enc.encode("base64", crypto.generateKeyPair("`+algorithm+`").publicKey)`).String()
		priv := mustRun(t, jsvm, `enc.encode("base64", crypto.generateKeyPair("`+algorithm+`").privateKey)`).String()
		if pub == "" || priv == "" {
			t.Errorf("%s produced an empty key", algorithm)
		}
		if pub == priv {
			t.Errorf("%s public and private keys are identical", algorithm)
		}
	}
	if msg := runErr(t, jsvm, `crypto.generateKeyPair("rsa-512")`); !strings.Contains(msg, "unknown algorithm") {
		t.Errorf("unknown key pair algorithm should throw, got %q", msg)
	}
}

func Test_Crypto_SignAndVerify(t *testing.T) {
	cases := []struct{ keyAlg, sigAlg string }{
		{"ed25519", "ed25519"},
		{"ecdsa-p256", "ecdsa-sha256"},
		{"ecdsa-p384", "ecdsa-sha384"},
		{"rsa-2048", "rsa-pss-sha256"},
		{"rsa-2048", "rsa-pkcs1-sha256"},
	}
	for _, c := range cases {
		jsvm := newTestRuntime(t)
		src := `
			var pair = crypto.generateKeyPair("` + c.keyAlg + `");
			var data = enc.decode("utf8", "the payload being signed");
			var sig  = crypto.sign("` + c.sigAlg + `", pair.privateKey, data);
			var good = crypto.verify("` + c.sigAlg + `", pair.publicKey, data, sig);
			var bad  = crypto.verify("` + c.sigAlg + `", pair.publicKey, enc.decode("utf8", "tampered"), sig);
			good + "," + bad
		`
		if got := mustRun(t, jsvm, src).String(); got != "true,false" {
			t.Errorf("%s/%s verify(valid),verify(tampered) = %s, want true,false", c.keyAlg, c.sigAlg, got)
		}
	}
}

// A signature from the wrong key must verify false rather than throwing:
// migrations branch on that outcome.
func Test_Crypto_VerifyReturnsFalseForWrongKey(t *testing.T) {
	jsvm := newTestRuntime(t)
	got := mustRun(t, jsvm, `
		var a = crypto.generateKeyPair("ed25519");
		var b = crypto.generateKeyPair("ed25519");
		var data = enc.decode("utf8", "payload");
		crypto.verify("ed25519", b.publicKey, data, crypto.sign("ed25519", a.privateKey, data))
	`)
	if got.ToBoolean() {
		t.Error("a signature from another key verified true")
	}
}

// Both sides of an exchange must arrive at the same shared secret, on every
// curve.
func Test_Crypto_Exchange(t *testing.T) {
	for _, algorithm := range []string{"x25519", "ecdh-p256", "ecdh-p384", "ecdh-p521"} {
		jsvm := newTestRuntime(t)
		got := mustRun(t, jsvm, `
			var a = crypto.generateKeyPair("`+algorithm+`");
			var b = crypto.generateKeyPair("`+algorithm+`");
			var ab = enc.encode("hex", crypto.exchange("`+algorithm+`", a.privateKey, b.publicKey));
			var ba = enc.encode("hex", crypto.exchange("`+algorithm+`", b.privateKey, a.publicKey));
			ab === ba ? ab : "MISMATCH"
		`).String()
		if got == "MISMATCH" || got == "" {
			t.Errorf("%s: the two sides derived different secrets", algorithm)
		}
	}
}

// PKCS#8 encodes ECDH and ECDSA P-curve keys identically, so a key generated
// as ecdsa-p256 is indistinguishable from an ecdh-p256 one once it has been
// through bytes. exchange converts rather than rejecting a difference it
// cannot observe — pinned here so the behaviour is deliberate rather than
// accidental. X25519 has its own OID and stays distinct.
func Test_Crypto_ExchangeAcceptsPCurveSigningKeys(t *testing.T) {
	jsvm := newTestRuntime(t)
	got := mustRun(t, jsvm, `
		var a = crypto.generateKeyPair("ecdsa-p256");
		var b = crypto.generateKeyPair("ecdh-p256");
		var ab = enc.encode("hex", crypto.exchange("ecdh-p256", a.privateKey, b.publicKey));
		var ba = enc.encode("hex", crypto.exchange("ecdh-p256", b.privateKey, a.publicKey));
		ab === ba ? "agree" : "MISMATCH"
	`).String()
	if got != "agree" {
		t.Errorf("an ecdsa P-256 key should interoperate with an ecdh one, got %s", got)
	}
}

// An X25519 key is a different type and cannot stand in for a P-curve.
func Test_Crypto_ExchangeRejectsWrongCurve(t *testing.T) {
	jsvm := newTestRuntime(t)
	msg := runErr(t, jsvm, `
		var a = crypto.generateKeyPair("x25519");
		var b = crypto.generateKeyPair("ecdh-p256");
		crypto.exchange("ecdh-p256", a.privateKey, b.publicKey)
	`)
	if !strings.Contains(msg, "curve") {
		t.Errorf("mixing curves should be reported, got %q", msg)
	}
}

func Test_Crypto_RsaOaepRoundTrip(t *testing.T) {
	jsvm := newTestRuntime(t)
	got := mustRun(t, jsvm, `
		var pair = crypto.generateKeyPair("rsa-2048");
		var sealed = crypto.encrypt("rsa-oaep-sha256", pair.publicKey, enc.decode("utf8", "asymmetric payload"));
		enc.encode("utf8", crypto.decrypt("rsa-oaep-sha256", pair.privateKey, sealed))
	`)
	if got.String() != "asymmetric payload" {
		t.Errorf("rsa-oaep round trip = %q", got.String())
	}
}

func Test_Crypto_RsaOaepWrongKeyFails(t *testing.T) {
	jsvm := newTestRuntime(t)
	msg := runErr(t, jsvm, `
		var a = crypto.generateKeyPair("rsa-2048");
		var b = crypto.generateKeyPair("rsa-2048");
		var sealed = crypto.encrypt("rsa-oaep-sha256", a.publicKey, enc.decode("utf8", "secret"));
		crypto.decrypt("rsa-oaep-sha256", b.privateKey, sealed)
	`)
	if !strings.Contains(msg, "decryption failed") {
		t.Errorf("wrong private key should fail loudly, got %q", msg)
	}
}

func Test_Crypto_Derive(t *testing.T) {
	jsvm := newTestRuntime(t)
	for _, algorithm := range []string{"hkdf-sha256", "hkdf-sha512", "pbkdf2-sha256", "scrypt", "argon2id"} {
		src := `enc.encode("hex", crypto.derive("` + algorithm + `",
			enc.decode("utf8", "secret"), enc.decode("utf8", "salt-value"), enc.decode("utf8", "context"), 32))`
		first := mustRun(t, jsvm, src).String()
		second := mustRun(t, jsvm, src).String()
		if len(first) != 64 {
			t.Errorf("%s produced %d hex chars, want 64", algorithm, len(first))
		}
		// Derivation is a pure function: same inputs, same output.
		if first != second {
			t.Errorf("%s is not deterministic", algorithm)
		}
	}
	if msg := runErr(t, jsvm, `crypto.derive("hkdf-md5", enc.decode("utf8","s"), enc.decode("utf8","s"), enc.decode("utf8",""), 32)`); !strings.Contains(msg, "unknown algorithm") {
		t.Errorf("unknown derive algorithm should throw, got %q", msg)
	}
}

// Different salts must produce different keys — the whole point of a salt.
func Test_Crypto_DeriveSaltMatters(t *testing.T) {
	jsvm := newTestRuntime(t)
	got := mustRun(t, jsvm, `
		var a = enc.encode("hex", crypto.derive("hkdf-sha256", enc.decode("utf8","secret"), enc.decode("utf8","salt-a"), enc.decode("utf8",""), 32));
		var b = enc.encode("hex", crypto.derive("hkdf-sha256", enc.decode("utf8","secret"), enc.decode("utf8","salt-b"), enc.decode("utf8",""), 32));
		a === b ? "SAME" : "different"
	`)
	if got.String() != "different" {
		t.Error("changing the salt did not change the derived key")
	}
}

func Test_Crypto_PasswordHashAndVerify(t *testing.T) {
	for _, algorithm := range []string{"bcrypt", "argon2id"} {
		jsvm := newTestRuntime(t)
		got := mustRun(t, jsvm, `
			var h = crypto.password.hash("`+algorithm+`", enc.decode("utf8", "correct horse"));
			var ok   = crypto.password.verify("`+algorithm+`", enc.decode("utf8", "correct horse"), h);
			var nope = crypto.password.verify("`+algorithm+`", enc.decode("utf8", "wrong horse"), h);
			ok + "," + nope
		`).String()
		if got != "true,false" {
			t.Errorf("%s verify(correct),verify(wrong) = %s, want true,false", algorithm, got)
		}
	}
}

// A password hash must be salted: the same password hashed twice differs.
func Test_Crypto_PasswordHashesAreSalted(t *testing.T) {
	for _, algorithm := range []string{"bcrypt", "argon2id"} {
		jsvm := newTestRuntime(t)
		got := mustRun(t, jsvm, `
			var a = crypto.password.hash("`+algorithm+`", enc.decode("utf8", "same password"));
			var b = crypto.password.hash("`+algorithm+`", enc.decode("utf8", "same password"));
			a === b ? "SAME" : "different"
		`).String()
		if got != "different" {
			t.Errorf("%s produced identical hashes for one password — not salted", algorithm)
		}
	}
}

// The argon2id encoding carries its own parameters, so a hash stays verifiable
// after the defaults here change.
func Test_Crypto_Argon2idEncodingIsSelfDescribing(t *testing.T) {
	jsvm := newTestRuntime(t)
	got := mustRun(t, jsvm, `crypto.password.hash("argon2id", enc.decode("utf8", "pw"))`).String()
	for _, part := range []string{"$argon2id$", "v=", "m=", "t=", "p="} {
		if !strings.Contains(got, part) {
			t.Errorf("argon2id hash %q is missing %q", got, part)
		}
	}
}

func Test_Crypto_EncryptWithoutNonceIsRandom(t *testing.T) {
	jsvm := newTestRuntime(t)
	src := `enc.encode("base64", crypto.encrypt("aes-gcm", enc.decode("utf8", "0123456789abcdef"), enc.decode("utf8", "payload")))`
	a := mustRun(t, jsvm, src).String()
	b := mustRun(t, jsvm, src).String()
	if a == b {
		t.Error("omitting the nonce should produce a random one, so ciphertext should differ")
	}
	jsvm.Set("sealed", a)
	got := mustRun(t, jsvm, `enc.encode("utf8", crypto.decrypt("aes-gcm", enc.decode("utf8", "0123456789abcdef"), enc.decode("base64", sealed)))`)
	if got.String() != "payload" {
		t.Errorf("randomly-nonced ciphertext did not decrypt: %q", got.String())
	}
}

func Test_Crypto_Sha3(t *testing.T) {
	jsvm := newTestRuntime(t)
	// SHA3-256 of "abc", the NIST test vector.
	got := mustRun(t, jsvm, `enc.encode("hex", crypto.hash("sha3-256", enc.decode("utf8", "abc")))`).String()
	const want = "3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532"
	if got != want {
		t.Errorf("sha3-256(abc) = %s, want %s", got, want)
	}
}
