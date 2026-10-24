package config

import "testing"

// The matrix is the contract between this code and the deployment manifests.
// Getting a cell wrong means a deployment that looks healthy while silently not
// doing half its job, so it is pinned rather than left to reading.
func TestRole_Responsibilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role           Role
		servesRequests bool
		runsBackground bool
	}{
		{role: RoleAll, servesRequests: true, runsBackground: true},
		{role: RoleAPI, servesRequests: true, runsBackground: false},
		{role: RoleWorker, servesRequests: false, runsBackground: true},
	}

	for _, tt := range tests {
		t.Run(tt.role.String(), func(t *testing.T) {
			t.Parallel()

			if got := tt.role.ServesRequests(); got != tt.servesRequests {
				t.Errorf("ServesRequests() = %v, want %v", got, tt.servesRequests)
			}
			if got := tt.role.RunsBackground(); got != tt.runsBackground {
				t.Errorf("RunsBackground() = %v, want %v", got, tt.runsBackground)
			}
		})
	}
}

// Every responsibility must be covered by at least one role, or a split
// deployment would drop work on the floor no matter how it is configured.
func TestRole_SplitDeploymentCoversEverything(t *testing.T) {
	t.Parallel()

	if !(RoleAPI.ServesRequests() || RoleWorker.ServesRequests()) {
		t.Error("no role serves requests")
	}
	if !(RoleAPI.RunsBackground() || RoleWorker.RunsBackground()) {
		t.Error("no role runs background work")
	}
	// And no responsibility may be claimed by both, or a split deployment would
	// double the concurrency it directs at downstream services — the exact
	// coupling the split exists to remove.
	if RoleAPI.ServesRequests() && RoleWorker.ServesRequests() {
		t.Error("both roles serve requests")
	}
	if RoleAPI.RunsBackground() && RoleWorker.RunsBackground() {
		t.Error("both roles run background work")
	}
}

func TestLoadRoleConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    Role
		wantErr bool
	}{
		{name: "unset defaults to all", env: "", want: RoleAll},
		{name: "api", env: "api", want: RoleAPI},
		{name: "worker", env: "worker", want: RoleWorker},
		{name: "case and space tolerant", env: "  Worker ", want: RoleWorker},
		// A typo must stop the process. Defaulting it would start something that
		// does half of what the deployment expected, with nothing to notice.
		{name: "typo is rejected", env: "wroker", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("APP_ROLE", tt.env)
			}
			roleSettings = RoleSettings{}

			err := loadRoleConfig()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("APP_ROLE=%q was accepted, want an error", tt.env)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadRoleConfig: %v", err)
			}
			if got := AppRole(); got != tt.want {
				t.Errorf("AppRole() = %q, want %q", got, tt.want)
			}
		})
	}
}
