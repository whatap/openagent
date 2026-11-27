package rdsevent

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/whatap/golib/lang/pack"
)

// Category is the TagCountPack category the agency used for RDS events.
const Category = "aws_rds_event"

// Tag and field names, in the order the agency put them (agency last).
const (
	categoryTagPrefix   = "category"
	tagSourceArn        = "sourceArn"
	tagSourceIdentifier = "sourceIdentifier"
	tagSourceType       = "sourceType"
	tagAgency           = "agency"
	fieldMessage        = "message"
)

// EventToPack converts one RDS event into the agency's pack: time is the event
// date in milliseconds; tags are category0..N (event categories in order),
// sourceArn, sourceIdentifier, sourceType (string) and agency; the only field
// is message. A missing value becomes an empty text, which is what the agency
// wrote for a Java null. An event without a date is skipped, as the agency's
// per-event exception handler did. Insertion order matters: MapValue keeps it
// and the tag hash is computed over the serialized tags.
func EventToPack(ev types.Event, agencyName string) (*pack.TagCountPack, bool) {
	if ev.Date == nil {
		return nil, false
	}
	p := pack.NewTagCountPack()
	p.Category = Category
	p.Time = ev.Date.UnixMilli()
	for i, category := range ev.EventCategories {
		p.PutTag(categoryTagPrefix+strconv.Itoa(i), category)
	}
	p.PutTag(tagSourceArn, aws.ToString(ev.SourceArn))
	p.PutTag(tagSourceIdentifier, aws.ToString(ev.SourceIdentifier))
	p.PutTag(tagSourceType, string(ev.SourceType))
	p.PutTag(tagAgency, agencyName)
	p.Put(fieldMessage, aws.ToString(ev.Message))
	return p, true
}

// eventKey identifies an event across overlapping windows: source, date (ms),
// message and categories. The hash keeps the state file small.
func eventKey(ev types.Event) string {
	h := sha256.New()
	var date int64
	if ev.Date != nil {
		date = ev.Date.UnixMilli()
	}
	parts := []string{
		aws.ToString(ev.SourceArn),
		string(ev.SourceType),
		aws.ToString(ev.SourceIdentifier),
		strconv.FormatInt(date, 10),
		aws.ToString(ev.Message),
	}
	parts = append(parts, ev.EventCategories...)
	for _, part := range parts {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{':'})
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// sortPacks orders packs by time like the agency's send (a stable sort, so
// events with the same millisecond keep the API order).
func sortPacks(packs []*pack.TagCountPack) {
	sort.SliceStable(packs, func(i, j int) bool { return packs[i].Time < packs[j].Time })
}
