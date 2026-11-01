package procmap

import (
	"os"
	"strings"
	"testing"
)

func TestRunningListsThisProgramOnce(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range Running() {
		if p == strings.TrimSuffix(self, " (deleted)") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("this test program (%s) is listed %d times, want once", self, n)
	}
}
