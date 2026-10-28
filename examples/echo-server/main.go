// Command echo-server runs an explicitly allowlisted, provisioned echo service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"

	"github.com/datagram-messenger/dgproto-go/examples/internal/echoapp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "echo server failed (check inputs and listener availability)")
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	flags := flag.NewFlagSet("echo-server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	address := flags.String("listen", "127.0.0.1:7443", "TCP listen address")
	identity := flags.String("identity", "", "provisioned raw 32-byte private-key file")
	clientPath := flags.String("allowed-client", "", "trusted raw 32-byte client public-key file")
	if err := flags.Parse(args); err != nil {
		return errors.New("invalid server options")
	}
	if flags.NArg() != 0 || *identity == "" || *clientPath == "" || *address == "" {
		return errors.New("missing server options")
	}
	key, err := echoapp.LoadIdentity(*identity)
	if err != nil {
		return err
	}
	client, err := echoapp.LoadPublic(*clientPath)
	if err != nil {
		return err
	}
	server, err := echoapp.NewServer(key, client)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return errors.Join(err, server.Close())
	}
	if _, err := io.WriteString(out, "echo server listening\n"); err != nil {
		return errors.Join(err, server.Close(), listener.Close())
	}
	return echoapp.Serve(ctx, server, listener)
}
