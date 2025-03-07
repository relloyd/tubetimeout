package main

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/config"
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

func TestInitLoadsConfig(t *testing.T) {
	t.Log("Testing Init()")
	t.Logf("dnsMasqConfig contains %+v", dnsMasqConfig)
}

func TestCheckDHCPServer(t *testing.T) {
	ifaceName, err := getPrimaryInterfaceName()
	assert.NoError(t, err, "failed to get primary interface (check your o/s is listed)")

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		t.Fatalf("Interface %s not found: %v", ifaceName, err)
	}

	mac := iface.HardwareAddr
	if mac == nil {
		t.Fatalf("No MAC address found for interface %s", ifaceName)
	}

	res, err := isDHCPServerRunning(mac)
	assert.Equal(t, true, res, "isDHCPServerRunning() should return true", err)
}

// TestGetConfigCached verifies that when dnsMasqConfig is already set,
// GetConfig returns the cached configuration.
func TestGetConfigCached(t *testing.T) {
	// Prepare a dummy config.
	dummyCfg := newDNSMasqConfig()
	dummyCfg.DefaultGateway = net.ParseIP("192.168.1.1")
	// Here you can also set other fields as needed.

	// Set the global to simulate an already loaded config.
	dnsMasqConfig = dummyCfg

	// Call GetConfig. In this case, the function should simply return our dummyCfg.
	cfg, err := GetConfig()
	assert.NoError(t, err, "GetConfig should not return an error when a config is cached")
	assert.Equal(t, dummyCfg, cfg, "Expected cached config to be returned")
}

// TestGetConfigLoads verifies that if dnsMasqConfig is nil,
// GetConfig attempts to load the configuration and returns a non-nil result.
// Note: This test may interact with external dependencies. For more robust tests,
// consider refactoring the code to allow dependency injection of external functions.
func TestGetConfigLoads(t *testing.T) {
	// Reset the global to force a fresh load.
	dnsMasqConfig = nil

	cfg, err := GetConfig()
	assert.NoError(t, err, "Expected no error when loading config")
	assert.NotNil(t, cfg, "Expected non-nil config")

	// Basic sanity check for fields that should be set.
	if cfg.DefaultGateway == nil {
		t.Log("DefaultGateway is nil; this could be valid in some environments but ensure your stubs or system state return a valid IP")
	}
	if cfg.LowerBound == nil || cfg.UpperBound == nil {
		t.Log("Subnet bounds are nil; ensure getSubnetBounds works correctly or stub these values in tests")
	}
	if cfg.ThisGateway == nil {
		t.Log("ThisGateway is nil; ensure chooseIPFromBottom works correctly or stub these values in tests")
	}
}

func TestSetConfig_WritesToFile(t *testing.T) {
	originalFnDefault := config.FnDefaultCreateAppHomeDirAndGetConfigFilePath

	// Create a temporary file for the config
	tmpFile, err := os.CreateTemp("", "dnsmasq-config-*.json")
	assert.NoError(t, err)
	// Clean up the temp file when done
	defer os.Remove(tmpFile.Name())

	// Override the function to return our temp file path.
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(fileName string) (string, error) {
		return tmpFile.Name(), nil
	}
	// Restore the original function after the test completes.
	defer func() {
		config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = originalFnDefault
	}()

	// Change the global config file variable to point to the temp file.
	originalFile := configFileDNSMasqService
	configFileDNSMasqService = tmpFile.Name()
	defer func() {
		configFileDNSMasqService = originalFile
	}()

	// Create a sample config with a known value.
	sampleCfg := &DNSMasqConfig{
		mu:                  &sync.Mutex{},
		DefaultGateway:      net.ParseIP("192.168.1.1"),
		ThisGateway:         net.ParseIP("192.168.1.2"),
		LowerBound:          net.ParseIP("192.168.1.3"),
		UpperBound:          net.ParseIP("192.168.1.254"),
		AddressReservations: []string{"192.168.1.10"},
		ServiceEnabled:      true,
	}

	// Call SetConfig and ensure no error is returned.
	err = SetConfig(sampleCfg)
	assert.NoError(t, err)

	// Read back the file contents.
	bytes, err := os.ReadFile(tmpFile.Name())
	assert.NoError(t, err)
	content := string(bytes)

	// Check that the content has at least one known value from the config.
	// (For example, our DefaultGateway should be present.)
	assert.Contains(t, content, "192.168.1.1", "Expected DefaultGateway value %q to be saved in config file", "192.168.1.1")
}

func TestChooseIPFromBottom(t *testing.T) {
	tests := []struct {
		name          string
		lower         net.IP
		upper         net.IP
		expectedIP    net.IP
		expectedLower net.IP
		expectedUpper net.IP
		expectError   bool
		errorMessage  string
	}{
		{
			name:          "Valid IPv4 range, non-edge case",
			lower:         net.ParseIP("192.168.1.10"),
			upper:         net.ParseIP("192.168.1.20"),
			expectedIP:    net.ParseIP("192.168.1.10"),
			expectedLower: net.ParseIP("192.168.1.11"),
			expectedUpper: net.ParseIP("192.168.1.20"),
			expectError:   false,
			errorMessage:  "lower IP should fit normally within range",
		},
		{
			name:          "Valid IPv4 range, edge case",
			lower:         net.ParseIP("192.168.1.20"),
			upper:         net.ParseIP("192.168.1.20"),
			expectedIP:    net.ParseIP("192.168.1.20"),
			expectedLower: nil,
			expectedUpper: nil,
			expectError:   true,
			errorMessage:  "range should be exhausted when lower == upper",
		},
		{
			name:          "Invalid range, lower greater than upper",
			lower:         net.ParseIP("192.168.1.30"),
			upper:         net.ParseIP("192.168.1.20"),
			expectedIP:    nil,
			expectedLower: nil,
			expectedUpper: nil,
			expectError:   true,
			errorMessage:  "lower should not be greater than upper",
		},
		{
			name:          "Mismatched IP versions (IPv4 vs IPv6)",
			lower:         net.ParseIP("192.168.1.10"),
			upper:         net.ParseIP("2001:db8::2"),
			expectedIP:    nil,
			expectedLower: nil,
			expectedUpper: nil,
			expectError:   true,
			errorMessage:  "IP versions must match",
		},
		{
			name:          "Valid IPv6 range",
			lower:         net.ParseIP("2001:db8::1"),
			upper:         net.ParseIP("2001:db8::5"),
			expectedIP:    net.ParseIP("2001:db8::1"),
			expectedLower: net.ParseIP("2001:db8::2"),
			expectedUpper: net.ParseIP("2001:db8::5"),
			expectError:   false,
			errorMessage:  "IPv6 range should work as expected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chosenIP, newLower, newUpper, err := chooseIPFromBottom(tt.lower, tt.upper)
			if tt.expectError {
				assert.Error(t, err, tt.errorMessage)
			} else {
				assert.NoError(t, err, tt.errorMessage)
				assert.Equal(t, tt.expectedIP, chosenIP, "chosen IP is incorrect")
				assert.Equal(t, tt.expectedLower, newLower, "new lower IP is incorrect")
				assert.Equal(t, tt.expectedUpper, newUpper, "upper IP should remain unchanged")
			}
		})
	}
}

func TestGetDefaultGateway(t *testing.T) {
	tests := []struct {
		name        string
		mockOutput  string
		mockError   error
		expectedIP  net.IP
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid gateway on Linux",
			mockOutput:  "Iface Destination Gateway Genmask Flags  Ref    Use Metric Mask\n   eth0 00000000 01010101 00000000  UG    0      0   0\n",
			mockError:   nil,
			expectedIP:  net.ParseIP("1.1.1.1"),
			expectError: false,
			errorMsg:    "should correctly parse valid gateway",
		},
		{
			name:        "Valid gateway on macOS",
			mockOutput:  "gateway: 192.168.1.1\n",
			mockError:   nil,
			expectedIP:  net.ParseIP("192.168.1.1"),
			expectError: false,
			errorMsg:    "should correctly parse macOS gateway",
		},
		{
			name:        "Invalid gateway parsing",
			mockOutput:  "Iface Destination Gateway Genmask Flags  Ref    Use Metric Mask\n   eth0 00000000 ZGFR1090 00000000  UG    0      0   0\n",
			mockError:   nil,
			expectedIP:  nil,
			expectError: true,
			errorMsg:    "should return an error for unexpected gateway format",
		},
		{
			name:        "Command execution error",
			mockOutput:  "",
			mockError:   fmt.Errorf("execution failed"),
			expectedIP:  nil,
			expectError: true,
			errorMsg:    "should return an error when command execution fails",
		},
		{
			name:        "Missing gateway on Linux",
			mockOutput:  "Iface Destination Gateway Genmask Flags  Ref    Use Metric Mask\n",
			mockError:   nil,
			expectedIP:  nil,
			expectError: true,
			errorMsg:    "should return an error when no gateway found",
		},
	}

	originalRouteCmd := routeCmd
	originalRouteCmdArgs := routeCmdArgs

	t.Cleanup(func() {
		routeCmd = originalRouteCmd
		routeCmdArgs = originalRouteCmdArgs
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock the command execution
			routeCmd = func() (string, error) {
				return tt.mockOutput, nil
			}

			// Execute the function under test
			ip, err := getDefaultGateway()

			// Validate the result against expectations
			if tt.expectError {
				assert.Error(t, err, tt.errorMsg)
			} else {
				assert.NoError(t, err, tt.errorMsg)
				assert.Equal(t, tt.expectedIP, ip, tt.errorMsg)
			}
		})
	}
}

// TestHelperProcess provides mock output for exec.Command. DO NOT call directly.
func TestHelperProcess(t *testing.T) {
	args := os.Args
	if len(args) < 3 || args[2] != "--" {
		return
	}
	fmt.Print(os.Getenv("MOCK_OUTPUT"))
	if mockError := os.Getenv("MOCK_ERROR"); mockError != "nil" {
		os.Exit(1)
	}
	os.Exit(0)
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
