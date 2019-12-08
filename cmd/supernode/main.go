package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/YLonely/sync-util/signals"

	"github.com/YLonely/sync-util/supernode"
	"github.com/urfave/cli"
)

func main() {
	var config supernode.Config

	app := cli.NewApp()
	app.Name = "supernode"
	app.Usage = "supernode provides synchronization and lock service for syncd nodes in cluster"
	app.Version = "v0.0.1"

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "port",
			Usage:       "Specify a port to listen on",
			Destination: &config.Port,
			Value:       "12345",
		},
	}

	app.Action = func(c *cli.Context) error {
		signalC := make(chan os.Signal, 2048)
		ctx := context.Background()
		s, err := supernode.NewServer(config)
		if err != nil {
			return err
		}
		errorC := s.Start()
		signal.Notify(signalC, signals.HandledSignals...)
		done := signals.HandleSignals(func() {
			s.Stop(ctx)
		}, signalC, errorC)
		<-done
		return nil
	}
	app.Run(os.Args)
}
