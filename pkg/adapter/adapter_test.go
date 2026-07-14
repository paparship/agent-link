package adapter

import "testing"

func TestRegistrySupportedAgents(t *testing.T) {
	got := SupportedAgents()
	// sorted, contains the two known agents
	want := map[string]bool{"claude": false, "tclaude": false}
	for _, a := range got {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for a, seen := range want {
		if !seen {
			t.Errorf("SupportedAgents missing %q; got %v", a, got)
		}
	}
	// sorted check
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("SupportedAgents not sorted: %v", got)
		}
	}
	// every supported name must build a launcher and detector
	for _, a := range got {
		if NewLauncher(a) == nil {
			t.Errorf("NewLauncher(%q) == nil for a supported agent", a)
		}
		if NewDetector(a) == nil {
			t.Errorf("NewDetector(%q) == nil for a supported agent", a)
		}
	}
}

func TestRegistryUnknownAgent(t *testing.T) {
	if NewLauncher("nope") != nil {
		t.Error("NewLauncher(unknown) should be nil")
	}
	if NewDetector("nope") != nil {
		t.Error("NewDetector(unknown) should be nil")
	}
	if IsAvailable("nope") {
		t.Error("IsAvailable(unknown) should be false")
	}
}

func TestRootEnv(t *testing.T) {
	for _, a := range []string{"claude", "tclaude"} {
		env := NewLauncher(a).RootEnv()
		found := false
		for _, kv := range env {
			if kv == "IS_SANDBOX=1" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s RootEnv missing IS_SANDBOX=1; got %v", a, env)
		}
	}
}
