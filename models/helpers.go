package models

import (
	"fmt"
	"net"
	"strings"
)

func NewMAC(mac string) string { // TODO: convert MAC string to a model.MAC and return it here plus everywhere.
	mac = strings.Replace(mac, ":", "-", -1)
	mac = strings.ToUpper(mac)
	return mac
}

func NewGroup(group string) string {
	return strings.Replace(group, "/", "", -1)
}

func NewMapGroupTrackerConfig() MapGroupTrackerConfig {
	return make(MapGroupTrackerConfig)
}

func (m *MAC) WithColons() string {
	if m == nil {
		return ""
	}
	return strings.Replace(string(*m), "-", ":", -1)
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
// It converts hyphens to colons and validates the MAC address format.
func (m *MAC) UnmarshalText(text []byte) error {
	if m == nil {
		return fmt.Errorf("MAC: UnmarshalText on nil pointer")
	}
	macStr := strings.ReplaceAll(string(text), "-", ":") // Convert hyphens to colons to support both formats.
	// Validate the MAC address using net.ParseMAC.
	_, err := net.ParseMAC(macStr)
	if err != nil {
		return err
	}
	// Store our friendly representation using hyphens.
	*m = MAC(NewMAC(string(text)))
	return nil
}
