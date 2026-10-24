package auth

import "github.com/prometheus/client_golang/prometheus"

// Business metrics for the auth domain.
//
// The HTTP metrics in internal/web only say "401 happened" — and by design
// every login failure answers the same 401. These counters say why, which is
// what an alert needs: a spike in `reuse` is an incident, a spike in
// `bad_password` is usually someone fat-fingering their password. Labels are
// closed sets (reason codes), never user ids or emails, so cardinality stays
// bounded and nothing personally identifying reaches Prometheus.
var (
	loginTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auth_login_total",
			Help: "Login attempts by outcome (success, bad_password, locked_out, locked, unknown_email, disabled, error).",
		},
		[]string{"result"},
	)
	refreshTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auth_refresh_total",
			Help: "Refresh attempts by outcome (ok, tolerated_replay, reuse, session_cap, expired, invalid, disabled, error).",
		},
		[]string{"result"},
	)
	// sessionRevocationsTotal is incremented by the repository, the single choke
	// point for every revocation path (logout, password change, admin action,
	// reuse detection, session cap, sibling cleanup).
	sessionRevocationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auth_session_revocations_total",
			Help: "Refresh sessions revoked in bulk, by reason.",
		},
		[]string{"reason"},
	)
	accountLockoutsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "auth_account_lockouts_total",
			Help: "Accounts locked after consecutive failed logins.",
		},
	)
)

func init() {
	prometheus.MustRegister(loginTotal, refreshTotal, sessionRevocationsTotal, accountLockoutsTotal)
}
