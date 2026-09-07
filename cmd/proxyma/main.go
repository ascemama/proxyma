package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/api"
	"proxyma/internal/buildinfo"
	"proxyma/internal/config"
	"proxyma/internal/executor"
	"proxyma/internal/interaction"
	"proxyma/internal/interaction/history"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/sshclient"
	"proxyma/internal/store/jsonfile"
	"proxyma/internal/store/sqlitehistory"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/tasks"
	"proxyma/internal/tlsserver"
	"proxyma/internal/users"
	"proxyma/internal/web"
	"proxyma/internal/winrmclient"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		info := buildinfo.Current()
		fmt.Printf("Proxyma %s (commit %s)\n", info.Version, info.Commit)
		return
	}

	cfg, err := config.Load(os.Args[1:], os.LookupEnv)
	if err != nil {
		log.Fatalf("parse configuration: %v", err)
	}
	logger := log.Default()
	sshTrustStore := &sshclient.TrustStore{Path: cfg.KnownHostsPath}
	if err := sshTrustStore.Ensure(); err != nil {
		log.Fatalf("initialize SSH trust store: %v", err)
	}

	registry, diagnostics, err := targets.LoadDir(cfg.TargetDir)
	if err != nil {
		log.Fatalf("load targets: %v", err)
	}
	for _, diagnostic := range diagnostics {
		log.Printf("target rejected: %s: %s", diagnostic.Path, diagnostic.Message)
	}
	policies, policyDiagnostics, err := policy.LoadDir(cfg.PolicyDir)
	if err != nil {
		log.Fatalf("load policies: %v", err)
	}
	for _, diagnostic := range policyDiagnostics {
		log.Printf("policy rejected: %s: %s", diagnostic.Path, diagnostic.Message)
	}
	if len(policyDiagnostics) > 0 {
		log.Fatalf("load policies: %d policy file(s) rejected", len(policyDiagnostics))
	}
	if referenceDiagnostics := validatePolicyReferences(registry, policies); len(referenceDiagnostics) > 0 {
		for _, diagnostic := range referenceDiagnostics {
			log.Printf("target policy rejected: %s: %s", diagnostic.Path, diagnostic.Message)
		}
		log.Fatalf("load policies: %d target policy reference(s) rejected", len(referenceDiagnostics))
	}
	userRegistry, userDiagnostics, err := users.LoadDir(cfg.UserDir)
	if err != nil {
		log.Fatalf("load users: %v", err)
	}
	for _, diagnostic := range userDiagnostics {
		log.Printf("user rejected: %s: %s", diagnostic.Path, diagnostic.Message)
	}
	if len(userRegistry.All()) == 0 {
		log.Printf("no users configured; create an administrator with proxymactl")
	}
	agentRegistry, agentDiagnostics, err := agents.LoadDir(cfg.AgentDir)
	if err != nil {
		log.Fatalf("load agents: %v", err)
	}
	for _, diagnostic := range agentDiagnostics {
		log.Printf("agent rejected: %s: %s", diagnostic.Path, diagnostic.Message)
	}
	if !agentRegistry.HasEnabled() {
		log.Printf("no enabled agents configured; agent authentication is unavailable")
	}
	interactionRegistry, interactionDiagnostics, err := interactioninstances.LoadDir(cfg.InteractionConfigDir)
	if err != nil {
		log.Fatalf("load interaction instances: %v", err)
	}
	for _, diagnostic := range interactionDiagnostics {
		log.Printf("interaction instance rejected: %s: %s", diagnostic.Path, diagnostic.Message)
	}
	if len(interactionDiagnostics) > 0 {
		log.Fatalf("load interaction instances: %d interaction instance file(s) rejected", len(interactionDiagnostics))
	}
	if referenceDiagnostics := validateAgentReferences(registry, agentRegistry); len(referenceDiagnostics) > 0 {
		for _, diagnostic := range referenceDiagnostics {
			log.Printf("target agent rejected: %s: %s", diagnostic.Path, diagnostic.Message)
		}
		log.Fatalf("load agents: %d target agent reference(s) rejected", len(referenceDiagnostics))
	}
	if referenceDiagnostics := validateUserReferences(registry, agentRegistry, userRegistry); len(referenceDiagnostics) > 0 {
		for _, diagnostic := range referenceDiagnostics {
			log.Printf("user ownership rejected: %s: %s", diagnostic.Path, diagnostic.Message)
		}
		log.Fatalf("load users: %d user ownership reference(s) rejected", len(referenceDiagnostics))
	}
	if referenceDiagnostics := validateAgentInteractionReferences(agentRegistry, interactionRegistry); len(referenceDiagnostics) > 0 {
		for _, diagnostic := range referenceDiagnostics {
			log.Printf("agent interaction rejected: %s: %s", diagnostic.Path, diagnostic.Message)
		}
		log.Fatalf("load interaction instances: %d agent interaction reference(s) rejected", len(referenceDiagnostics))
	}
	if referenceDiagnostics := validateInteractionCreatorReferences(interactionRegistry, userRegistry); len(referenceDiagnostics) > 0 {
		for _, diagnostic := range referenceDiagnostics {
			log.Printf("interaction owner rejected: %s: %s", diagnostic.Path, diagnostic.Message)
		}
		log.Fatalf("load interaction instances: %d interaction owner reference(s) rejected", len(referenceDiagnostics))
	}

	provider, err := interactionProvider(interactionRegistry, cfg.Debug, logger)
	if err != nil {
		log.Fatalf("configure interaction provider: %v", err)
	}
	taskStore := jsonfile.NewTaskStore(cfg.TaskDir)
	interactionHistoryStore, err := sqlitehistory.Open(cfg.InteractionHistoryDB)
	if err != nil {
		log.Fatalf("open interaction history db: %v", err)
	}
	defer interactionHistoryStore.Close()
	interactionStore := jsonfile.NewInteractionStore(cfg.InteractionDir)
	indexingInteractionStore := history.NewIndexingInteractionStore(interactionStore, interactionHistoryStore, agentRegistry, interactionRegistry, logger.Printf)
	interactionService := interaction.New(indexingInteractionStore, interaction.Options{
		Provider: provider,
		Timeout:  cfg.InteractionTimeout,
	})
	userSessionStore := users.NewSessionStore(cfg.UserSessionDir)
	credentialTokenStore := users.NewCredentialTokenStore(cfg.CredentialTokenDir)
	if err := interactionService.Recover(); err != nil {
		log.Fatalf("recover interactions: %v", err)
	}
	sshRunner := sshclient.Runner{
		Timeout:               cfg.CommandTimeout,
		OutputLimit:           cfg.OutputLimit,
		DefaultKnownHostsPath: cfg.KnownHostsPath,
		Debug:                 cfg.Debug,
		Logger:                logger,
		TrustStore:            sshTrustStore,
	}
	winrmRunner := winrmclient.Runner{
		Timeout: cfg.CommandTimeout, OutputLimit: cfg.OutputLimit, Debug: cfg.Debug, Logger: logger,
	}
	manager := tasks.NewManagerWithOptions(taskStore, executor.Executor{
		Timeout:     cfg.CommandTimeout,
		OutputLimit: cfg.OutputLimit,
	}, tasks.ManagerOptions{
		Debug:        cfg.Debug,
		Logger:       logger,
		Interactions: interactionService,
		SSHClient:    sshRunner,
		WinRMClient:  winrmRunner,
	})
	indexingInteractionStore.SetTaskFinder(manager.Get)
	if err := manager.Recover(); err != nil {
		log.Fatalf("recover tasks: %v", err)
	}
	if cfg.RebuildInteractionHistory {
		interactionRecords, err := interactionStore.LoadAll()
		if err != nil {
			log.Fatalf("load interactions for history rebuild: %v", err)
		}
		taskRecords, err := taskStore.LoadAll()
		if err != nil {
			log.Fatalf("load tasks for history rebuild: %v", err)
		}
		diagnostics, err := history.Rebuild(context.Background(), interactionHistoryStore, interactionRecords, taskRecords, agentRegistry, interactionRegistry)
		if err != nil {
			log.Fatalf("rebuild interaction history: %v", err)
		}
		for _, diagnostic := range diagnostics {
			log.Printf("interaction history rebuild diagnostic: interaction_id=%s task_id=%s: %s", diagnostic.InteractionID, diagnostic.TaskID, diagnostic.Message)
		}
		log.Printf("interaction history rebuild completed interactions=%d diagnostics=%d", len(interactionRecords), len(diagnostics))
	}
	policyEvaluator := policy.NewMutableEvaluator(policies)
	flow := taskflow.New(manager, interactionService, taskflow.Options{
		RequireCommandApproval: cfg.RequireCommandApproval,
		Policies:               policyEvaluator,
	})
	scheduler := taskflow.NewScheduler(flow, 0)
	scheduler.Start(context.Background())

	if cfg.Debug {
		logger.Printf("debug logging enabled")
	}
	server := &api.Server{
		Agents:              agentRegistry,
		Users:               userRegistry,
		UserSessions:        userSessionStore,
		CredentialTokens:    credentialTokenStore,
		Targets:             registry,
		Policies:            policyEvaluator,
		Tasks:               manager,
		Interactions:        interactionService,
		InteractionHistory:  interactionHistoryStore,
		InteractionProvider: provider,
		SSHClient:           sshRunner,
		WinRMClient:         winrmRunner,
		ManagementPaths: users.Paths{
			UserDir:              cfg.UserDir,
			CredentialTokenDir:   cfg.CredentialTokenDir,
			AgentDir:             cfg.AgentDir,
			TargetDir:            cfg.TargetDir,
			PolicyDir:            cfg.PolicyDir,
			InteractionConfigDir: cfg.InteractionConfigDir,
		},
		CommandTimeout:            cfg.CommandTimeout,
		OutputLimit:               cfg.OutputLimit,
		KnownHostsPath:            cfg.KnownHostsPath,
		UserSessionTTL:            cfg.UserSessionTTL,
		UserActivationTokenTTL:    cfg.UserActivationTokenTTL,
		UserActivationTokenMaxTTL: cfg.UserActivationTokenMaxTTL,
		UseDevelopmentCookies:     usesDevelopmentCookies(cfg.Scheme),
		BuildInfo: func() api.BuildInfoResponse {
			info := buildinfo.Current()
			return api.BuildInfoResponse{Version: info.Version, Commit: info.Commit}
		}(),
		Debug:  cfg.Debug,
		Logger: logger,
	}
	httpServer := &http.Server{
		Addr:      cfg.ListenAddress,
		Handler:   web.Wrap(server.Routes()),
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	log.Printf("Proxyma listening on %s://%s", cfg.Scheme, cfg.ListenAddress)
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		log.Fatalf("listen for Proxyma: %v", err)
	}
	if serveErr := serveConfigured(httpServer, listener, cfg, logger); serveErr != nil {
		log.Fatalf("serve Proxyma: %v", serveErr)
	}
}

func usesDevelopmentCookies(scheme string) bool {
	return scheme == config.SchemeHTTP
}

func serveConfigured(httpServer *http.Server, listener net.Listener, cfg config.Config, logger *log.Logger) error {
	if cfg.Scheme == config.SchemeHTTPS {
		info, err := tlsserver.Prepare(tlsserver.Options{
			Mode: cfg.TLSMode, CertificateFile: cfg.TLSCertificateFile,
			PrivateKeyFile: cfg.TLSPrivateKeyFile, ServerName: cfg.TLSServerName,
			ListenAddress: cfg.ListenAddress,
		})
		if err != nil {
			return fmt.Errorf("prepare Proxyma TLS: %w", err)
		}
		logger.Printf("Proxyma TLS certificate fingerprint_sha256=%s not_after=%s generated=%t",
			info.FingerprintSHA256, info.NotAfter.Format(time.RFC3339), info.Generated)
		return httpServer.ServeTLS(listener, cfg.TLSCertificateFile, cfg.TLSPrivateKeyFile)
	}
	logger.Printf("WARNING: Proxyma HTTP mode disables Secure session cookies; use only for development or debugging")
	return httpServer.Serve(listener)
}

func validatePolicyReferences(registry targets.Registry, policies *policy.Set) []policy.Diagnostic {
	var diagnostics []policy.Diagnostic
	for _, target := range registry.All() {
		label := target.Hostname
		if label == "" {
			label = target.IP
		}
		diagnostics = append(diagnostics, policies.ValidateReferences(label, target.PolicyIDs)...)
	}
	return diagnostics
}

func validateAgentReferences(registry targets.Registry, agentRegistry agents.Registry) []policy.Diagnostic {
	var diagnostics []policy.Diagnostic
	for _, target := range registry.All() {
		label := target.Hostname
		if label == "" {
			label = target.IP
		}
		for _, agentID := range target.AgentIDs {
			if _, found := agentRegistry.Find(agentID); !found {
				diagnostics = append(diagnostics, policy.Diagnostic{
					Path:    label,
					Message: fmt.Sprintf("unknown agent id %q", agentID),
				})
			}
		}
	}
	return diagnostics
}

func validateUserReferences(registry targets.Registry, agentRegistry agents.Registry, userRegistry users.Registry) []policy.Diagnostic {
	var diagnostics []policy.Diagnostic
	for _, target := range registry.All() {
		label := target.Hostname
		if label == "" {
			label = target.IP
		}
		for _, userID := range target.UserIDs {
			if _, found := userRegistry.Find(userID); !found {
				diagnostics = append(diagnostics, policy.Diagnostic{
					Path:    label,
					Message: fmt.Sprintf("unknown user id %q", userID),
				})
			}
		}
	}
	for _, agent := range agentRegistry.All() {
		for _, userID := range agent.UserIDs {
			if _, found := userRegistry.Find(userID); !found {
				diagnostics = append(diagnostics, policy.Diagnostic{
					Path:    agent.ID,
					Message: fmt.Sprintf("unknown user id %q", userID),
				})
			}
		}
	}
	return diagnostics
}

func validateAgentInteractionReferences(agentRegistry agents.Registry, interactionRegistry interactioninstances.Registry) []policy.Diagnostic {
	var diagnostics []policy.Diagnostic
	for _, agent := range agentRegistry.All() {
		instanceID := interactioninstances.NormalizeInstanceID(agent.InteractionInstanceID)
		if !interactionRegistry.HasEnabled(instanceID) {
			diagnostics = append(diagnostics, policy.Diagnostic{
				Path:    agent.ID,
				Message: fmt.Sprintf("unknown or disabled interaction instance id %q", instanceID),
			})
		}
	}
	return diagnostics
}

func validateInteractionCreatorReferences(interactionRegistry interactioninstances.Registry, userRegistry users.Registry) []policy.Diagnostic {
	var diagnostics []policy.Diagnostic
	for _, definition := range interactionRegistry.All() {
		metadata := definition.Metadata()
		if _, found := userRegistry.Find(metadata.CreatorUserID); !found {
			diagnostics = append(diagnostics, policy.Diagnostic{
				Path:    metadata.ID,
				Message: fmt.Sprintf("unknown creator user id %q", metadata.CreatorUserID),
			})
		}
	}
	return diagnostics
}

func interactionProvider(registry interactioninstances.Registry, debug bool, logger *log.Logger) (*interactioninstances.InteractionRouter, error) {
	providers, err := interactioninstances.BuildProviders(registry, interactioninstances.ProviderRuntimeOptions{
		Debug:  debug,
		Logger: logger,
	})
	if err != nil {
		return nil, err
	}
	return interactioninstances.NewInteractionRouter(providers), nil
}
