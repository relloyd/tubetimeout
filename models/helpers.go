package models

import (
	"strings"
)

func NewMAC(mac string) string {  // TODO: convert MAC string to a model.MAC and return it here plus everywhere.
	mac = strings.Replace(mac, ":", "-", -1)
	mac = strings.ToUpper(mac)
	return mac
}
