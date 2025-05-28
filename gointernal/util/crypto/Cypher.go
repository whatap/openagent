package crypto

import (
	"crypto/aes"
	"crypto/cipher"

	"github.com/whatap/golib/io"
	"github.com/whatap/golib/logger"
)

type Cypher struct {
	Log     logger.Logger
	block   int
	cypher  cipher.Block
	xor_key []byte
	level   int
}

func NewCypher(cypher_key []byte, xor_key int32, opts ...CryptoOption) *Cypher {
	cph := new(Cypher)

	for _, opt := range opts {
		opt.apply(conf)
	}
	cph.Log = conf.Log
	if conf.Log == nil {
		cph.Log = &logger.EmptyLogger{}
	}
	cph.level = int(conf.CypherLevel)
	cph.block = int(cph.level) / 8
	cypher, err := aes.NewCipher(cph.padding(cypher_key))
	if err != nil {
		conf.Log.Println("WA811", "ERROR ", err)
		cph.cypher = nil
	} else {
		cph.cypher = cypher
	}
	cph.xor_key = io.ToBytesInt(xor_key)
	return cph
}

func (this *Cypher) Encrypt(data []byte) []byte {
	if this.block == 0 || this.cypher == nil {
		return data
	}
	defer func() []byte {
		err := recover()
		if err != nil {
			this.Log.Println("WA812", "Encrypt", err)
		}
		return data
	}()

	dataLen := len(data)
	remainder := dataLen % this.block
	var src []byte
	if remainder == 0 {
		src = data
	} else {
		src = make([]byte, (dataLen/this.block+1)*this.block)
		io.SetBytes(src, 0, data)
	}
	dst := make([]byte, len(src))
	data = dst

	for len(src) > 0 {
		this.cypher.Encrypt(dst, src[:this.block])
		src = src[this.block:]
		dst = dst[this.block:]
	}

	return data
}

func (this *Cypher) Decrypt(data []byte) (ret []byte) {
	if this.block == 0 || this.cypher == nil {
		return data
	}

	defer func() {
		err := recover()
		if err != nil {
			ret = data
			this.Log.Println("WA813", err)
		}
	}()

	dataLen := len(data)
	src := data
	dst := make([]byte, dataLen)
	data = dst
	for len(src) > 0 {
		this.cypher.Decrypt(dst, src[:this.block])
		src = src[this.block:]
		dst = dst[this.block:]
	}
	return data
}

func (this *Cypher) padding(src []byte) []byte {
	if len(src) == this.block {
		return src
	}
	dest := make([]byte, this.block)
	if len(src) > this.block {
		copy(dest, src[:this.block])
	} else {
		copy(dest, src)
	}
	return dest
}

func (this *Cypher) Hide(data []byte) []byte {
	if this.block == 0 || this.xor_key == nil {
		return data
	}
	keyLen := len(this.xor_key)
	dataLen := len(data)
	j := 0
	for i := 0; i < dataLen; i += 1 {
		data[i] ^= this.xor_key[j]
		j = (j + 1) % keyLen
	}
	return data
}
