package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/YLonely/sync-util/signals"

	"github.com/YLonely/sync-util/syncd"
	"github.com/urfave/cli"
)

func main() {
	var config syncd.Config

	app := cli.NewApp()
	app.Name = "syncd"
	app.Usage = "syncd monitors the update(add) of the related dirs of docker images and syncs the content in dirs with remote storage dir"
	app.Version = "v0.0.1"

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "node-ip",
			Usage:       "Specify the ip of the super node",
			Destination: &(config.SuperNodeIP),
			Value:       "",
		},
		&cli.StringFlag{
			Name:        "node-port",
			Usage:       "Specify the port of the super node",
			Destination: &(config.SuperNodePort),
			Value:       "",
		},
		&cli.StringFlag{
			Name:        "sync-dir",
			Usage:       "Specify the dir to sync to",
			Destination: &(config.SyncDir),
			Required:    true,
		},
	}

	app.Action = func(c *cli.Context) error {
		signalC := make(chan os.Signal, 2048)
		ctx := context.Background()
		s, err := syncd.NewServer(config)
		if err != nil {
			return err
		}
		errorC := s.Start(ctx)
		signal.Notify(signalC, signals.HandledSignals...)
		done := signals.HandleSignals(func() {
			s.Stop(ctx)
		}, signalC, errorC)
		<-done
		return nil
	}

	app.Run(os.Args)

}
