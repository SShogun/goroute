package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func NewLaunchCmd() *cobra.Command {
	launchCmd := &cobra.Command{
		Use:                "launch [agent] [args...]",
		Short:              "Launch an AI coding agent with GoRoute proxy environment injected",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExecutableWithGoRoute(args)
		},
	}
	return launchCmd
}

func NewRunCmd() *cobra.Command {
	runCmd := &cobra.Command{
		Use:                "run <target> [args...]",
		Short:              "Run any CLI tool or coding agent seamlessly routed through GoRoute",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExecutableWithGoRoute(args)
		},
	}
	return runCmd
}

func runExecutableWithGoRoute(args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage: goroute run <target> [args...]")
		fmt.Println("       goroute launch <target> [args...]")
		fmt.Println("\nExamples:")
		fmt.Println("  goroute run claude")
		fmt.Println("  goroute run codex --profile goroute")
		fmt.Println("  goroute run aider --model openai/claude-3-7-sonnet")
		fmt.Println("  goroute run opencode")
		fmt.Println("  goroute run goose")
		return nil
	}

	target := args[0]
	extraArgs := args[1:]

	binPath, err := exec.LookPath(target)
	if err != nil {
		return fmt.Errorf("executable '%s' not found in PATH: %w", target, err)
	}

	c := exec.Command(binPath, extraArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	// Inherit existing environment and inject universal proxy settings
	env := os.Environ()
	proxyVars := []string{
		"OPENAI_BASE_URL=http://127.0.0.1:20128/v1",
		"OPENAI_API_BASE=http://127.0.0.1:20128/v1",
		"OPENAI_API_KEY=goroute",
		"ANTHROPIC_BASE_URL=http://127.0.0.1:20128",
		"ANTHROPIC_AUTH_TOKEN=goroute",
		"CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1",
		"GOOSE_PROVIDER=openai",
		"OPENAI_HOST=http://127.0.0.1:20128",
		"GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:20128",
		"GEMINI_API_KEY=goroute",
	}

	c.Env = append(env, proxyVars...)

	// Signal forwarding
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range sigChan {
			if c.Process != nil {
				_ = c.Process.Signal(sig)
			}
		}
	}()

	return c.Run()
}
