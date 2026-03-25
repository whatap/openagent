package sender

import (
	"open-agent/pkg/model"
	"testing"
)

func TestDuplicateDetection(t *testing.T) {
	// Create sender
	processedQueue := make(chan *model.ConversionResult, 10)

	// Pass nil logger, NewSender will create a default one.
	// This might create log files in current directory, which we should clean up or accept.
	// For this test, it's fine.
	s := NewSender(processedQueue, nil)

	// Create a result with empty lists so it doesn't try to send to network
	res1 := &model.ConversionResult{
		Target:         "target1",
		CollectionTime: 1000,
		OpenMxList:     []*model.OpenMx{},
		OpenMxHelpList: []*model.OpenMxHelp{},
	}

	// Send first time
	s.sendResult(res1)

	s.mu.Lock()
	lastTime, exists := s.lastSendTime["target1"]
	s.mu.Unlock()

	if !exists {
		t.Fatal("target1 should exist in map")
	}
	if lastTime != 1000 {
		t.Fatalf("expected 1000, got %d", lastTime)
	}

	// Send again (duplicate) - should trigger log logic but not crash
	s.sendResult(res1)

	// Send new time
	res2 := &model.ConversionResult{
		Target:         "target1",
		CollectionTime: 2000,
		OpenMxList:     []*model.OpenMx{},
		OpenMxHelpList: []*model.OpenMxHelp{},
	}
	s.sendResult(res2)

	s.mu.Lock()
	lastTime, exists = s.lastSendTime["target1"]
	s.mu.Unlock()

	if lastTime != 2000 {
		t.Fatalf("expected 2000, got %d", lastTime)
	}

	// Test with different target
	res3 := &model.ConversionResult{
		Target:         "target2",
		CollectionTime: 1000,
		OpenMxList:     []*model.OpenMx{},
		OpenMxHelpList: []*model.OpenMxHelp{},
	}
	s.sendResult(res3)

	s.mu.Lock()
	lastTime2, exists2 := s.lastSendTime["target2"]
	s.mu.Unlock()

	if !exists2 {
		t.Fatal("target2 should exist in map")
	}
	if lastTime2 != 1000 {
		t.Fatalf("expected 1000 for target2, got %d", lastTime2)
	}
}
