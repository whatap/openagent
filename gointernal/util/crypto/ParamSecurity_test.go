package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/whatap/golib/lang/ref"
)

func TestParamSecurity(t *testing.T) {
	ps := GetParamSecurity()

	s := "A112fda12fafa34"
	c1 := ref.NewBYTE()
	b := ps.Encrypt([]byte(s), c1)

	c2 := ref.NewBYTE()
	c := string(ps.Decrypt(b, c2, []byte("WHATAP")))

	assert.Equal(t, s, c)
}
