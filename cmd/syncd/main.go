package main

import (
	"os"

	"github.com/YLonely/sync-util/syncd"
	"github.com/urfave/cli"
)

const (
	defaultImageMetaDataDir  = "/var/lib/docker/image/overlay2"
	defaultImageLayerDataDir = "/var/lib/docker/overlay2"
)

func main() {
	var config syncd.Config

	app := cli.NewApp()
	app.Name = "syncd"
	app.Usage = "syncd monitors the update(add) of the related dirs of docker images and syncs the content in dirs with remote storage dir"
	app.Version = "v0.0.1"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "node-ip",
			Usage:       "Specify the ip of the super node",
			Destination: &(config.SuperNodeIP),
			Required:    true,
		},
		cli.StringFlag{
			Name:        "node-port",
			Usage:       "Specify the port of the super node",
			Destination: &(config.SuperNodePort),
			Required:    true,
		},
		cli.StringFlag{
			Name:        "meta-dir",
			Value:       defaultImageMetaDataDir,
			Usage:       "Specify the dir path in which the docker's storage driver save the meta data of images",
			Destination: &(config.ImageMetaDataDir),
		},
		cli.StringFlag{
			Name:        "layer-dir",
			Value:       defaultImageLayerDataDir,
			Usage:       "Specify the dir path in which the docker's storage driver save the image layer data",
			Destination: &(config.ImageLayerDataDir),
		},
		cli.StringFlag{
			Name:        "sync-dir",
			Usage:       "Specify the dir to sync to",
			Destination: &(config.SyncDir),
			Required:    true,
		},
	}

	app.Action = func(c *cli.Context) error {
		return nil
	}

	app.Run(os.Args)

}
