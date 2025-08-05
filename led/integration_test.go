//go:build integration

package led

import (
	"testing"

	"relloyd/tubetimeout/config"
)

func TestController_EnableWarning(t *testing.T) {
	lc := NewController(config.MustGetLogger())
	lc.EnableWarning()
}

func TestController_DisableWarning(t *testing.T) {
	lc := NewController(config.MustGetLogger())
	lc.DisableWarning()
}
