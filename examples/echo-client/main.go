// Command echo-client sends one validated echo to an independently run server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/datagram-messenger/dgproto-go/examples/internal/echoapp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "echo client failed (check inputs, trust and server availability)")
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	flags := flag.NewFlagSet("echo-client", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // Never echo input paths or unexpected arguments.
	address := flags.String("address", "127.0.0.1:7443", "server TCP address")
	identity := flags.String("identity", "", "provisioned raw 32-byte private-key file")
	pinPath := flags.String("server-pin", "", "trusted raw 32-byte server public-key file")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			_, err = io.WriteString(out, "Usage: echo-client [options]\n  -address string        server TCP address (default 127.0.0.1:7443)\n  -identity string       provisioned raw 32-byte private-key file (required)\n  -server-pin string     trusted raw 32-byte server public-key file (required)\n  -h, -help              show help without loading keys or dialing\n")
			return err
		}
		return errors.New("invalid client options")
	}
	if flags.NArg() != 0 || *identity == "" || *pinPath == "" || *address == "" {
		return errors.New("missing client options")
	}
	key, err := echoapp.LoadIdentity(*identity)
	if err != nil {
		return err
	}
	pin, err := echoapp.LoadPublic(*pinPath)
	if err != nil {
		return err
	}
	if err := echoapp.Exchange(ctx, *address, key, pin); err != nil {
		return err
	}
	_, err = io.WriteString(out, "authenticated application echo received\n")
	return err
}
