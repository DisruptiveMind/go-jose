/*-
 * Copyright 2014 Square Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package jose

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"testing"
)

// We generate only a single RSA and EC key for testing, speeds up tests.
var rsaTestKey, _ = rsa.GenerateKey(rand.Reader, 2048)

var ecTestKey256, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
var ecTestKey384, _ = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
var ecTestKey521, _ = ecdsa.GenerateKey(elliptic.P521(), rand.Reader)

func RoundtripJWE(keyAlg KeyAlgorithm, encAlg ContentEncryption, compressionAlg CompressionAlgorithm, serializer func(*JsonWebEncryption) (string, error), corrupter func(*JsonWebEncryption) bool, aad []byte, encryptionKey interface{}, decryptionKey interface{}) error {
	enc, err := NewEncrypter(keyAlg, encAlg, encryptionKey)
	if err != nil {
		return fmt.Errorf("error on new encrypter: %s", err)
	}

	enc.SetCompression(compressionAlg)

	input := []byte("Lorem ipsum dolor sit amet")
	obj, err := enc.EncryptWithAuthData(input, aad)
	if err != nil {
		return fmt.Errorf("error in encrypt: %s", err)
	}

	msg, err := serializer(obj)
	if err != nil {
		return fmt.Errorf("error in serializer: %s", err)
	}

	parsed, err := ParseEncrypted(msg)
	if err != nil {
		return fmt.Errorf("error in parse: %s, on msg '%s'", err, msg)
	}

	// (Maybe) mangle object
	skip := corrupter(parsed)
	if skip {
		return fmt.Errorf("corrupter indicated message should be skipped")
	}

	if bytes.Compare(parsed.GetAuthData(), aad) != 0 {
		return fmt.Errorf("auth data in parsed object does not match")
	}

	output, err := parsed.Decrypt(decryptionKey)
	if err != nil {
		return fmt.Errorf("error on decrypt: %s", err)
	}

	if bytes.Compare(input, output) != 0 {
		return fmt.Errorf("Decrypted output does not match input, got '%s' but wanted '%s'", output, input)
	}

	return nil
}

func TestRoundtripsJWE(t *testing.T) {
	// Test matrix
	keyAlgs := []KeyAlgorithm{
		DIRECT, ECDH_ES, ECDH_ES_A128KW, ECDH_ES_A192KW, ECDH_ES_A256KW, A128KW, A192KW, A256KW,
		RSA1_5, RSA_OAEP, RSA_OAEP_256, A128GCMKW, A192GCMKW, A256GCMKW}
	encAlgs := []ContentEncryption{A128GCM, A192GCM, A256GCM, A128CBC_HS256, A192CBC_HS384, A256CBC_HS512}
	zipAlgs := []CompressionAlgorithm{NONE, DEFLATE}

	serializers := []func(*JsonWebEncryption) (string, error){
		func(obj *JsonWebEncryption) (string, error) { return obj.CompactSerialize() },
		func(obj *JsonWebEncryption) (string, error) { return obj.FullSerialize(), nil },
	}

	corrupter := func(obj *JsonWebEncryption) bool { return false }

	// Note: can't use AAD with compact serialization
	aads := [][]byte{
		nil,
		[]byte("Ut enim ad minim veniam"),
	}

	// Test all different configurations
	for _, alg := range keyAlgs {
		for _, enc := range encAlgs {
			for _, key := range generateTestKeys(alg, enc) {
				for _, zip := range zipAlgs {
					for i, serializer := range serializers {
						err := RoundtripJWE(alg, enc, zip, serializer, corrupter, aads[i], key.enc, key.dec)
						if err != nil {
							t.Error(err, alg, enc, zip, i)
						}
					}
				}
			}
		}
	}
}

func TestRoundtripsJWECorrupted(t *testing.T) {
	// Test matrix
	keyAlgs := []KeyAlgorithm{DIRECT, ECDH_ES, ECDH_ES_A128KW, A128KW, RSA1_5, RSA_OAEP, RSA_OAEP_256, A128GCMKW}
	encAlgs := []ContentEncryption{A128GCM, A192GCM, A256GCM, A128CBC_HS256, A192CBC_HS384, A256CBC_HS512}
	zipAlgs := []CompressionAlgorithm{NONE, DEFLATE}

	serializers := []func(*JsonWebEncryption) (string, error){
		func(obj *JsonWebEncryption) (string, error) { return obj.CompactSerialize() },
		func(obj *JsonWebEncryption) (string, error) { return obj.FullSerialize(), nil },
	}

	bitflip := func(slice []byte) bool {
		if len(slice) > 0 {
			slice[0] ^= 0xFF
			return false
		}
		return true
	}

	corrupters := []func(*JsonWebEncryption) bool{
		func(obj *JsonWebEncryption) bool {
			// Set invalid ciphertext
			return bitflip(obj.ciphertext)
		},
		func(obj *JsonWebEncryption) bool {
			// Set invalid auth tag
			return bitflip(obj.tag)
		},
		func(obj *JsonWebEncryption) bool {
			// Set invalid AAD
			return bitflip(obj.aad)
		},
		func(obj *JsonWebEncryption) bool {
			// Mess with encrypted key
			return bitflip(obj.recipients[0].encryptedKey)
		},
		func(obj *JsonWebEncryption) bool {
			// Mess with GCM-KW auth tag
			return bitflip(obj.protected.Tag.bytes())
		},
	}

	// Note: can't use AAD with compact serialization
	aads := [][]byte{
		nil,
		[]byte("Ut enim ad minim veniam"),
	}

	// Test all different configurations
	for _, alg := range keyAlgs {
		for _, enc := range encAlgs {
			for _, key := range generateTestKeys(alg, enc) {
				for _, zip := range zipAlgs {
					for i, serializer := range serializers {
						for j, corrupter := range corrupters {
							err := RoundtripJWE(alg, enc, zip, serializer, corrupter, aads[i], key.enc, key.dec)
							if err == nil {
								t.Error("failed to detect corrupt data", err, alg, enc, zip, i, j)
							}
						}
					}
				}
			}
		}
	}
}

func TestNewEncrypterErrors(t *testing.T) {
	_, err := NewEncrypter("XYZ", "XYZ", nil)
	if err == nil {
		t.Error("was able to instantiate encrypter with invalid cipher")
	}

	_, err = NewMultiEncrypter("XYZ")
	if err == nil {
		t.Error("was able to instantiate multi-encrypter with invalid cipher")
	}

	_, err = NewEncrypter(DIRECT, A128GCM, nil)
	if err == nil {
		t.Error("was able to instantiate encrypter with invalid direct key")
	}

	_, err = NewEncrypter(ECDH_ES, A128GCM, nil)
	if err == nil {
		t.Error("was able to instantiate encrypter with invalid EC key")
	}
}

func TestMultiRecipientJWE(t *testing.T) {
	enc, err := NewMultiEncrypter(A128GCM)
	if err != nil {
		panic(err)
	}

	err = enc.AddRecipient(RSA_OAEP, &rsaTestKey.PublicKey)
	if err != nil {
		t.Error("error when adding RSA recipient", err)
	}

	sharedKey := []byte{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	}

	err = enc.AddRecipient(A256GCMKW, sharedKey)
	if err != nil {
		t.Error("error when adding AES recipient: ", err)
		return
	}

	input := []byte("Lorem ipsum dolor sit amet")
	obj, err := enc.Encrypt(input)
	if err != nil {
		t.Error("error in encrypt: ", err)
		return
	}

	msg := obj.FullSerialize()

	parsed, err := ParseEncrypted(msg)
	if err != nil {
		t.Error("error in parse: ", err)
		return
	}

	output, err := parsed.Decrypt(rsaTestKey)
	if err != nil {
		t.Error("error on decrypt with RSA: ", err)
		return
	}

	if bytes.Compare(input, output) != 0 {
		t.Error("Decrypted output does not match input: ", output, input)
		return
	}

	output, err = parsed.Decrypt(sharedKey)
	if err != nil {
		t.Error("error on decrypt with AES: ", err)
		return
	}

	if bytes.Compare(input, output) != 0 {
		t.Error("Decrypted output does not match input", output, input)
		return
	}
}

func TestMultiRecipientErrors(t *testing.T) {
	enc, err := NewMultiEncrypter(A128GCM)
	if err != nil {
		panic(err)
	}

	input := []byte("Lorem ipsum dolor sit amet")
	_, err = enc.Encrypt(input)
	if err == nil {
		t.Error("should fail when encrypting to zero recipients")
	}

	err = enc.AddRecipient(DIRECT, nil)
	if err == nil {
		t.Error("should reject DIRECT mode when encrypting to multiple recipients")
	}

	err = enc.AddRecipient(ECDH_ES, nil)
	if err == nil {
		t.Error("should reject ECDH_ES mode when encrypting to multiple recipients")
	}

	err = enc.AddRecipient(RSA1_5, nil)
	if err == nil {
		t.Error("should reject invalid recipient key")
	}
}

type testKey struct {
	enc, dec interface{}
}

func symmetricTestKey(size int) []testKey {
	key, _, _ := randomKeyGenerator{size: size}.genKey()

	return []testKey{
		testKey{
			enc: key,
			dec: key,
		},
	}
}

func generateTestKeys(keyAlg KeyAlgorithm, encAlg ContentEncryption) []testKey {
	switch keyAlg {
	case DIRECT:
		return symmetricTestKey(getContentCipher(encAlg).keySize())
	case ECDH_ES, ECDH_ES_A128KW, ECDH_ES_A192KW, ECDH_ES_A256KW:
		return []testKey{
			testKey{
				dec: ecTestKey256,
				enc: &ecTestKey256.PublicKey,
			},
			testKey{
				dec: ecTestKey384,
				enc: &ecTestKey384.PublicKey,
			},
			testKey{
				dec: ecTestKey521,
				enc: &ecTestKey521.PublicKey,
			},
		}
	case A128GCMKW, A128KW:
		return symmetricTestKey(16)
	case A192GCMKW, A192KW:
		return symmetricTestKey(24)
	case A256GCMKW, A256KW:
		return symmetricTestKey(32)
	case RSA1_5, RSA_OAEP, RSA_OAEP_256:
		return []testKey{testKey{
			dec: rsaTestKey,
			enc: &rsaTestKey.PublicKey,
		}}
	}

	panic("Must update test case")
}

func RunRoundtripsJWE(b *testing.B, alg KeyAlgorithm, enc ContentEncryption, zip CompressionAlgorithm, priv, pub interface{}) {
	serializer := func(obj *JsonWebEncryption) (string, error) {
		return obj.CompactSerialize()
	}

	corrupter := func(obj *JsonWebEncryption) bool { return false }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := RoundtripJWE(alg, enc, zip, serializer, corrupter, nil, pub, priv)
		if err != nil {
			b.Error(err)
		}
	}
}

var (
	chunks = map[string][]byte{
		"1B":   make([]byte, 1),
		"64B":  make([]byte, 64),
		"1KB":  make([]byte, 1024),
		"64KB": make([]byte, 65536),
		"1MB":  make([]byte, 1048576),
		"64MB": make([]byte, 67108864),
	}

	symKey, _, _ = randomKeyGenerator{size: 32}.genKey()

	encrypters = map[string]Encrypter{
		"OAEPAndGCM":          mustEncrypter(RSA_OAEP, A128GCM, &rsaTestKey.PublicKey),
		"PKCSAndGCM":          mustEncrypter(RSA1_5, A128GCM, &rsaTestKey.PublicKey),
		"OAEPAndCBC":          mustEncrypter(RSA_OAEP, A128CBC_HS256, &rsaTestKey.PublicKey),
		"PKCSAndCBC":          mustEncrypter(RSA1_5, A128CBC_HS256, &rsaTestKey.PublicKey),
		"DirectGCM128":        mustEncrypter(DIRECT, A128GCM, symKey),
		"DirectCBC128":        mustEncrypter(DIRECT, A128CBC_HS256, symKey),
		"DirectGCM256":        mustEncrypter(DIRECT, A256GCM, symKey),
		"DirectCBC256":        mustEncrypter(DIRECT, A256CBC_HS512, symKey),
		"AESKWAndGCM128":      mustEncrypter(A128KW, A128GCM, symKey),
		"AESKWAndCBC256":      mustEncrypter(A256KW, A256GCM, symKey),
		"ECDHOnP256AndGCM128": mustEncrypter(ECDH_ES, A128GCM, &ecTestKey256.PublicKey),
		"ECDHOnP384AndGCM128": mustEncrypter(ECDH_ES, A128GCM, &ecTestKey384.PublicKey),
		"ECDHOnP521AndGCM128": mustEncrypter(ECDH_ES, A128GCM, &ecTestKey521.PublicKey),
	}
)

func BenchmarkEncrypt1BWithOAEPAndGCM(b *testing.B)   { benchEncrypt("1B", "OAEPAndGCM", b) }
func BenchmarkEncrypt64BWithOAEPAndGCM(b *testing.B)  { benchEncrypt("64B", "OAEPAndGCM", b) }
func BenchmarkEncrypt1KBWithOAEPAndGCM(b *testing.B)  { benchEncrypt("1KB", "OAEPAndGCM", b) }
func BenchmarkEncrypt64KBWithOAEPAndGCM(b *testing.B) { benchEncrypt("64KB", "OAEPAndGCM", b) }
func BenchmarkEncrypt1MBWithOAEPAndGCM(b *testing.B)  { benchEncrypt("1MB", "OAEPAndGCM", b) }
func BenchmarkEncrypt64MBWithOAEPAndGCM(b *testing.B) { benchEncrypt("64MB", "OAEPAndGCM", b) }

func BenchmarkEncrypt1BWithPKCSAndGCM(b *testing.B)   { benchEncrypt("1B", "PKCSAndGCM", b) }
func BenchmarkEncrypt64BWithPKCSAndGCM(b *testing.B)  { benchEncrypt("64B", "PKCSAndGCM", b) }
func BenchmarkEncrypt1KBWithPKCSAndGCM(b *testing.B)  { benchEncrypt("1KB", "PKCSAndGCM", b) }
func BenchmarkEncrypt64KBWithPKCSAndGCM(b *testing.B) { benchEncrypt("64KB", "PKCSAndGCM", b) }
func BenchmarkEncrypt1MBWithPKCSAndGCM(b *testing.B)  { benchEncrypt("1MB", "PKCSAndGCM", b) }
func BenchmarkEncrypt64MBWithPKCSAndGCM(b *testing.B) { benchEncrypt("64MB", "PKCSAndGCM", b) }

func BenchmarkEncrypt1BWithOAEPAndCBC(b *testing.B)   { benchEncrypt("1B", "OAEPAndCBC", b) }
func BenchmarkEncrypt64BWithOAEPAndCBC(b *testing.B)  { benchEncrypt("64B", "OAEPAndCBC", b) }
func BenchmarkEncrypt1KBWithOAEPAndCBC(b *testing.B)  { benchEncrypt("1KB", "OAEPAndCBC", b) }
func BenchmarkEncrypt64KBWithOAEPAndCBC(b *testing.B) { benchEncrypt("64KB", "OAEPAndCBC", b) }
func BenchmarkEncrypt1MBWithOAEPAndCBC(b *testing.B)  { benchEncrypt("1MB", "OAEPAndCBC", b) }
func BenchmarkEncrypt64MBWithOAEPAndCBC(b *testing.B) { benchEncrypt("64MB", "OAEPAndCBC", b) }

func BenchmarkEncrypt1BWithPKCSAndCBC(b *testing.B)   { benchEncrypt("1B", "PKCSAndCBC", b) }
func BenchmarkEncrypt64BWithPKCSAndCBC(b *testing.B)  { benchEncrypt("64B", "PKCSAndCBC", b) }
func BenchmarkEncrypt1KBWithPKCSAndCBC(b *testing.B)  { benchEncrypt("1KB", "PKCSAndCBC", b) }
func BenchmarkEncrypt64KBWithPKCSAndCBC(b *testing.B) { benchEncrypt("64KB", "PKCSAndCBC", b) }
func BenchmarkEncrypt1MBWithPKCSAndCBC(b *testing.B)  { benchEncrypt("1MB", "PKCSAndCBC", b) }
func BenchmarkEncrypt64MBWithPKCSAndCBC(b *testing.B) { benchEncrypt("64MB", "PKCSAndCBC", b) }

func BenchmarkEncrypt1BWithDirectGCM128(b *testing.B)   { benchEncrypt("1B", "DirectGCM128", b) }
func BenchmarkEncrypt64BWithDirectGCM128(b *testing.B)  { benchEncrypt("64B", "DirectGCM128", b) }
func BenchmarkEncrypt1KBWithDirectGCM128(b *testing.B)  { benchEncrypt("1KB", "DirectGCM128", b) }
func BenchmarkEncrypt64KBWithDirectGCM128(b *testing.B) { benchEncrypt("64KB", "DirectGCM128", b) }
func BenchmarkEncrypt1MBWithDirectGCM128(b *testing.B)  { benchEncrypt("1MB", "DirectGCM128", b) }
func BenchmarkEncrypt64MBWithDirectGCM128(b *testing.B) { benchEncrypt("64MB", "DirectGCM128", b) }

func BenchmarkEncrypt1BWithDirectCBC128(b *testing.B)   { benchEncrypt("1B", "DirectCBC128", b) }
func BenchmarkEncrypt64BWithDirectCBC128(b *testing.B)  { benchEncrypt("64B", "DirectCBC128", b) }
func BenchmarkEncrypt1KBWithDirectCBC128(b *testing.B)  { benchEncrypt("1KB", "DirectCBC128", b) }
func BenchmarkEncrypt64KBWithDirectCBC128(b *testing.B) { benchEncrypt("64KB", "DirectCBC128", b) }
func BenchmarkEncrypt1MBWithDirectCBC128(b *testing.B)  { benchEncrypt("1MB", "DirectCBC128", b) }
func BenchmarkEncrypt64MBWithDirectCBC128(b *testing.B) { benchEncrypt("64MB", "DirectCBC128", b) }

func BenchmarkEncrypt1BWithDirectGCM256(b *testing.B)   { benchEncrypt("1B", "DirectGCM256", b) }
func BenchmarkEncrypt64BWithDirectGCM256(b *testing.B)  { benchEncrypt("64B", "DirectGCM256", b) }
func BenchmarkEncrypt1KBWithDirectGCM256(b *testing.B)  { benchEncrypt("1KB", "DirectGCM256", b) }
func BenchmarkEncrypt64KBWithDirectGCM256(b *testing.B) { benchEncrypt("64KB", "DirectGCM256", b) }
func BenchmarkEncrypt1MBWithDirectGCM256(b *testing.B)  { benchEncrypt("1MB", "DirectGCM256", b) }
func BenchmarkEncrypt64MBWithDirectGCM256(b *testing.B) { benchEncrypt("64MB", "DirectGCM256", b) }

func BenchmarkEncrypt1BWithDirectCBC256(b *testing.B)   { benchEncrypt("1B", "DirectCBC256", b) }
func BenchmarkEncrypt64BWithDirectCBC256(b *testing.B)  { benchEncrypt("64B", "DirectCBC256", b) }
func BenchmarkEncrypt1KBWithDirectCBC256(b *testing.B)  { benchEncrypt("1KB", "DirectCBC256", b) }
func BenchmarkEncrypt64KBWithDirectCBC256(b *testing.B) { benchEncrypt("64KB", "DirectCBC256", b) }
func BenchmarkEncrypt1MBWithDirectCBC256(b *testing.B)  { benchEncrypt("1MB", "DirectCBC256", b) }
func BenchmarkEncrypt64MBWithDirectCBC256(b *testing.B) { benchEncrypt("64MB", "DirectCBC256", b) }

func BenchmarkEncrypt1BWithAESKWAndGCM128(b *testing.B)   { benchEncrypt("1B", "AESKWAndGCM128", b) }
func BenchmarkEncrypt64BWithAESKWAndGCM128(b *testing.B)  { benchEncrypt("64B", "AESKWAndGCM128", b) }
func BenchmarkEncrypt1KBWithAESKWAndGCM128(b *testing.B)  { benchEncrypt("1KB", "AESKWAndGCM128", b) }
func BenchmarkEncrypt64KBWithAESKWAndGCM128(b *testing.B) { benchEncrypt("64KB", "AESKWAndGCM128", b) }
func BenchmarkEncrypt1MBWithAESKWAndGCM128(b *testing.B)  { benchEncrypt("1MB", "AESKWAndGCM128", b) }
func BenchmarkEncrypt64MBWithAESKWAndGCM128(b *testing.B) { benchEncrypt("64MB", "AESKWAndGCM128", b) }

func BenchmarkEncrypt1BWithAESKWAndCBC256(b *testing.B)   { benchEncrypt("1B", "AESKWAndCBC256", b) }
func BenchmarkEncrypt64BWithAESKWAndCBC256(b *testing.B)  { benchEncrypt("64B", "AESKWAndCBC256", b) }
func BenchmarkEncrypt1KBWithAESKWAndCBC256(b *testing.B)  { benchEncrypt("1KB", "AESKWAndCBC256", b) }
func BenchmarkEncrypt64KBWithAESKWAndCBC256(b *testing.B) { benchEncrypt("64KB", "AESKWAndCBC256", b) }
func BenchmarkEncrypt1MBWithAESKWAndCBC256(b *testing.B)  { benchEncrypt("1MB", "AESKWAndCBC256", b) }
func BenchmarkEncrypt64MBWithAESKWAndCBC256(b *testing.B) { benchEncrypt("64MB", "AESKWAndCBC256", b) }

func BenchmarkEncrypt1BWithECDHOnP256AndGCM128(b *testing.B) {
	benchEncrypt("1B", "ECDHOnP256AndGCM128", b)
}
func BenchmarkEncrypt64BWithECDHOnP256AndGCM128(b *testing.B) {
	benchEncrypt("64B", "ECDHOnP256AndGCM128", b)
}
func BenchmarkEncrypt1KBWithECDHOnP256AndGCM128(b *testing.B) {
	benchEncrypt("1KB", "ECDHOnP256AndGCM128", b)
}
func BenchmarkEncrypt64KBWithECDHOnP256AndGCM128(b *testing.B) {
	benchEncrypt("64KB", "ECDHOnP256AndGCM128", b)
}
func BenchmarkEncrypt1MBWithECDHOnP256AndGCM128(b *testing.B) {
	benchEncrypt("1MB", "ECDHOnP256AndGCM128", b)
}
func BenchmarkEncrypt64MBWithECDHOnP256AndGCM128(b *testing.B) {
	benchEncrypt("64MB", "ECDHOnP256AndGCM128", b)
}

func BenchmarkEncrypt1BWithECDHOnP384AndGCM128(b *testing.B) {
	benchEncrypt("1B", "ECDHOnP384AndGCM128", b)
}
func BenchmarkEncrypt64BWithECDHOnP384AndGCM128(b *testing.B) {
	benchEncrypt("64B", "ECDHOnP384AndGCM128", b)
}
func BenchmarkEncrypt1KBWithECDHOnP384AndGCM128(b *testing.B) {
	benchEncrypt("1KB", "ECDHOnP384AndGCM128", b)
}
func BenchmarkEncrypt64KBWithECDHOnP384AndGCM128(b *testing.B) {
	benchEncrypt("64KB", "ECDHOnP384AndGCM128", b)
}
func BenchmarkEncrypt1MBWithECDHOnP384AndGCM128(b *testing.B) {
	benchEncrypt("1MB", "ECDHOnP384AndGCM128", b)
}
func BenchmarkEncrypt64MBWithECDHOnP384AndGCM128(b *testing.B) {
	benchEncrypt("64MB", "ECDHOnP384AndGCM128", b)
}

func BenchmarkEncrypt1BWithECDHOnP521AndGCM128(b *testing.B) {
	benchEncrypt("1B", "ECDHOnP521AndGCM128", b)
}
func BenchmarkEncrypt64BWithECDHOnP521AndGCM128(b *testing.B) {
	benchEncrypt("64B", "ECDHOnP521AndGCM128", b)
}
func BenchmarkEncrypt1KBWithECDHOnP521AndGCM128(b *testing.B) {
	benchEncrypt("1KB", "ECDHOnP521AndGCM128", b)
}
func BenchmarkEncrypt64KBWithECDHOnP521AndGCM128(b *testing.B) {
	benchEncrypt("64KB", "ECDHOnP521AndGCM128", b)
}
func BenchmarkEncrypt1MBWithECDHOnP521AndGCM128(b *testing.B) {
	benchEncrypt("1MB", "ECDHOnP521AndGCM128", b)
}
func BenchmarkEncrypt64MBWithECDHOnP521AndGCM128(b *testing.B) {
	benchEncrypt("64MB", "ECDHOnP521AndGCM128", b)
}

func benchEncrypt(chunkKey, primKey string, b *testing.B) {
	data, ok := chunks[chunkKey]
	if !ok {
		b.Fatalf("unknown chunk size %s", chunkKey)
	}

	enc, ok := encrypters[primKey]
	if !ok {
		b.Fatalf("unknown encrypter %s", primKey)
	}

	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		enc.Encrypt(data)
	}
}

var (
	decryptionKeys = map[string]interface{}{
		"OAEPAndGCM": rsaTestKey,
		"PKCSAndGCM": rsaTestKey,
		"OAEPAndCBC": rsaTestKey,
		"PKCSAndCBC": rsaTestKey,

		"DirectGCM128": symKey,
		"DirectCBC128": symKey,
		"DirectGCM256": symKey,
		"DirectCBC256": symKey,

		"AESKWAndGCM128": symKey,
		"AESKWAndCBC256": symKey,

		"ECDHOnP256AndGCM128": ecTestKey256,
		"ECDHOnP384AndGCM128": ecTestKey384,
		"ECDHOnP521AndGCM128": ecTestKey521,
	}
)

func BenchmarkDecrypt1BWithOAEPAndGCM(b *testing.B)   { benchDecrypt("1B", "OAEPAndGCM", b) }
func BenchmarkDecrypt64BWithOAEPAndGCM(b *testing.B)  { benchDecrypt("64B", "OAEPAndGCM", b) }
func BenchmarkDecrypt1KBWithOAEPAndGCM(b *testing.B)  { benchDecrypt("1KB", "OAEPAndGCM", b) }
func BenchmarkDecrypt64KBWithOAEPAndGCM(b *testing.B) { benchDecrypt("64KB", "OAEPAndGCM", b) }
func BenchmarkDecrypt1MBWithOAEPAndGCM(b *testing.B)  { benchDecrypt("1MB", "OAEPAndGCM", b) }
func BenchmarkDecrypt64MBWithOAEPAndGCM(b *testing.B) { benchDecrypt("64MB", "OAEPAndGCM", b) }

func BenchmarkDecrypt1BWithPKCSAndGCM(b *testing.B)   { benchDecrypt("1B", "PKCSAndGCM", b) }
func BenchmarkDecrypt64BWithPKCSAndGCM(b *testing.B)  { benchDecrypt("64B", "PKCSAndGCM", b) }
func BenchmarkDecrypt1KBWithPKCSAndGCM(b *testing.B)  { benchDecrypt("1KB", "PKCSAndGCM", b) }
func BenchmarkDecrypt64KBWithPKCSAndGCM(b *testing.B) { benchDecrypt("64KB", "PKCSAndGCM", b) }
func BenchmarkDecrypt1MBWithPKCSAndGCM(b *testing.B)  { benchDecrypt("1MB", "PKCSAndGCM", b) }
func BenchmarkDecrypt64MBWithPKCSAndGCM(b *testing.B) { benchDecrypt("64MB", "PKCSAndGCM", b) }

func BenchmarkDecrypt1BWithOAEPAndCBC(b *testing.B)   { benchDecrypt("1B", "OAEPAndCBC", b) }
func BenchmarkDecrypt64BWithOAEPAndCBC(b *testing.B)  { benchDecrypt("64B", "OAEPAndCBC", b) }
func BenchmarkDecrypt1KBWithOAEPAndCBC(b *testing.B)  { benchDecrypt("1KB", "OAEPAndCBC", b) }
func BenchmarkDecrypt64KBWithOAEPAndCBC(b *testing.B) { benchDecrypt("64KB", "OAEPAndCBC", b) }
func BenchmarkDecrypt1MBWithOAEPAndCBC(b *testing.B)  { benchDecrypt("1MB", "OAEPAndCBC", b) }
func BenchmarkDecrypt64MBWithOAEPAndCBC(b *testing.B) { benchDecrypt("64MB", "OAEPAndCBC", b) }

func BenchmarkDecrypt1BWithPKCSAndCBC(b *testing.B)   { benchDecrypt("1B", "PKCSAndCBC", b) }
func BenchmarkDecrypt64BWithPKCSAndCBC(b *testing.B)  { benchDecrypt("64B", "PKCSAndCBC", b) }
func BenchmarkDecrypt1KBWithPKCSAndCBC(b *testing.B)  { benchDecrypt("1KB", "PKCSAndCBC", b) }
func BenchmarkDecrypt64KBWithPKCSAndCBC(b *testing.B) { benchDecrypt("64KB", "PKCSAndCBC", b) }
func BenchmarkDecrypt1MBWithPKCSAndCBC(b *testing.B)  { benchDecrypt("1MB", "PKCSAndCBC", b) }
func BenchmarkDecrypt64MBWithPKCSAndCBC(b *testing.B) { benchDecrypt("64MB", "PKCSAndCBC", b) }

func BenchmarkDecrypt1BWithDirectGCM128(b *testing.B)   { benchDecrypt("1B", "DirectGCM128", b) }
func BenchmarkDecrypt64BWithDirectGCM128(b *testing.B)  { benchDecrypt("64B", "DirectGCM128", b) }
func BenchmarkDecrypt1KBWithDirectGCM128(b *testing.B)  { benchDecrypt("1KB", "DirectGCM128", b) }
func BenchmarkDecrypt64KBWithDirectGCM128(b *testing.B) { benchDecrypt("64KB", "DirectGCM128", b) }
func BenchmarkDecrypt1MBWithDirectGCM128(b *testing.B)  { benchDecrypt("1MB", "DirectGCM128", b) }
func BenchmarkDecrypt64MBWithDirectGCM128(b *testing.B) { benchDecrypt("64MB", "DirectGCM128", b) }

func BenchmarkDecrypt1BWithDirectCBC128(b *testing.B)   { benchDecrypt("1B", "DirectCBC128", b) }
func BenchmarkDecrypt64BWithDirectCBC128(b *testing.B)  { benchDecrypt("64B", "DirectCBC128", b) }
func BenchmarkDecrypt1KBWithDirectCBC128(b *testing.B)  { benchDecrypt("1KB", "DirectCBC128", b) }
func BenchmarkDecrypt64KBWithDirectCBC128(b *testing.B) { benchDecrypt("64KB", "DirectCBC128", b) }
func BenchmarkDecrypt1MBWithDirectCBC128(b *testing.B)  { benchDecrypt("1MB", "DirectCBC128", b) }
func BenchmarkDecrypt64MBWithDirectCBC128(b *testing.B) { benchDecrypt("64MB", "DirectCBC128", b) }

func BenchmarkDecrypt1BWithDirectGCM256(b *testing.B)   { benchDecrypt("1B", "DirectGCM256", b) }
func BenchmarkDecrypt64BWithDirectGCM256(b *testing.B)  { benchDecrypt("64B", "DirectGCM256", b) }
func BenchmarkDecrypt1KBWithDirectGCM256(b *testing.B)  { benchDecrypt("1KB", "DirectGCM256", b) }
func BenchmarkDecrypt64KBWithDirectGCM256(b *testing.B) { benchDecrypt("64KB", "DirectGCM256", b) }
func BenchmarkDecrypt1MBWithDirectGCM256(b *testing.B)  { benchDecrypt("1MB", "DirectGCM256", b) }
func BenchmarkDecrypt64MBWithDirectGCM256(b *testing.B) { benchDecrypt("64MB", "DirectGCM256", b) }

func BenchmarkDecrypt1BWithDirectCBC256(b *testing.B)   { benchDecrypt("1B", "DirectCBC256", b) }
func BenchmarkDecrypt64BWithDirectCBC256(b *testing.B)  { benchDecrypt("64B", "DirectCBC256", b) }
func BenchmarkDecrypt1KBWithDirectCBC256(b *testing.B)  { benchDecrypt("1KB", "DirectCBC256", b) }
func BenchmarkDecrypt64KBWithDirectCBC256(b *testing.B) { benchDecrypt("64KB", "DirectCBC256", b) }
func BenchmarkDecrypt1MBWithDirectCBC256(b *testing.B)  { benchDecrypt("1MB", "DirectCBC256", b) }
func BenchmarkDecrypt64MBWithDirectCBC256(b *testing.B) { benchDecrypt("64MB", "DirectCBC256", b) }

func BenchmarkDecrypt1BWithAESKWAndGCM128(b *testing.B)   { benchDecrypt("1B", "AESKWAndGCM128", b) }
func BenchmarkDecrypt64BWithAESKWAndGCM128(b *testing.B)  { benchDecrypt("64B", "AESKWAndGCM128", b) }
func BenchmarkDecrypt1KBWithAESKWAndGCM128(b *testing.B)  { benchDecrypt("1KB", "AESKWAndGCM128", b) }
func BenchmarkDecrypt64KBWithAESKWAndGCM128(b *testing.B) { benchDecrypt("64KB", "AESKWAndGCM128", b) }
func BenchmarkDecrypt1MBWithAESKWAndGCM128(b *testing.B)  { benchDecrypt("1MB", "AESKWAndGCM128", b) }
func BenchmarkDecrypt64MBWithAESKWAndGCM128(b *testing.B) { benchDecrypt("64MB", "AESKWAndGCM128", b) }

func BenchmarkDecrypt1BWithAESKWAndCBC256(b *testing.B)   { benchDecrypt("1B", "AESKWAndCBC256", b) }
func BenchmarkDecrypt64BWithAESKWAndCBC256(b *testing.B)  { benchDecrypt("64B", "AESKWAndCBC256", b) }
func BenchmarkDecrypt1KBWithAESKWAndCBC256(b *testing.B)  { benchDecrypt("1KB", "AESKWAndCBC256", b) }
func BenchmarkDecrypt64KBWithAESKWAndCBC256(b *testing.B) { benchDecrypt("64KB", "AESKWAndCBC256", b) }
func BenchmarkDecrypt1MBWithAESKWAndCBC256(b *testing.B)  { benchDecrypt("1MB", "AESKWAndCBC256", b) }
func BenchmarkDecrypt64MBWithAESKWAndCBC256(b *testing.B) { benchDecrypt("64MB", "AESKWAndCBC256", b) }

func BenchmarkDecrypt1BWithECDHOnP256AndGCM128(b *testing.B) {
	benchDecrypt("1B", "ECDHOnP256AndGCM128", b)
}
func BenchmarkDecrypt64BWithECDHOnP256AndGCM128(b *testing.B) {
	benchDecrypt("64B", "ECDHOnP256AndGCM128", b)
}
func BenchmarkDecrypt1KBWithECDHOnP256AndGCM128(b *testing.B) {
	benchDecrypt("1KB", "ECDHOnP256AndGCM128", b)
}
func BenchmarkDecrypt64KBWithECDHOnP256AndGCM128(b *testing.B) {
	benchDecrypt("64KB", "ECDHOnP256AndGCM128", b)
}
func BenchmarkDecrypt1MBWithECDHOnP256AndGCM128(b *testing.B) {
	benchDecrypt("1MB", "ECDHOnP256AndGCM128", b)
}
func BenchmarkDecrypt64MBWithECDHOnP256AndGCM128(b *testing.B) {
	benchDecrypt("64MB", "ECDHOnP256AndGCM128", b)
}

func BenchmarkDecrypt1BWithECDHOnP384AndGCM128(b *testing.B) {
	benchDecrypt("1B", "ECDHOnP384AndGCM128", b)
}
func BenchmarkDecrypt64BWithECDHOnP384AndGCM128(b *testing.B) {
	benchDecrypt("64B", "ECDHOnP384AndGCM128", b)
}
func BenchmarkDecrypt1KBWithECDHOnP384AndGCM128(b *testing.B) {
	benchDecrypt("1KB", "ECDHOnP384AndGCM128", b)
}
func BenchmarkDecrypt64KBWithECDHOnP384AndGCM128(b *testing.B) {
	benchDecrypt("64KB", "ECDHOnP384AndGCM128", b)
}
func BenchmarkDecrypt1MBWithECDHOnP384AndGCM128(b *testing.B) {
	benchDecrypt("1MB", "ECDHOnP384AndGCM128", b)
}
func BenchmarkDecrypt64MBWithECDHOnP384AndGCM128(b *testing.B) {
	benchDecrypt("64MB", "ECDHOnP384AndGCM128", b)
}

func BenchmarkDecrypt1BWithECDHOnP521AndGCM128(b *testing.B) {
	benchDecrypt("1B", "ECDHOnP521AndGCM128", b)
}
func BenchmarkDecrypt64BWithECDHOnP521AndGCM128(b *testing.B) {
	benchDecrypt("64B", "ECDHOnP521AndGCM128", b)
}
func BenchmarkDecrypt1KBWithECDHOnP521AndGCM128(b *testing.B) {
	benchDecrypt("1KB", "ECDHOnP521AndGCM128", b)
}
func BenchmarkDecrypt64KBWithECDHOnP521AndGCM128(b *testing.B) {
	benchDecrypt("64KB", "ECDHOnP521AndGCM128", b)
}
func BenchmarkDecrypt1MBWithECDHOnP521AndGCM128(b *testing.B) {
	benchDecrypt("1MB", "ECDHOnP521AndGCM128", b)
}
func BenchmarkDecrypt64MBWithECDHOnP521AndGCM128(b *testing.B) {
	benchDecrypt("64MB", "ECDHOnP521AndGCM128", b)
}

func benchDecrypt(chunkKey, primKey string, b *testing.B) {
	chunk, ok := chunks[chunkKey]
	if !ok {
		b.Fatalf("unknown chunk size %s", chunkKey)
	}

	enc, ok := encrypters[primKey]
	if !ok {
		b.Fatalf("unknown encrypter %s", primKey)
	}

	dec, ok := decryptionKeys[primKey]
	if !ok {
		b.Fatalf("unknown decryption key %s", primKey)
	}

	data, err := enc.Encrypt(chunk)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(chunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data.Decrypt(dec)
	}
}

func mustEncrypter(keyAlg KeyAlgorithm, encAlg ContentEncryption, encryptionKey interface{}) Encrypter {
	enc, err := NewEncrypter(keyAlg, encAlg, encryptionKey)
	if err != nil {
		panic(err)
	}
	return enc
}
