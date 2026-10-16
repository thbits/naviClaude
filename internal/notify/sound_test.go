package notify

import "testing"

func TestBuiltinSoundsEmbedded(t *testing.T) {
	for _, s := range []Sound{Request, Done} {
		data, err := builtin.ReadFile("sounds/" + string(s) + ".mp3")
		if err != nil || len(data) < 1000 {
			t.Errorf("%s: len=%d err=%v", s, len(data), err)
		}
	}
}

func TestPlayDisabledByEnv(t *testing.T) {
	t.Setenv(DisableEnv, "1")
	if err := Play(Request, "/does/not/exist.mp3", 1); err != nil {
		t.Errorf("disabled Play should be a no-op, got %v", err)
	}
}

func TestPlayNoneIsSilent(t *testing.T) {
	if err := Play(Done, "none", 1); err != nil {
		t.Errorf("Play(none) = %v", err)
	}
}
