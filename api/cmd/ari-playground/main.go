// Command ari-playground is a learning tool, not a production binary: it
// reuses this repo's ari/ami/config packages to poke at a running Asterisk
// instance from the command line, so you can see what each API call and
// event actually looks like.
//
// It reads the same environment variables as atsap-api (see .env), so run
// it from the api/ directory with the dev stack up:
//
//	go run ./cmd/ari-playground watch
//	go run ./cmd/ari-playground originate PJSIP/1000
//	go run ./cmd/ari-playground answer <channel-id>
//	go run ./cmd/ari-playground play <channel-id> sound:hello-world
//	go run ./cmd/ari-playground hangup <channel-id> [reason]
//
// Typical session: run "watch" in one terminal to see events live, then
// use a softphone (or "originate") to place a call and drive it with
// "answer"/"play"/"hangup" from another terminal, watching the events each
// action produces.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"atsap-api/internal/config"
	"atsap-api/internal/logging"
	"atsap-api/internal/telephony/acl/ami"
	"atsap-api/internal/telephony/acl/ari"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fatal("config: %v (did you `cp .env.example .env` and export it, e.g. `set -a; source ../.env; set +a`?)", err)
	}

	// Quiet logger: this tool prints its own event lines, it doesn't need
	// the library's structured logs too.
	logger := logging.New("error")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ariClient := ari.New(cfg.ARIURL, cfg.ARIUsername, cfg.ARIPassword, cfg.ARIAppName)

	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "watch":
		watch(ctx, cfg, ariClient, logger)

	case "answer":
		requireArgs(args, 1, "answer <channel-id>")
		if err := ariClient.AnswerChannel(ctx, args[0]); err != nil {
			fatal("answer: %v", err)
		}
		fmt.Println("answered", args[0])

	case "hangup":
		requireArgs(args, 1, "hangup <channel-id> [reason]")
		reason := ""
		if len(args) > 1 {
			reason = args[1]
		}
		if err := ariClient.HangupChannel(ctx, args[0], reason); err != nil {
			fatal("hangup: %v", err)
		}
		fmt.Println("hung up", args[0])

	case "play":
		requireArgs(args, 2, "play <channel-id> <media>")
		id, err := ariClient.Play(ctx, args[0], args[1])
		if err != nil {
			fatal("play: %v", err)
		}
		fmt.Println("playback id:", id)

	case "originate":
		requireArgs(args, 1, "originate <endpoint> [callerID]")
		callerID := ""
		if len(args) > 1 {
			callerID = args[1]
		}
		id, err := ariClient.Originate(ctx, args[0], callerID)
		if err != nil {
			fatal("originate: %v", err)
		}
		fmt.Println("channel id:", id)

	default:
		usage()
		os.Exit(1)
	}
}

// watch connects to both the AMI event socket and the ARI event
// WebSocket and prints every event as one JSON line, prefixed with which
// API it came from. It does not answer or otherwise act on calls, so you
// can drive channels manually with the other subcommands while observing.
func watch(ctx context.Context, cfg config.Config, ariClient *ari.Client, logger *slog.Logger) {
	amiClient := ami.New(cfg.AMIAddr, cfg.AMIUsername, cfg.AMIPassword, func(msg ami.Message) {
		printEvent("ami", msg["Event"], msg)
	}, logger)

	go func() {
		for {
			if err := amiClient.Connect(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				fmt.Fprintln(os.Stderr, "[ami] reconnecting:", err)
				time.Sleep(2 * time.Second)
				continue
			}
		}
	}()

	go func() {
		err := ariClient.StreamEvents(ctx, func(ev ari.Event) {
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(ev.Raw, &raw)
			printEvent("ari", ev.Type, raw)
		}, logger)
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "[ari] stream stopped:", err)
		}
	}()

	fmt.Printf("watching AMI (%s) + ARI (%s) events for app %q. Ctrl+C to stop.\n",
		cfg.AMIAddr, cfg.ARIURL, cfg.ARIAppName)
	<-ctx.Done()
	_ = amiClient.Close()
}

func printEvent(source, kind string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte(fmt.Sprintf("%v", data))
	}
	fmt.Printf("[%s] %-24s %s\n", source, kind, b)
}

func requireArgs(args []string, n int, usage string) {
	if len(args) < n {
		fatal("usage: ari-playground %s", usage)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `ari-playground: explore Asterisk's ARI/AMI APIs and events live.

Usage:
  ari-playground watch                              stream AMI + ARI events
  ari-playground answer <channel-id>                 answer a channel
  ari-playground hangup <channel-id> [reason]         hang up a channel
  ari-playground play <channel-id> <media>            e.g. media=sound:hello-world
  ari-playground originate <endpoint> [callerID]      e.g. endpoint=PJSIP/1000`)
}
