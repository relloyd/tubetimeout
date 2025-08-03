package led

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

var (
	sysfsPath = "/sys/class/leds"
)

type Config struct {
	Name              string
	EnableTrigger     string
	EnableBrightness  string
	DisableTrigger    string
	DisableBrightness string
}

type Controller struct {
	name       string
	basePath   string
	trigger    string
	brightness string
	logger     *zap.SugaredLogger
	exists     bool
	config     Config
}

// List of known LED configurations
var knownLEDs = []Config{
	{ // OrangePiZero3
		Name:              "red:status",
		EnableTrigger:     "heartbeat",
		EnableBrightness:  "",
		DisableTrigger:    "none",
		DisableBrightness: "0",
	},
	{ // RaspberryPi Zero 2w
		Name:              "ACT",
		EnableTrigger:     "heartbeat",
		EnableBrightness:  "",
		DisableTrigger:    "default-on",
		DisableBrightness: "1",
	},
}

func NewController(logger *zap.SugaredLogger) *Controller {
	for _, cfg := range knownLEDs {
		base := filepath.Join(sysfsPath, cfg.Name)
		if _, err := os.Stat(base); err == nil {
			logger.Infof("Using LED: %s", cfg.Name)
			return &Controller{
				name:       cfg.Name,
				basePath:   base,
				trigger:    filepath.Join(base, "trigger"),
				brightness: filepath.Join(base, "brightness"),
				logger:     logger,
				exists:     true,
				config:     cfg,
			}
		}
	}

	logger.Warn("No known LED sysfs path found. LED control will be disabled.")
	return &Controller{
		logger: logger,
		exists: false,
	}
}

func (l *Controller) EnableWarning() {
	if !l.exists {
		l.logger.Warn("EnableWarning called, but no LED available on this hardware.")
		return
	}
	l.writeLEDAttribute(l.trigger, l.config.EnableTrigger)
	l.writeLEDAttribute(l.brightness, l.config.EnableBrightness)
}

func (l *Controller) DisableWarning() {
	if !l.exists {
		l.logger.Warn("DisableWarning called, but no LED available on this hardware.")
		return
	}
	l.writeLEDAttribute(l.trigger, l.config.DisableTrigger)
	l.writeLEDAttribute(l.brightness, l.config.DisableBrightness)
}

// writeLEDAttribute writes the given value to the given sysfs file, if the value is not empty.
func (l *Controller) writeLEDAttribute(path, value string) {
	if value == "" {
		return
	}
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		l.logger.Warnf("Failed to write '%s' to %s: %v", value, path, err)
	}
}
