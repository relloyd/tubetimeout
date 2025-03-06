package main

import (
	"fmt"
	"net"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func getNetAdapterName() string {
	switch os := runtime.GOOS; os {
	case "darwin":
		return "en0"
	case "linux":
		return "eth0"
	default:
		panic(fmt.Sprintf("unsupported OS: %v", os))
	}
}

func TestCheckDHCPServer(t *testing.T) {
	ifaceName := "eth0"
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		t.Fatalf("Interface %s not found: %v", ifaceName, err)
	}

	mac := iface.HardwareAddr
	if mac == nil {
		t.Fatalf("No MAC address found for interface %s", ifaceName)
	}

	res, err := checkDHCPServer(mac)
	assert.Equal(t, true, res, "checkDHCPServer() should return true", err)
}

//
// func Test_generateDnsmasqConfig(t *testing.T) {
// 	type args struct {
// 		fnGetIfaceAddr IfaceAddrGetterFunc
// 		namedMACs      []models.NamedMAC
// 	}
// 	tests := []struct {
// 		name    string
// 		args    args
// 		want    string
// 		wantErr bool
// 	}{
// 		{
// 			name: "test",
// 			args: args{fnGetIfaceAddr: getIfaceAddresses, namedMACs: []models.NamedMAC{{MAC: "", Name: "name"}}},
// 			want: `# dnsmasq configuration generated programmatically
// port=67
// interface=en0
// dhcp-range=192.168.1.5,192.168.1.250,12h
//
// dhcp-option=tag:customgw,option:router,192.0.0.2 # this gateway
// dhcp-host=,set:customgw # name`,
// 			wantErr: false,
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			defaultInterfaceName = getNetAdapterName()
// 			got, err := generateDnsmasqConfig(tt.args.fnGetIfaceAddr, tt.args.namedMACs)
// 			if (err != nil) != tt.wantErr {
// 				t.Errorf("generateDnsmasqConfig() error = %v, wantErr %v", err, tt.wantErr)
// 				return
// 			}
// 			if got != tt.want {
// 				t.Errorf("generateDnsmasqConfig() got = %v, want %v", got, tt.want)
// 			}
// 		})
// 	}
// }
//
// func Test_getSubnetBounds(t *testing.T) {
// 	type args struct {
// 		interfaceName string
// 	}
// 	tests := []struct {
// 		name    string
// 		args    args
// 		want    net.IP
// 		want1   net.IP
// 		wantErr bool
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			got, got1, err := getSubnetBounds(tt.args.interfaceName)
// 			if (err != nil) != tt.wantErr {
// 				t.Errorf("getSubnetBounds() error = %v, wantErr %v", err, tt.wantErr)
// 				return
// 			}
// 			if !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("getSubnetBounds() got = %v, want %v", got, tt.want)
// 			}
// 			if !reflect.DeepEqual(got1, tt.want1) {
// 				t.Errorf("getSubnetBounds() got1 = %v, want %v", got1, tt.want1)
// 			}
// 		})
// 	}
// }
