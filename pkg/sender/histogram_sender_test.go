package sender

import (
	"testing"

	"github.com/whatap/golib/lang/pack"

	"open-agent/pkg/model"
)

func TestSendResultChunksNativeHistograms(t *testing.T) {
	s := NewSender(make(chan *model.ConversionResult), nil, false)

	var sent []pack.Pack
	s.sendPack = func(p pack.Pack) {
		sent = append(sent, p)
	}

	histograms := make([]*model.OpenMxHistogram, ChunkSize+1)
	for i := range histograms {
		histograms[i] = &model.OpenMxHistogram{}
	}
	result := model.NewConversionResult(nil, nil)
	result.SetOpenMxHistogramList(histograms)

	s.sendResult(result)

	if len(sent) != 2 {
		t.Fatalf("expected 2 histogram packs, got %d", len(sent))
	}
	wantSizes := []int{ChunkSize, 1}
	for i, p := range sent {
		histogramPack, ok := p.(*model.OpenMxHistogramPack)
		if !ok {
			t.Fatalf("pack %d has type %T, want *model.OpenMxHistogramPack", i, p)
		}
		if histogramPack.GetPackType() != model.OPEN_MX_HISTOGRAM_PACK {
			t.Errorf("pack %d type = %#x, want %#x", i, histogramPack.GetPackType(), model.OPEN_MX_HISTOGRAM_PACK)
		}
		if got := len(histogramPack.GetRecords()); got != wantSizes[i] {
			t.Errorf("pack %d records = %d, want %d", i, got, wantSizes[i])
		}
	}
}
