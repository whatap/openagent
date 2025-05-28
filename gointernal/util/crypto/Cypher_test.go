package crypto

import (
	"crypto/aes"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCypher(t *testing.T) {
	c := NewCypher([]byte("abcdefg"), 0)

	s := "A112fda12faf"

	b := c.Encrypt([]byte(s))
	ret := c.Decrypt(b)

	retStr := string(ret[:len(s)])

	assert.Equal(t, s, retStr)
}
func TestAes(t *testing.T) {
	// 암호화 예제
	plaintext := []byte("Hello, world! 12")
	plaintextResult := make([]byte, len(plaintext))

	key := make([]byte, 32)
	rand.Read(key) // 랜덤값 키 생성
	ciphertext := make([]byte, len(plaintext))

	cip, _ := aes.NewCipher(key)

	// fmt.Println("plaintext len=", len(plaintext))
	// 암호화
	cip.Encrypt(ciphertext, plaintext)
	// fmt.Printf("ciphertext: %x\n", ciphertext)

	// 복호화
	cip.Decrypt(plaintextResult, ciphertext)
	// fmt.Printf("plaintext: %s\n", plaintextResult)

	assert.Equal(t, plaintext, plaintextResult)
}
