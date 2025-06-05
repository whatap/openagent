package hashutil

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"io"
	"os"
)

func GetStructHash(s interface{}) string {
	// 구조체를 []byte로 변환하여 해시 계산에 사용
	bytes := []byte(fmt.Sprintf("%#v", s))

	// MD5 해시 객체 생성
	hash := md5.New()

	// 해시 계산
	_, err := hash.Write(bytes)
	if err != nil {
		panic(err)
	}

	// 해시 값을 16진수 문자열로 변환하여 반환
	return hex.EncodeToString(hash.Sum(nil))
}

func GetByteHash(data []byte) uint64 {
	hash := fnv.New64a()
	// 구조체 필드를 연속적으로 해시 함수에 입력합니다.
	hash.Write([]byte(data))
	return hash.Sum64()
}

func GetFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	hashValue := fmt.Sprintf("%x", hash.Sum(nil))
	return hashValue, nil
}
