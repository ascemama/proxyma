package api

import (
	"log"
	"sync"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/interaction"
	"proxyma/internal/interaction/history"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/llm"
	"proxyma/internal/sshclient"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/tasks"
	"proxyma/internal/users"
	"proxyma/internal/winrmclient"
)

// Server owns Proxyma HTTP dependencies.
type Server struct {
	Agents                    agents.Registry
	Users                     users.Registry
	UserSessions              *users.SessionStore
	CredentialTokens          *users.CredentialTokenStore
	Targets                   targets.Registry
	Policies                  *policy.MutableEvaluator
	LLMProfiles               *llm.MutableCatalog
	Tasks                     *tasks.Manager
	Interactions              interaction.Communicator
	InteractionHistory        history.QueryStore
	InteractionProvider       *interactioninstances.InteractionRouter
	SSHClient                 sshclient.Client
	WinRMClient               winrmclient.Client
	CommandTimeout            time.Duration
	OutputLimit               int64
	KnownHostsPath            string
	ManagementPaths           users.Paths
	UserSessionTTL            time.Duration
	UserActivationTokenTTL    time.Duration
	UserActivationTokenMaxTTL time.Duration
	UseDevelopmentCookies     bool
	BuildInfo                 BuildInfoResponse
	Debug                     bool
	Logger                    *log.Logger
	usersMu                   sync.RWMutex
	agentsMu                  sync.RWMutex
	targetsMu                 sync.RWMutex
	interactionProvidersMu    sync.Mutex
	throttle                  *loginThrottle
	credentialTestThrottle    *credentialTestThrottle
}
