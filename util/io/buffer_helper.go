package io

import "fmt"

const (
	BYTE_LIMIT = 20 * 1024 * 1024
)

func NewByteArray(sz int32) ([]byte, error) {
	if sz > BYTE_LIMIT {
		panic(fmt.Sprint("ILLEGAL MEMORY SIZE REQUEST ", sz))
	}
	return make([]byte, sz), nil
}
func NewStringArray(sz int32) ([]string, error) {
	if sz > 10240 {
		panic(fmt.Sprint("ILLEGAL MEMORY SIZE REQUEST ", sz))
	}
	return make([]string, sz), nil
}

func NewInt16Array(sz int32) ([]int16, error) {
	if sz*2 > BYTE_LIMIT {
		panic(fmt.Sprint("ILLEGAL MEMORY SIZE REQUEST ", sz))
	}
	return make([]int16, sz), nil
}

func NewInt32Array(sz int32) ([]int32, error) {
	if sz*4 > BYTE_LIMIT {
		panic(fmt.Sprint("ILLEGAL MEMORY SIZE REQUEST ", sz))
	}
	return make([]int32, sz), nil
}

func NewInt64Array(sz int32) ([]int64, error) {
	if sz*8 > BYTE_LIMIT {
		panic(fmt.Sprint("ILLEGAL MEMORY SIZE REQUEST ", sz))
	}
	return make([]int64, sz), nil
}

func NewFloat32Array(sz int32) ([]float32, error) {
	if sz*4 > BYTE_LIMIT {
		panic(fmt.Sprint("ILLEGAL MEMORY SIZE REQUEST ", sz))
	}
	return make([]float32, sz), nil
}

func NewFloat64Array(sz int32) ([]float64, error) {
	if sz*8 > BYTE_LIMIT {
		panic(fmt.Sprint("ILLEGAL MEMORY SIZE REQUEST ", sz))
	}
	return make([]float64, sz), nil
}
