package signals

import (
	"os"
	"syscall"

	"github.com/YLonely/sync-util/log"
)

var HandledSignals = []os.Signal{
	syscall.SIGTERM,
	syscall.SIGINT,
}

func HandleSignals(hook func(), signals chan os.Signal, errorC chan error) chan struct{} {
	done := make(chan struct{}, 1)
	go func() {
		select {
		case s := <-signals:
			log.Logger.WithField("signal", s).Debug("get a signal")
		case err := <-errorC:
			log.Logger.WithError(err).Error("get error from error channel")
		}
		hook()
		close(done)
	}()
	return done
}
