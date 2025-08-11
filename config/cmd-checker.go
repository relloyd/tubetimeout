package config

import (
	"fmt"
	"os/exec"
)

func CheckCmdAvailability(cmd string) error {
	_, err := exec.LookPath(cmd)
	if err != nil {
		return fmt.Errorf("%v command not found on the system: %w", cmd, err)
	}
	return nil
}