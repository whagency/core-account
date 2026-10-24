package config

import (
	"fmt"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

// Role selects which halves of the service this process runs.
//
// The split exists so background work can be scaled independently of request
// serving. Without it, the concurrency this service directs at its downstreams
// is a function of how many replicas the HTTP load happens to need: scaling out
// for API traffic also multiplies the outbox pools hitting restriction-service,
// and multiplies the queue consumers competing for the same messages.
//
// RoleAll is the default, so a deployment that sets nothing behaves exactly as
// before. Splitting is opt-in, and the two roles must then be deployed as a
// pair — see the startup banner in main.go.
type Role string

const (
	// RoleAll runs everything in one process. Default, and the right choice for
	// local development and small deployments.
	RoleAll Role = "all"

	// RoleAPI serves synchronous traffic only: HTTP and gRPC. It runs no queue
	// consumers, no cron jobs and no outbox pool.
	RoleAPI Role = "api"

	// RoleWorker runs background processing only: queue consumers, cron jobs and
	// the outbox pool. It still serves HTTP, but only so probes and the metrics
	// endpoint are reachable — do not route business traffic to it.
	RoleWorker Role = "worker"
)

// ServesRequests reports whether this process should accept synchronous traffic.
func (r Role) ServesRequests() bool { return r == RoleAll || r == RoleAPI }

// RunsBackground reports whether this process should run queue consumers, cron
// jobs and the outbox pool.
func (r Role) RunsBackground() bool { return r == RoleAll || r == RoleWorker }

// String implements fmt.Stringer.
func (r Role) String() string { return string(r) }

type RoleSettings struct {
	Role Role `envconfig:"APP_ROLE" default:"all"`
}

var roleSettings RoleSettings

// AppRole returns the role this process was started with.
func AppRole() Role {
	return roleSettings.Role
}

// loadRoleConfig loads and validates the process role.
func loadRoleConfig() error {
	if err := envconfig.Process("", &roleSettings); err != nil {
		return fmt.Errorf("[config:loadRoleConfig]%w", err)
	}

	roleSettings.Role = Role(strings.ToLower(strings.TrimSpace(string(roleSettings.Role))))
	switch roleSettings.Role {
	case RoleAll, RoleAPI, RoleWorker:
		return nil
	default:
		// Fail rather than default: a typo in APP_ROLE would otherwise silently
		// start a process that does half of what the deployment expected.
		return fmt.Errorf("[config:loadRoleConfig][err: unknown APP_ROLE %q, want one of %s, %s, %s]",
			roleSettings.Role, RoleAll, RoleAPI, RoleWorker)
	}
}
