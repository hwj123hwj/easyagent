package config

import "testing"

func TestOutsideWorkspaceIndependentOfApproval(t *testing.T) {
	for _, approve := range []string{"false", "true"} {
		for _, outside := range []string{"false", "true"} {
			t.Setenv("EA_AUTO_APPROVE", approve)
			t.Setenv("EA_ALLOW_OUTSIDE_WORKSPACE", outside)
			c := Default()
			c.LoadFromEnv()
			if c.AutoApprove != (approve == "true") || c.AllowOutsideWorkspace != (outside == "true") {
				t.Fatal("approval and path policy coupled")
			}
		}
	}
	if Default().AllowOutsideWorkspace {
		t.Fatal("unsafe default")
	}
}
