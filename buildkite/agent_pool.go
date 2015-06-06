package buildkite

import (
	"fmt"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/buildkite/agent/signalwatcher"
	"os"
)

type AgentPool struct {
	APIClient                      *api.Client
	Token                          string
	ConfigFilePath                 string
	Name                           string
	Priority                       string
	BootstrapScript                string
	BuildPath                      string
	HooksPath                      string
	MetaData                       []string
	MetaDataEC2Tags                bool
	AutoSSHFingerprintVerification bool
	CommandEval                    bool
	RunInPty                       bool
	Endpoint                       string
}

func (r *AgentPool) Start() error {
	// Show the welcome banner and config options used
	r.ShowBanner()

	// Create the agent registration API Client
	r.APIClient = APIClient{Endpoint: r.Endpoint, Token: r.Token}.Create()

	// Create the agent template. We use pass this template to the register
	// call, at which point we get back a real agent.
	template := r.CreateAgentTemplate()

	logger.Info("Registering agent with Buildkite...")

	// Register the agent
	registered, err := r.RegisterAgent(template)
	if err != nil {
		logger.Fatal("%s", err)
	}

	logger.Info("Successfully registered agent \"%s\" with meta-data %s", registered.Name, registered.MetaData)

	// Now that we have a registereted agent, we can connect it to the API,
	// and start running jobs.
	agent := Agent{Agent: registered, Endpoint: r.Endpoint}.Create()

	logger.Info("Connecting to Buildkite...")

	if err := agent.Connect(); err != nil {
		logger.Fatal("%s", err)
	}

	logger.Info("Agent successfully connected")
	logger.Info("You can press Ctrl-C to stop the agent")
	logger.Info("Waiting for work...")

	return nil

	// // Start the signal watcher
	signalwatcher.Watch(func(sig signalwatcher.Signal) {
		if sig == signalwatcher.QUIT {
			logger.Debug("Received signal `%s`", sig.String())

			// If this is the second quit signal, or if the
			// agent doesnt' have a job.
			//if r.stopping || r.jobRunner == nil {
			//	// r.Stop(&agent)
			//}

			//if r.jobRunner != nil {
			//	logger.Warn("Waiting for job to finish before stopping. Send the signal again to exit immediately.")
			//	r.jobRunner.Kill()
			//}

			//r.stopping = true
		} else {
			logger.Debug("Ignoring signal `%s`", sig.String())
		}
	})

	return nil
}

func (r *AgentPool) CreateAgentTemplate() *api.Agent {
	agent := &api.Agent{
		Name:              r.Name,
		Priority:          r.Priority,
		MetaData:          r.MetaData,
		ScriptEvalEnabled: r.CommandEval,
		Version:           Version(),
		PID:               os.Getpid(),
	}

	// Attempt to add the EC2 tags
	if r.MetaDataEC2Tags {
		tags, err := EC2Tags{}.Get()
		if err != nil {
			// Don't blow up if we can't find them, just show a nasty error.
			logger.Error(fmt.Sprintf("Failed to find EC2 Tags: %s", err.Error()))
		} else {
			for tag, value := range tags {
				agent.MetaData = append(agent.MetaData, fmt.Sprintf("%s=%s", tag, value))
			}
		}
	}

	// Add the hostname
	agent.Hostname, _ = os.Hostname()

	// Add the OS dump
	agent.OS, _ = OSDump()

	return agent
}

func (r *AgentPool) RegisterAgent(agent *api.Agent) (*api.Agent, error) {
	var registered *api.Agent
	var err error

	register := func(s *retry.Stats) error {
		registered, _, err = r.APIClient.Agents.Register(agent)
		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}

		return err
	}

	err = retry.Do(register, &retry.Config{Maximum: 30})

	return registered, err
}

func (r *AgentPool) ShowBanner() {
	welcomeMessage :=
		"\n" +
			"%s  _           _ _     _ _    _ _                                _\n" +
			" | |         (_) |   | | |  (_) |                              | |\n" +
			" | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_\n" +
			" | '_ \\| | | | | |/ _` | |/ / | __/ _ \\  / _` |/ _` |/ _ \\ '_ \\| __|\n" +
			" | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_\n" +
			" |_.__/ \\__,_|_|_|\\__,_|_|\\_\\_|\\__\\___|  \\__,_|\\__, |\\___|_| |_|\\__|\n" +
			"                                                __/ |\n" +
			" http://buildkite.com/agent                    |___/\n%s\n"

	if logger.ColorsEnabled() {
		fmt.Fprintf(logger.OutputPipe(), welcomeMessage, "\x1b[32m", "\x1b[0m")
	} else {
		fmt.Fprintf(logger.OutputPipe(), welcomeMessage, "", "")
	}

	logger.Notice("Starting buildkite-agent v%s with PID: %s", Version(), fmt.Sprintf("%d", os.Getpid()))
	logger.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
	logger.Notice("For questions and support, email us at: hello@buildkite.com")

	if r.ConfigFilePath != "" {
		logger.Info("Configuration loaded from: %s", r.ConfigFilePath)
	}

	logger.Debug("Bootstrap script: %s", r.BootstrapScript)
	logger.Debug("Build path: %s", r.BuildPath)
	logger.Debug("Hooks directory: %s", r.HooksPath)

	if !r.AutoSSHFingerprintVerification {
		logger.Debug("Automatic SSH fingerprint verification has been disabled")
	}

	if !r.CommandEval {
		logger.Debug("Evaluating console commands has been disabled")
	}

	if !r.RunInPty {
		logger.Debug("Running builds within a pseudoterminal (PTY) has been disabled")
	}
}
