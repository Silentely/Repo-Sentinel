package notify

import (
	"testing"
)

func TestAssignBarkSoundAndLevel(t *testing.T) {
	sndCrit, lvlCrit := AssignBarkSoundAndLevel("critical")
	if sndCrit != "alarm.caf" || lvlCrit != "timeSensitive" {
		t.Errorf("unexpected critical bark sound/level: %s, %s", sndCrit, lvlCrit)
	}

	sndMed, lvlMed := AssignBarkSoundAndLevel("medium")
	if sndMed != "glass.caf" || lvlMed != "active" {
		t.Errorf("unexpected medium bark sound/level: %s, %s", sndMed, lvlMed)
	}
}

func TestBuildBarkPayloadWithExt(t *testing.T) {
	opts := BarkNotificationOptions{
		Badge:     2,
		Sound:     "bell.caf",
		Level:     "active",
		IsArchive: 1,
	}

	p := BuildBarkPayloadWithExt("服务告警", "CPU 使用率过高", "server-monitor", opts)
	if p["title"] != "服务告警" || p["group"] != "server-monitor" {
		t.Errorf("unexpected title/group: %+v", p)
	}
	if p["badge"] != 2 || p["sound"] != "bell.caf" {
		t.Errorf("unexpected badge/sound: %+v", p)
	}
	if p["level"] != "active" || p["isArchive"] != 1 {
		t.Errorf("unexpected level/archive: %+v", p)
	}
}
