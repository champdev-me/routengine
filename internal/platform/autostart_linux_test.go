package platform

import "testing"

func TestSetAutostart(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if AutostartEnabled() {
		t.Fatal("on before SetAutostart")
	}
	if err := SetAutostart(true); err != nil {
		t.Fatal(err)
	}
	if !AutostartEnabled() {
		t.Fatal("still off after SetAutostart(true)")
	}
	for i := 0; i < 2; i++ { // turning off twice is fine
		if err := SetAutostart(false); err != nil {
			t.Fatal(err)
		}
	}
	if AutostartEnabled() {
		t.Fatal("still on after SetAutostart(false)")
	}
}
