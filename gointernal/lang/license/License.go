package license

import (
	"strings"

	"github.com/whatap/golib/io"
	"github.com/whatap/golib/util/hexa32"
	"github.com/whatap/golib/util/keygen"
)

func Build(pcode int64, secure_key []byte) string {
	out := io.NewDataOutputX()
	out.WriteDecimal(pcode)
	out.WriteBlob(secure_key)
	sz := out.Size()
	n := sz / 8
	if sz-n*8 > 0 {
		out.WriteLong(keygen.Next())
		n++
	}
	in := io.NewDataInputX(out.ToByteArray())
	sb := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			sb += "-"
		}
		sb += hexa32.ToString32(in.ReadLong())
	}
	return sb
}
func Parse(lic string) (int64, []byte) {
	tokens := strings.Split(lic, "-")
	out := io.NewDataOutputX()
	for i := 0; i < len(tokens); i++ {
		out.WriteLong(hexa32.ToLong32(tokens[i]))
	}
	in := io.NewDataInputX(out.ToByteArray())
	pcode := in.ReadDecimal()
	security_key := in.ReadBlob()
	return pcode, security_key
}
