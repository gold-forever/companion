package refused

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)

func TestARefusalIsDueADayLaterAndAtTheNextLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refused.json")
	l := Load(path)
	if err := l.Refuse("/wow/GoldForever.lua", "ab12", now); err != nil {
		t.Fatal(err)
	}

	if due := l.Due(now.Add(Every - time.Second)); len(due) != 0 {
		t.Fatalf("due within the day: %+v", due)
	}
	if due := l.Due(now.Add(Every)); len(due) != 1 || due[0] != (Save{"/wow/GoldForever.lua", "ab12"}) {
		t.Fatalf("due after a day: %+v", due)
	}
	if due := Load(path).Due(now); len(due) != 1 {
		t.Fatalf("not due at once after a load: %+v", due)
	}
}

func TestADroppedRefusalIsGoneAfterTheNextLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refused.json")
	l := Load(path)
	_ = l.Refuse("/wow/GoldForever.lua", "ab12", now)

	if err := l.Drop("/wow/GoldForever.lua"); err != nil {
		t.Fatal(err)
	}

	if Load(path).Holds("/wow/GoldForever.lua") {
		t.Fatal("a dropped refusal came back")
	}
}

func TestAnUnreadableListIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refused.json")
	_ = os.WriteFile(path, []byte("not json"), 0o600)

	if due := Load(path).Due(now); len(due) != 0 {
		t.Fatalf("due = %+v", due)
	}
}
