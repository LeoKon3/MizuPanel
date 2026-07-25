package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/user"
	"time"

	agentconfig "github.com/mizupanel/mizupanel/internal/agent/config"
	agentdocker "github.com/mizupanel/mizupanel/internal/agent/docker"
	agentkubectl "github.com/mizupanel/mizupanel/internal/agent/kubectl"
	agentlogtail "github.com/mizupanel/mizupanel/internal/agent/logtail"
	agentmanagement "github.com/mizupanel/mizupanel/internal/agent/management"
	"github.com/mizupanel/mizupanel/internal/agent/metrics"
	agentprocess "github.com/mizupanel/mizupanel/internal/agent/process"
	agentsystemd "github.com/mizupanel/mizupanel/internal/agent/systemd"
	agentterminal "github.com/mizupanel/mizupanel/internal/agent/terminal"
	agentupgrade "github.com/mizupanel/mizupanel/internal/agent/upgrade"
	agentws "github.com/mizupanel/mizupanel/internal/agent/ws"
	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/version"
)

func main() {
	handled, err := version.PrintCommand(os.Args[1:], "Agent", os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	if handled {
		return
	}

	configPath := flag.String("config", "", "path to agent config file")
	flag.Parse()

	if err := runAgentEntrypoint(*configPath); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}

func currentUsername() string {
	current, err := user.Current()
	if err != nil || current == nil {
		return ""
	}
	if current.Username != "" {
		return current.Username
	}
	return current.Uid
}

func runAgent(ctx context.Context, configPath string) error {
	cfg, err := agentconfig.Load(configPath)
	if err != nil {
		return err
	}
	collector := metrics.NewCollector()
	processCollector := agentprocess.NewCollector()
	var dockerCollector *agentdocker.Collector
	var composeHandler *agentdocker.ComposeHandler
	if cfg.EnableDocker {
		dockerCollector = agentdocker.NewCollector()
		composeHandler = agentdocker.NewComposeHandler()
	}
	initialSnapshot, err := collector.Collect()
	if err != nil {
		return err
	}
	if initialSnapshot.Hostname == "" {
		initialSnapshot.Hostname = cfg.Name
	}
	client := agentws.NewClient(cfg.ServerURL, cfg.Token)
	client.SetDebug(cfg.Debug)
	if cfg.Debug {
		log.Printf("[debug][agent] debug logging enabled")
	}
	upgrader := agentupgrade.New(cfg.ServerURL, version.Current, cfg.AgentMode)
	if cfg.AgentMode == "ops" {
		client.SetConnectedHandler(upgrader.ConfirmSuccessfulStart)
	}
	client.SetAgentManagementHandler(agentmanagement.NewHandler(agentmanagement.Options{
		Version:         version.Current,
		User:            currentUsername(),
		Mode:            cfg.AgentMode,
		TerminalEnabled: cfg.EnableTerminal && agentterminal.Supported(),
		DockerStatus: func() (bool, string) {
			if dockerCollector == nil {
				return false, "Docker 监控未启用"
			}
			snapshot := dockerCollector.Collect()
			return snapshot.Available, snapshot.Error
		},
		ConfigPath: configPath,
		StartTime:  time.Now(),
		Upgrade:    upgrader.Start,
	}))
	client.SetTerminalHandlerFactory(func(sender agentws.TerminalSender) agentws.TerminalHandler {
		return agentterminal.NewManager(cfg.EnableTerminal, sender)
	})
	client.SetContainerExecHandlerFactory(func(sender agentws.ContainerExecSender) agentws.ContainerExecHandler {
		return agentdocker.NewExecManager(cfg.EnableDocker && cfg.EnableTerminal, sender)
	})
	client.SetLogTailHandler(agentlogtail.NewHandler())
	systemdHandler := agentsystemd.NewHandler()
	client.SetSystemdServiceHandler(systemdHandler)
	kubectlHandler := agentkubectl.NewHandler()
	kubectlHandler.SetDebug(cfg.Debug)
	client.SetKubectlHandler(kubectlHandler)
	if dockerCollector != nil {
		client.SetContainerLogsHandler(agentdocker.NewLogsHandler(dockerCollector))
		client.SetDockerExecHandler(agentdocker.NewExecHandler())
		client.SetContainerOperationsHandler(agentdocker.NewOperationsHandlerWithCompose(dockerCollector, composeHandler))
	}
	if configPath != "" {
		client.SetNodeTokenHandler(func(token string) error {
			return agentconfig.SaveToken(configPath, token)
		})
	}
	return client.RunForever(ctx, protocol.HelloMessage{
		Type:                        protocol.MessageTypeHello,
		NodeID:                      cfg.NodeID,
		AgentVersion:                version.Current,
		ProtocolVersion:             protocol.CurrentProtocolVersion,
		IdentitySource:              cfg.IdentitySource,
		Hostname:                    initialSnapshot.Hostname,
		Name:                        cfg.Name,
		IP:                          initialSnapshot.IP,
		OS:                          initialSnapshot.OS,
		Arch:                        initialSnapshot.Arch,
		Kernel:                      initialSnapshot.Kernel,
		Terminal:                    cfg.EnableTerminal && agentterminal.Supported(),
		DockerCompose:               composeHandler != nil,
		DockerComposeServiceActions: composeHandler != nil && composeHandler.SupportsServiceActions(),
		DockerComposeDeployment:     composeHandler != nil && composeHandler.SupportsDeployment(),
		DockerResources:             dockerCollector != nil,
		SystemdServices:             systemdHandler.Supported(),
		AgentMode:                   cfg.AgentMode,
		AgentUser:                   currentUsername(),
		AgentManagement:             true,
		AgentUpgrade:                cfg.AgentMode == "ops",
	}, cfg.Interval, 3*time.Second, func(nodeID string, timestamp int64) (protocol.MetricsMessage, error) {
		snapshot, err := collector.Collect()
		if err != nil {
			return protocol.MetricsMessage{}, err
		}
		if snapshot.Hostname == "" {
			snapshot.Hostname = cfg.Name
		}
		message := snapshot.ToMessage(nodeID, timestamp)
		processSnapshot := processCollector.Collect()
		message.ProcessSnapshot = &processSnapshot
		if dockerCollector != nil {
			dockerSnapshot := dockerCollector.Collect()
			message.DockerSnapshot = &dockerSnapshot
		}
		return message, nil
	})
}
