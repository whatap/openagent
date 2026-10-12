package secure

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/whatap/gointernal/util/crypto"
	"github.com/whatap/golib/lang/pack"
)

func TestOnewaySharesConnectionWithOrdinaryFrames(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		for _, replay := range []bool{false, true} {
			t.Run(fmt.Sprintf("encrypted_%t_replay_%t", encrypted, replay), func(t *testing.T) {
				q := onewayTestQueue(t, 2)
				conf.CypherLevel = 0
				conf.NetFailoverRetrySendDataEnabled = true
				oldMaster, oldSecurity := master, secSession
				master = &SecurityMaster{PCODE: 999, OID: 99}
				secSession = &SecuritySession{TRANSFER_KEY: 789}
				t.Cleanup(func() { master, secSession = oldMaster, oldSecurity })

				normal := onewayTestPack(999)
				normalItem := TcpSend{pack: normal}
				normalPayload := pack.ToBytesPack(normal)
				if encrypted {
					conf.CypherLevel = 128
					// Synthetic session key; no license or key files are loaded.
					secSession.Cypher = crypto.NewCypher(make([]byte, 16), 123)
					normalItem.flag = NET_SECURE_CYPHER
					normalPayload = secSession.Cypher.Encrypt(pack.ToBytesPackECB(normal, 16))
					if bytes.Equal(normalPayload, pack.ToBytesPack(normal)) {
						t.Fatal("ordinary cipher fixture did not encrypt")
					}
				}
				target := onewayTestPack(101)
				if err := SendOneway(101, -42, target); err != nil {
					t.Fatal(err)
				}
				onewayItem := q.GetNoWait().(TcpSend)
				frames := onewayPipeFrames(t, 2, func(s *TcpSession) bool {
					s.RetryQueue.PutForce(&normalItem)
					var ok bool
					if replay {
						s.RetryQueue.PutForce(&onewayItem)
						s.client = nil // reconnect has not yet published the new socket
						ok = s.SendFailover()
					} else {
						// Ordinary Send uses the legacy global session for its deadline.
						// Supply it explicitly rather than booting a connection singleton.
						oldSession := session
						session = s
						defer func() { session = oldSession }()
						ok = s.Send(normalItem.flag, normalPayload, false) && s.sendQueuedOneway(&onewayItem)
					}
					return ok && s.RetryQueue.Size() == 0
				})
				ordinary := frames[0]
				if ordinary[0] != 1 || ordinary[1] != normalItem.flag ||
					int64(binary.BigEndian.Uint64(ordinary[2:10])) != 999 ||
					int32(binary.BigEndian.Uint32(ordinary[10:14])) != 99 ||
					int32(binary.BigEndian.Uint32(ordinary[14:18])) != 789 {
					t.Error("ordinary frame routing/flags changed")
				}
				if !bytes.Equal(ordinary[22:], normalPayload) {
					t.Error("ordinary frame payload/encryption changed")
				}
				assertOnewayFrame(t, frames[1], 101, -42, target)
			})
		}
	}
}
