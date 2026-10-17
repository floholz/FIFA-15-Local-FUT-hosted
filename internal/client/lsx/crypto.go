// Package lsx is the local Origin LSX stub FIFA 15 talks to on 127.0.0.1:3216.
//
// Port of OriginRandom / OriginCrypto / LSXHandler from payload/localfut15/server.py.
// OriginSDK 9.x/10.x speaks first with a plaintext challenge, then exchanges
// NUL-terminated ASCII-hex frames of AES-128-ECB/PKCS7 encrypted XML.
package lsx

import (
	"bytes"
	"crypto/aes"
	"encoding/hex"
	"errors"
)

// originRandom is the legacy Origin SDK LCG used for session-key derivation.
type originRandom struct{ seed uint32 }

func (r *originRandom) next() uint32 {
	r.seed = r.seed*214013 + 2531011
	return (r.seed >> 16) & 0x7FFF
}

// Key derives the AES key for a seed. Seed 0 is the primary key 00..0f.
func Key(seed uint32) []byte {
	key := make([]byte, 16)
	if seed == 0 {
		for i := range key {
			key[i] = byte(i)
		}
		return key
	}
	rng := originRandom{seed: 7}
	rng.seed = rng.next() + seed
	for i := range key {
		key[i] = byte(rng.next() & 0xFF)
	}
	return key
}

// Encrypt is AES-128-ECB with PKCS7 padding.
func Encrypt(key, plain []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // key is always 16 bytes
	}
	pad := 16 - len(plain)%16
	buf := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	for i := 0; i < len(buf); i += 16 {
		block.Encrypt(buf[i:i+16], buf[i:i+16])
	}
	return buf
}

// Decrypt reverses Encrypt.
func Decrypt(key, cipherText []byte) ([]byte, error) {
	if len(cipherText) == 0 || len(cipherText)%16 != 0 {
		return nil, errors.New("ciphertext is not a multiple of the block size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(cipherText))
	for i := 0; i < len(out); i += 16 {
		block.Decrypt(out[i:i+16], cipherText[i:i+16])
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > 16 || pad > len(out) || !bytes.Equal(out[len(out)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return nil, errors.New("bad PKCS7 padding")
	}
	return out[:len(out)-pad], nil
}

// EncryptHex returns the lowercase hex of Encrypt, like Python's bytes.hex().
func EncryptHex(key []byte, text string) string {
	return hex.EncodeToString(Encrypt(key, []byte(text)))
}

// SessionSeed is the protocol-3 seed: the first two ASCII bytes of the accept response.
func SessionSeed(acceptResponse string) uint32 {
	if len(acceptResponse) < 2 {
		return 0
	}
	return uint32(acceptResponse[0])<<8 | uint32(acceptResponse[1])
}
