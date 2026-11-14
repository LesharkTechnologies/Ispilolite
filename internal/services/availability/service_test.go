package availability

import "testing"

func TestValidClockRange(t *testing.T) {
	if !validClockRange("09:00", "17:00") {
		t.Fatal("valid range rejected")
	}
	for _, item := range [][2]string{{"17:00", "09:00"}, {"bad", "17:00"}, {"09:00", "09:00"}} {
		if validClockRange(item[0], item[1]) {
			t.Fatalf("invalid range accepted: %v", item)
		}
	}
}
