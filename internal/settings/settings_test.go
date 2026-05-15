package settings

import "testing"

func TestSettingsRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	want := Settings{
		UpdateRepo:         "owner/repo",
		IncludePrerelease:  false,
		UpdateSOCKSEnabled: true,
		UpdateSOCKSHost:    "127.0.0.1",
		UpdateSOCKSPort:    1080,
		UpdateSOCKSUser:    "user",
		UpdateSOCKSPass:    "pa'ss",
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.UpdateRepo != want.UpdateRepo || got.UpdateSOCKSPass != want.UpdateSOCKSPass || got.UpdateSOCKSPort != want.UpdateSOCKSPort {
		t.Fatalf("settings mismatch: %#v", got)
	}
	if got.IncludePrerelease {
		t.Fatalf("expected prerelease setting to round-trip false")
	}
}
