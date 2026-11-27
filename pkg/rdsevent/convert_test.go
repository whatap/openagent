package rdsevent

import (
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
)

type kv struct {
	key   string
	value string
}

// orderedText lists a MapValue in insertion (wire) order and fails when a
// value is not a TextValue, which is what the agency's MapValue.put(String,
// String) produced.
func orderedText(t *testing.T, m *value.MapValue) []kv {
	t.Helper()
	var out []kv
	keys := m.Keys()
	for keys.HasMoreElements() {
		key := keys.NextString()
		text, ok := m.Get(key).(*value.TextValue)
		if !ok {
			t.Fatalf("%s is %T, want *value.TextValue", key, m.Get(key))
		}
		out = append(out, kv{key, text.Val})
	}
	return out
}

func TestEventToPackMatchesAgencyFormat(t *testing.T) {
	date := time.Date(2026, 11, 25, 2, 58, 1, 123456789, time.UTC)
	ev := types.Event{
		Date:             aws.Time(date),
		EventCategories:  []string{"failover", "notification"},
		Message:          aws.String("Multi-AZ instance failover completed."),
		SourceArn:        aws.String("arn:aws:rds:ap-northeast-2:111122223333:db:orders-db"),
		SourceIdentifier: aws.String("orders-db"),
		SourceType:       types.SourceTypeDbInstance,
	}
	p, ok := EventToPack(ev, "aws.rdsevent")
	if !ok {
		t.Fatal("event skipped")
	}
	if p.Category != "aws_rds_event" {
		t.Fatalf("category = %q", p.Category)
	}
	if p.Time != date.UnixMilli() || p.Time != 1795575481123 {
		t.Fatalf("time = %d, want event date in ms (%d)", p.Time, date.UnixMilli())
	}
	wantTags := []kv{
		{"category0", "failover"},
		{"category1", "notification"},
		{"sourceArn", "arn:aws:rds:ap-northeast-2:111122223333:db:orders-db"},
		{"sourceIdentifier", "orders-db"},
		{"sourceType", "db-instance"},
		{"agency", "aws.rdsevent"},
	}
	if got := orderedText(t, p.Tags); !reflect.DeepEqual(got, wantTags) {
		t.Fatalf("tags\n got %v\nwant %v", got, wantTags)
	}
	wantFields := []kv{{"message", "Multi-AZ instance failover completed."}}
	if got := orderedText(t, p.Data); !reflect.DeepEqual(got, wantFields) {
		t.Fatalf("fields\n got %v\nwant %v", got, wantFields)
	}
	// The identity is stamped by the sink, never by the conversion.
	if p.Pcode != 0 || p.Oid != 0 {
		t.Fatalf("conversion set pcode/oid: %d/%d", p.Pcode, p.Oid)
	}
}

func TestEventToPackMissingValuesAndDate(t *testing.T) {
	p, ok := EventToPack(types.Event{Date: aws.Time(t0)}, "openagent")
	if !ok {
		t.Fatal("event with a date was skipped")
	}
	want := []kv{{"sourceArn", ""}, {"sourceIdentifier", ""}, {"sourceType", ""}, {"agency", "openagent"}}
	if got := orderedText(t, p.Tags); !reflect.DeepEqual(got, want) {
		t.Fatalf("tags without categories\n got %v\nwant %v", got, want)
	}
	if got := orderedText(t, p.Data); !reflect.DeepEqual(got, []kv{{"message", ""}}) {
		t.Fatalf("fields = %v", got)
	}
	if _, ok := EventToPack(types.Event{Message: aws.String("no date")}, "openagent"); ok {
		t.Fatal("an event without a date must be skipped like the agency did")
	}
}

func TestEventPackSurvivesWireRoundTrip(t *testing.T) {
	p, _ := EventToPack(rdsEvent("db-1", "DB instance restarted", t0, "availability"), "openagent")
	p.Pcode, p.Oid = 42, 7
	decoded, ok := pack.ToPack(pack.ToBytesPack(p)).(*pack.TagCountPack)
	if !ok {
		t.Fatal("did not decode as a TagCountPack")
	}
	if decoded.Category != Category || decoded.Time != t0.UnixMilli() || decoded.Pcode != 42 || decoded.Oid != 7 {
		t.Fatalf("decoded header %+v", decoded.AbstractPack)
	}
	if got, want := orderedText(t, decoded.Tags), orderedText(t, p.Tags); !reflect.DeepEqual(got, want) {
		t.Fatalf("tags changed on the wire\n got %v\nwant %v", got, want)
	}
	if got := decoded.Data.GetString(fieldMessage); got != "DB instance restarted" {
		t.Fatalf("message = %q", got)
	}
}

func TestEventKeyIdentifiesTheEvent(t *testing.T) {
	a := rdsEvent("db-1", "DB instance restarted", t0, "availability")
	same := rdsEvent("db-1", "DB instance restarted", t0, "availability")
	if eventKey(a) != eventKey(same) {
		t.Fatal("equal events must share a key")
	}
	variants := []types.Event{
		rdsEvent("db-2", "DB instance restarted", t0, "availability"),
		rdsEvent("db-1", "DB instance shutdown", t0, "availability"),
		rdsEvent("db-1", "DB instance restarted", t0.Add(time.Millisecond), "availability"),
		rdsEvent("db-1", "DB instance restarted", t0, "notification"),
	}
	for i, v := range variants {
		if eventKey(v) == eventKey(a) {
			t.Fatalf("variant %d shares the key of a different event", i)
		}
	}
}

func TestSortPacksIsStable(t *testing.T) {
	mk := func(ms int64, msg string) *pack.TagCountPack {
		p := pack.NewTagCountPack()
		p.Time = ms
		p.Put(fieldMessage, msg)
		return p
	}
	packs := []*pack.TagCountPack{mk(3, "c"), mk(1, "a1"), mk(2, "b"), mk(1, "a2")}
	sortPacks(packs)
	var got []string
	for _, p := range packs {
		got = append(got, p.Data.GetString(fieldMessage))
	}
	if want := []string{"a1", "a2", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
