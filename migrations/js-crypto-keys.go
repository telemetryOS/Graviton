package migrations

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"fmt"

	"github.com/dop251/goja"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/scrypt"
)

const (
	keyPairAlgorithms  = "ed25519, x25519, ecdsa-p256, ecdsa-p384, ecdsa-p521, ecdh-p256, ecdh-p384, ecdh-p521, rsa-2048, rsa-3072, rsa-4096"
	signAlgorithms     = "ed25519, ecdsa-sha256, ecdsa-sha384, ecdsa-sha512, rsa-pss-sha256, rsa-pkcs1-sha256"
	exchangeAlgorithms = "x25519, ecdh-p256, ecdh-p384, ecdh-p521"
	deriveAlgorithms   = "hkdf-sha256, hkdf-sha512, pbkdf2-sha256, pbkdf2-sha512, scrypt, argon2id"
	passwordAlgorithms = "bcrypt, argon2id"
)

// registerKeyFunctions adds the asymmetric surface, derivation and password
// hashing to the crypto global.
//
// Keys cross into JS as separate byte values rather than opaque handles: a
// public key and a private key are two independent values a migration can
// store, compare and pass around like any other bytes. They use the standard
// encodings — PKCS#8 for private keys, SPKI for public — so a key written by a
// migration is readable by any other language.
func registerKeyFunctions(jsvm *goja.Runtime, cryptoObj *goja.Object) {
	// random(n) -> bytes. Nonces, salts and IVs all need it, and without it
	// encrypt() could only be used by callers who derived a nonce themselves.
	cryptoObj.Set("random", func(call goja.FunctionCall) goja.Value {
		size := call.Argument(0).ToInteger()
		if size <= 0 || size > 1<<20 {
			panic(jsvm.NewGoError(fmt.Errorf("crypto.random needs a size between 1 and 1048576, got %d", size)))
		}
		raw := make([]byte, size)
		if _, err := rand.Read(raw); err != nil {
			panic(jsvm.NewGoError(err))
		}
		return jsvm.ToValue(jsvm.NewArrayBuffer(raw))
	})

	// generateKeyPair(algorithm) -> { publicKey, privateKey }
	cryptoObj.Set("generateKeyPair", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		pub, priv, err := generateKeyPair(algorithm)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		out := jsvm.NewObject()
		out.Set("publicKey", jsvm.NewArrayBuffer(pub))
		out.Set("privateKey", jsvm.NewArrayBuffer(priv))
		return out
	})

	cryptoObj.Set("sign", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		key, data, err := keyAndData(jsvm, call, "sign")
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		sig, err := signWith(algorithm, key, data)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		return jsvm.ToValue(jsvm.NewArrayBuffer(sig))
	})

	// verify returns a boolean rather than throwing: an invalid signature is an
	// expected outcome a migration branches on, not an error condition.
	cryptoObj.Set("verify", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		key, data, err := keyAndData(jsvm, call, "verify")
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		sig, err := BytesFromJS(jsvm, call.Argument(3))
		if err != nil {
			panic(jsvm.NewGoError(fmt.Errorf("crypto.verify signature: %w", err)))
		}
		ok, err := verifyWith(algorithm, key, data, sig)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		return jsvm.ToValue(ok)
	})

	// exchange(algorithm, privateKey, peerPublicKey) -> shared secret.
	// crypto/ecdh validates peer points and rejects low-order ones, which the
	// older curve25519 API left to the caller.
	cryptoObj.Set("exchange", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		priv, peer, err := keyAndData(jsvm, call, "exchange")
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		shared, err := exchangeWith(algorithm, priv, peer)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		return jsvm.ToValue(jsvm.NewArrayBuffer(shared))
	})

	// derive(algorithm, secret, salt, info, length) -> bytes.
	// info is unused by pbkdf2/scrypt/argon2id; pass "" there.
	cryptoObj.Set("derive", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		secret, err := BytesFromJS(jsvm, call.Argument(1))
		if err != nil {
			panic(jsvm.NewGoError(fmt.Errorf("crypto.derive secret: %w", err)))
		}
		salt, err := BytesFromJS(jsvm, call.Argument(2))
		if err != nil {
			panic(jsvm.NewGoError(fmt.Errorf("crypto.derive salt: %w", err)))
		}
		info, err := BytesFromJS(jsvm, call.Argument(3))
		if err != nil {
			panic(jsvm.NewGoError(fmt.Errorf("crypto.derive info: %w", err)))
		}
		length := int(call.Argument(4).ToInteger())
		if length <= 0 || length > 1<<16 {
			panic(jsvm.NewGoError(fmt.Errorf("crypto.derive needs a length between 1 and 65536, got %d", length)))
		}
		out, err := deriveWith(algorithm, secret, salt, info, length)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		return jsvm.ToValue(jsvm.NewArrayBuffer(out))
	})

	// password.hash / password.verify are separated from derive() on purpose.
	// They use the same primitives for a different job, with a self-describing
	// output that embeds its own parameters, and conflating them is how a KDF
	// ends up used where a password hash was meant.
	password := jsvm.NewObject()
	password.Set("hash", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		plain, err := BytesFromJS(jsvm, call.Argument(1))
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		encoded, err := passwordHash(algorithm, plain)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		return jsvm.ToValue(encoded)
	})
	password.Set("verify", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		plain, err := BytesFromJS(jsvm, call.Argument(1))
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		encoded := call.Argument(2).String()
		ok, err := passwordVerify(algorithm, plain, encoded)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		return jsvm.ToValue(ok)
	})
	cryptoObj.Set("password", password)
}

func generateKeyPair(algorithm string) (pub []byte, priv []byte, err error) {
	switch algorithm {
	case "ed25519":
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		return marshalPair(pubKey, privKey)
	case "x25519", "ecdh-p256", "ecdh-p384", "ecdh-p521":
		// ECDH keys are a distinct type from ECDSA keys over the same curve, so
		// the algorithm name has to say which is wanted rather than the curve.
		// A key generated as ecdsa-p256 cannot be used with exchange().
		curve := map[string]ecdh.Curve{
			"x25519":    ecdh.X25519(),
			"ecdh-p256": ecdh.P256(),
			"ecdh-p384": ecdh.P384(),
			"ecdh-p521": ecdh.P521(),
		}[algorithm]
		privKey, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		return marshalPair(privKey.PublicKey(), privKey)
	case "ecdsa-p256", "ecdsa-p384", "ecdsa-p521":
		curve := map[string]elliptic.Curve{
			"ecdsa-p256": elliptic.P256(),
			"ecdsa-p384": elliptic.P384(),
			"ecdsa-p521": elliptic.P521(),
		}[algorithm]
		privKey, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		return marshalPair(&privKey.PublicKey, privKey)
	case "rsa-2048", "rsa-3072", "rsa-4096":
		bits := map[string]int{"rsa-2048": 2048, "rsa-3072": 3072, "rsa-4096": 4096}[algorithm]
		privKey, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return nil, nil, err
		}
		return marshalPair(&privKey.PublicKey, privKey)
	default:
		return nil, nil, fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, keyPairAlgorithms)
	}
}

func marshalPair(pub any, priv any) ([]byte, []byte, error) {
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, nil, err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	return pubDER, privDER, nil
}

func parsePrivate(der []byte) (any, error) {
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("private key must be PKCS#8 DER (as generateKeyPair returns): %w", err)
	}
	return key, nil
}

func parsePublic(der []byte) (any, error) {
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("public key must be SPKI DER (as generateKeyPair returns): %w", err)
	}
	return key, nil
}

func signWith(algorithm string, keyDER []byte, data []byte) ([]byte, error) {
	key, err := parsePrivate(keyDER)
	if err != nil {
		return nil, err
	}
	switch algorithm {
	case "ed25519":
		priv, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("ed25519 signing needs an ed25519 private key, got %T", key)
		}
		return ed25519.Sign(priv, data), nil
	case "ecdsa-sha256", "ecdsa-sha384", "ecdsa-sha512":
		priv, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("ecdsa signing needs an ecdsa private key, got %T", key)
		}
		digest, _ := digestFor(algorithm, data)
		return ecdsa.SignASN1(rand.Reader, priv, digest)
	case "rsa-pss-sha256", "rsa-pkcs1-sha256":
		priv, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("rsa signing needs an rsa private key, got %T", key)
		}
		digest, h := digestFor(algorithm, data)
		if algorithm == "rsa-pss-sha256" {
			return rsa.SignPSS(rand.Reader, priv, h, digest, nil)
		}
		return rsa.SignPKCS1v15(rand.Reader, priv, h, digest)
	default:
		return nil, fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, signAlgorithms)
	}
}

func verifyWith(algorithm string, keyDER []byte, data []byte, sig []byte) (bool, error) {
	key, err := parsePublic(keyDER)
	if err != nil {
		return false, err
	}
	switch algorithm {
	case "ed25519":
		pub, ok := key.(ed25519.PublicKey)
		if !ok {
			return false, fmt.Errorf("ed25519 verification needs an ed25519 public key, got %T", key)
		}
		return ed25519.Verify(pub, data, sig), nil
	case "ecdsa-sha256", "ecdsa-sha384", "ecdsa-sha512":
		pub, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return false, fmt.Errorf("ecdsa verification needs an ecdsa public key, got %T", key)
		}
		digest, _ := digestFor(algorithm, data)
		return ecdsa.VerifyASN1(pub, digest, sig), nil
	case "rsa-pss-sha256", "rsa-pkcs1-sha256":
		pub, ok := key.(*rsa.PublicKey)
		if !ok {
			return false, fmt.Errorf("rsa verification needs an rsa public key, got %T", key)
		}
		digest, h := digestFor(algorithm, data)
		if algorithm == "rsa-pss-sha256" {
			return rsa.VerifyPSS(pub, h, digest, sig, nil) == nil, nil
		}
		return rsa.VerifyPKCS1v15(pub, h, digest, sig) == nil, nil
	default:
		return false, fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, signAlgorithms)
	}
}

func digestFor(algorithm string, data []byte) ([]byte, crypto.Hash) {
	switch algorithm {
	case "ecdsa-sha384":
		sum := sha512.Sum384(data)
		return sum[:], crypto.SHA384
	case "ecdsa-sha512":
		sum := sha512.Sum512(data)
		return sum[:], crypto.SHA512
	default:
		sum := sha256.Sum256(data)
		return sum[:], crypto.SHA256
	}
}

func exchangeWith(algorithm string, privDER []byte, peerDER []byte) ([]byte, error) {
	curve, ok := map[string]ecdh.Curve{
		"x25519":    ecdh.X25519(),
		"ecdh-p256": ecdh.P256(),
		"ecdh-p384": ecdh.P384(),
		"ecdh-p521": ecdh.P521(),
	}[algorithm]
	if !ok {
		return nil, fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, exchangeAlgorithms)
	}
	privAny, err := parsePrivate(privDER)
	if err != nil {
		return nil, err
	}
	priv, err := asECDHPrivate(algorithm, privAny)
	if err != nil {
		return nil, err
	}
	peerAny, err := parsePublic(peerDER)
	if err != nil {
		return nil, err
	}
	peer, err := asECDHPublic(algorithm, peerAny)
	if err != nil {
		return nil, err
	}
	if priv.Curve() != curve || peer.Curve() != curve {
		return nil, fmt.Errorf("keys are on a different curve than %s", algorithm)
	}
	return priv.ECDH(peer)
}

func deriveWith(algorithm string, secret, salt, info []byte, length int) ([]byte, error) {
	switch algorithm {
	case "hkdf-sha256":
		return hkdf.Key(sha256.New, secret, salt, string(info), length)
	case "hkdf-sha512":
		return hkdf.Key(sha512.New, secret, salt, string(info), length)
	case "pbkdf2-sha256":
		return pbkdf2.Key(sha256.New, string(secret), salt, 600_000, length)
	case "pbkdf2-sha512":
		return pbkdf2.Key(sha512.New, string(secret), salt, 210_000, length)
	case "scrypt":
		return scrypt.Key(secret, salt, 32768, 8, 1, length)
	case "argon2id":
		return argon2.IDKey(secret, salt, 3, 64*1024, 4, uint32(length)), nil
	default:
		return nil, fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, deriveAlgorithms)
	}
}

// passwordHash returns a self-describing string carrying its own parameters, so
// a stored hash stays verifiable after the defaults here change.
func passwordHash(algorithm string, plain []byte) (string, error) {
	switch algorithm {
	case "bcrypt":
		out, err := bcrypt.GenerateFromPassword(plain, bcrypt.DefaultCost)
		return string(out), err
	case "argon2id":
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return "", err
		}
		const time, memory, threads, keyLen = 3, 64 * 1024, 4, 32
		key := argon2.IDKey(plain, salt, time, memory, threads, keyLen)
		return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
			argon2.Version, memory, time, threads,
			rawB64(salt), rawB64(key)), nil
	default:
		return "", fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, passwordAlgorithms)
	}
}

func passwordVerify(algorithm string, plain []byte, encoded string) (bool, error) {
	switch algorithm {
	case "bcrypt":
		err := bcrypt.CompareHashAndPassword([]byte(encoded), plain)
		if err == bcrypt.ErrMismatchedHashAndPassword {
			return false, nil
		}
		return err == nil, err
	case "argon2id":
		var version, memory, time, threads int
		var saltB64, keyB64 string
		n, err := fmt.Sscanf(encoded, "$argon2id$v=%d$m=%d,t=%d,p=%d$%s",
			&version, &memory, &time, &threads, &saltB64)
		if err != nil || n != 5 {
			return false, fmt.Errorf("argon2id hash is not in the expected $argon2id$... form")
		}
		// Sscanf stops at the separator; split the trailing salt$key itself.
		for i := 0; i < len(saltB64); i++ {
			if saltB64[i] == '$' {
				keyB64 = saltB64[i+1:]
				saltB64 = saltB64[:i]
				break
			}
		}
		salt, err := unRawB64(saltB64)
		if err != nil {
			return false, err
		}
		want, err := unRawB64(keyB64)
		if err != nil {
			return false, err
		}
		got := argon2.IDKey(plain, salt, uint32(time), uint32(memory), uint8(threads), uint32(len(want)))
		return subtleEqual(got, want), nil
	default:
		return false, fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, passwordAlgorithms)
	}
}

// The argon2id encoding uses unpadded base64, as the reference implementation
// and every other argon2 library do, so hashes are portable.
func rawB64(raw []byte) string {
	return base64.RawStdEncoding.EncodeToString(raw)
}

func unRawB64(text string) ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(text)
}

// subtleEqual compares in constant time: a length- or content-dependent early
// exit would leak how much of a hash a guess matched.
func subtleEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// asECDHPrivate converts a parsed private key to its ECDH form.
//
// PKCS#8 encodes an ECDH P-curve key and an ECDSA P-curve key identically —
// same OID — so a key that has been through bytes always parses back as an
// *ecdsa.PrivateKey regardless of which it was generated as. Since keys cross
// into JS as bytes, that distinction cannot survive, and exchange converts
// rather than rejecting a key it has no way to tell apart. X25519 has its own
// OID and parses directly.
//
// Using one key for both signing and key exchange is poor practice, but the
// encoding gives us no way to detect it, so that judgement stays with the
// migration author.
func asECDHPrivate(algorithm string, key any) (*ecdh.PrivateKey, error) {
	switch k := key.(type) {
	case *ecdh.PrivateKey:
		return k, nil
	case *ecdsa.PrivateKey:
		converted, err := k.ECDH()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", algorithm, err)
		}
		return converted, nil
	default:
		return nil, fmt.Errorf("%s needs an ecdh or ecdsa private key, got %T — generate one with generateKeyPair(%q)", algorithm, key, algorithm)
	}
}

func asECDHPublic(algorithm string, key any) (*ecdh.PublicKey, error) {
	switch k := key.(type) {
	case *ecdh.PublicKey:
		return k, nil
	case *ecdsa.PublicKey:
		converted, err := k.ECDH()
		if err != nil {
			return nil, fmt.Errorf("%s peer key: %w", algorithm, err)
		}
		return converted, nil
	default:
		return nil, fmt.Errorf("%s needs an ecdh or ecdsa public key for the peer, got %T", algorithm, key)
	}
}
