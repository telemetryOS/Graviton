package migrations

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha3"
	"crypto/sha512"
	"fmt"
	"hash"
	"strings"

	"github.com/dop251/goja"
	"golang.org/x/crypto/chacha20poly1305"
)

// Algorithm names, listed in errors so a typo names the alternatives.
const (
	aeadAlgorithms    = "aes-gcm, chacha20-poly1305"
	asymEncAlgorithms = "rsa-oaep-sha256, rsa-oaep-sha512"
	hashAlgorithms    = "sha256, sha384, sha512, sha3-256, sha3-512, sha1"
)

// JSCrypto builds the `crypto` global. Every function takes the algorithm as
// its first argument, so a call names the operation it performs:
//
//	crypto.decrypt("aes-gcm", key, ciphertext)          -> bytes
//	crypto.encrypt("aes-gcm", key, plaintext, nonce)    -> bytes
//	crypto.hash("sha256", data)                         -> bytes
//	crypto.hmac("sha256", key, data)                    -> bytes
//
// Keys and data are bytes, not encoded strings — `enc` does every conversion,
// so nothing here has to guess whether an argument arrived base64 or raw.
//
// Two deliberate constraints:
//
// An unknown algorithm throws. Silently falling back would be the worst
// outcome, since a migration writes its result to the target database.
//
// encrypt requires an explicit nonce. Generating a random one would make a
// migration non-convergent: re-running it would produce different ciphertext
// for unchanged input, rewriting rows that did not change and breaking any
// provenance comparison against what was written last time. Callers that need
// encryption derive a nonce deterministically from the data they are
// encrypting.
func JSCrypto(jsvm *goja.Runtime) *goja.Object {
	cryptoObj := jsvm.NewObject()

	cryptoObj.Set("decrypt", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		key, data, err := keyAndData(jsvm, call, "decrypt")
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		if isAsymmetric(algorithm) {
			plain, err := rsaDecrypt(algorithm, key, data)
			if err != nil {
				panic(jsvm.NewGoError(err))
			}
			return jsvm.ToValue(jsvm.NewArrayBuffer(plain))
		}
		aead, err := newAEAD(algorithm, key)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		if len(data) < aead.NonceSize() {
			panic(jsvm.NewGoError(fmt.Errorf(
				"ciphertext is %d bytes, shorter than the %d-byte %s nonce it must begin with",
				len(data), aead.NonceSize(), algorithm)))
		}
		nonce, sealed := data[:aead.NonceSize()], data[aead.NonceSize():]
		plain, err := aead.Open(nil, nonce, sealed, nil)
		if err != nil {
			// Authenticated: this is a wrong key or altered ciphertext, never
			// silently wrong plaintext.
			panic(jsvm.NewGoError(fmt.Errorf("%s decryption failed: %w", algorithm, err)))
		}
		return jsvm.ToValue(jsvm.NewArrayBuffer(plain))
	})

	cryptoObj.Set("encrypt", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		key, data, err := keyAndData(jsvm, call, "encrypt")
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		if isAsymmetric(algorithm) {
			sealed, err := rsaEncrypt(algorithm, key, data)
			if err != nil {
				panic(jsvm.NewGoError(err))
			}
			return jsvm.ToValue(jsvm.NewArrayBuffer(sealed))
		}
		aead, err := newAEAD(algorithm, key)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		// The nonce is optional. Supplying one makes the output reproducible;
		// omitting it generates a random one, so re-running produces different
		// ciphertext for the same input. Which of those a migration wants is the
		// author's call, so neither is forced here.
		nonceVal := call.Argument(3)
		var nonce []byte
		if goja.IsUndefined(nonceVal) || goja.IsNull(nonceVal) {
			nonce = make([]byte, aead.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				panic(jsvm.NewGoError(err))
			}
		} else if nonce, err = BytesFromJS(jsvm, nonceVal); err != nil {
			panic(jsvm.NewGoError(fmt.Errorf("nonce: %w", err)))
		}
		if len(nonce) != aead.NonceSize() {
			panic(jsvm.NewGoError(fmt.Errorf(
				"%s needs a %d-byte nonce, got %d", algorithm, aead.NonceSize(), len(nonce))))
		}
		sealed := aead.Seal(nil, nonce, data, nil)
		// Nonce first, matching what decrypt expects.
		return jsvm.ToValue(jsvm.NewArrayBuffer(append(append([]byte{}, nonce...), sealed...)))
	})

	cryptoObj.Set("hash", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		data, err := BytesFromJS(jsvm, call.Argument(1))
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		h, err := newHash(algorithm)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		h.Write(data)
		return jsvm.ToValue(jsvm.NewArrayBuffer(h.Sum(nil)))
	})

	cryptoObj.Set("hmac", func(call goja.FunctionCall) goja.Value {
		algorithm := call.Argument(0).String()
		key, data, err := keyAndData(jsvm, call, "hmac")
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		// Validate the algorithm before building the MAC, so an unknown name
		// reports itself rather than surfacing as a nil hash inside hmac.New.
		if _, err := newHash(algorithm); err != nil {
			panic(jsvm.NewGoError(err))
		}
		mac := hmac.New(func() hash.Hash { h, _ := newHash(algorithm); return h }, key)
		mac.Write(data)
		return jsvm.ToValue(jsvm.NewArrayBuffer(mac.Sum(nil)))
	})

	registerKeyFunctions(jsvm, cryptoObj)

	return cryptoObj
}

// keyAndData reads the (key, data) pair every keyed operation shares.
func keyAndData(jsvm *goja.Runtime, call goja.FunctionCall, op string) ([]byte, []byte, error) {
	key, err := BytesFromJS(jsvm, call.Argument(1))
	if err != nil {
		return nil, nil, fmt.Errorf("crypto.%s key: %w", op, err)
	}
	data, err := BytesFromJS(jsvm, call.Argument(2))
	if err != nil {
		return nil, nil, fmt.Errorf("crypto.%s data: %w", op, err)
	}
	return key, data, nil
}

// newAEAD builds the cipher for an authenticated algorithm. AES accepts 16, 24
// or 32 byte keys (AES-128/192/256) — the legacy TelemetryOS device keys are 16
// bytes, so rejecting anything but 32 would lock that data out.
func newAEAD(algorithm string, key []byte) (cipher.AEAD, error) {
	switch algorithm {
	case "aes-gcm":
		switch len(key) {
		case 16, 24, 32:
		default:
			return nil, fmt.Errorf("aes-gcm needs a 16, 24 or 32 byte key, got %d", len(key))
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	case "chacha20-poly1305":
		if len(key) != chacha20poly1305.KeySize {
			return nil, fmt.Errorf("chacha20-poly1305 needs a %d byte key, got %d",
				chacha20poly1305.KeySize, len(key))
		}
		return chacha20poly1305.New(key)
	default:
		return nil, fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, aeadAlgorithms)
	}
}

func newHash(algorithm string) (hash.Hash, error) {
	switch algorithm {
	case "sha256":
		return sha256.New(), nil
	case "sha384":
		return sha512.New384(), nil
	case "sha512":
		return sha512.New(), nil
	case "sha3-256":
		return sha3.New256(), nil
	case "sha3-512":
		return sha3.New512(), nil
	case "sha1":
		// Present for reading legacy digests, not for new ones.
		return sha1.New(), nil
	default:
		return nil, fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, hashAlgorithms)
	}
}

// isAsymmetric reports whether an algorithm name selects public-key encryption
// rather than a symmetric AEAD. encrypt and decrypt serve both: the algorithm
// decides how the key argument is interpreted, so there is no second namespace
// and no separate pair of functions to remember.
func isAsymmetric(algorithm string) bool {
	return strings.HasPrefix(algorithm, "rsa-oaep-")
}

func oaepHash(algorithm string) (hash.Hash, error) {
	switch algorithm {
	case "rsa-oaep-sha256":
		return sha256.New(), nil
	case "rsa-oaep-sha512":
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("unknown algorithm %q; supported: %s", algorithm, asymEncAlgorithms)
	}
}

// rsaEncrypt takes an SPKI public key, as generateKeyPair returns.
func rsaEncrypt(algorithm string, keyDER []byte, data []byte) ([]byte, error) {
	h, err := oaepHash(algorithm)
	if err != nil {
		return nil, err
	}
	keyAny, err := parsePublic(keyDER)
	if err != nil {
		return nil, err
	}
	pub, ok := keyAny.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s needs an rsa public key, got %T", algorithm, keyAny)
	}
	return rsa.EncryptOAEP(h, rand.Reader, pub, data, nil)
}

// rsaDecrypt takes a PKCS#8 private key, as generateKeyPair returns.
func rsaDecrypt(algorithm string, keyDER []byte, data []byte) ([]byte, error) {
	h, err := oaepHash(algorithm)
	if err != nil {
		return nil, err
	}
	keyAny, err := parsePrivate(keyDER)
	if err != nil {
		return nil, err
	}
	priv, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s needs an rsa private key, got %T", algorithm, keyAny)
	}
	plain, err := rsa.DecryptOAEP(h, rand.Reader, priv, data, nil)
	if err != nil {
		return nil, fmt.Errorf("%s decryption failed: %w", algorithm, err)
	}
	return plain, nil
}
