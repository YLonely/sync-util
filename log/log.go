package log

import (
	"os"

	"github.com/sirupsen/logrus"
)

// Logger is the overall logger
var Logger = logrus.New()

func init() {
	Logger.SetOutput(os.Stdout)
	Logger.SetLevel(logrus.DebugLevel)
}
