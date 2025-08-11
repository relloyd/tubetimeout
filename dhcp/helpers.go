package dhcp

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func defaultRouteCmd() (string, error) {
	output, err := exec.Command("netstat", routeCmdArgs...).Output() // -n: show numerical addresses, -a: show all hosts
	return string(output), err
}

var preferredIfaces = []string{
	"eth0", // RaspberryPi Zero 2w
	"end0", // OrangePiZero3
}

func getPrimaryInterfaceName() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, name := range preferredIfaces {
		for _, iface := range ifaces {
			if iface.Name == name {
				return iface.Name, nil
			}
		}
	}

	return "", errors.New("no preferred interface found")
}

// GetHardwareAddress returns the hardware (MAC) address of the given network interface.
// If the interface cannot be found or does not have a hardware address, it returns an error.
func getIfaceHardwareAddress(ifaceName string) (net.HardwareAddr, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, err
	}
	if len(iface.HardwareAddr) == 0 {
		return nil, errors.New("interface does not have a hardware address")
	}
	return iface.HardwareAddr, nil
}

// getDefaultGateway reads /proc/net/route to obtain the default gateway for interface eth0.
func getDefaultGateway() (net.IP, error) {
	// Use "netstat -rn" for both macOS and Linux.
	// Assume routeCmd now takes the command name and its arguments.
	output, err := routeCmd()
	if err != nil {
		return nil, fmt.Errorf("failed to execute netstat command: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		// Skip header lines and empty lines.
		if strings.HasPrefix(line, "Destination") || strings.HasPrefix(line, "Kernel") || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// On macOS, the default route is marked as "default"
		// On Linux, it is usually marked as "0.0.0.0"
		if (runtime.GOOS == "darwin" && fields[0] == "default") ||
			(runtime.GOOS != "darwin" && fields[0] == "0.0.0.0") {
			ip := net.ParseIP(fields[1])
			if ip == nil {
				return nil, fmt.Errorf("failed to parse gateway IP: %s", fields[1])
			}
			return ip, nil
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("default gateway not found")
}

// getSubnetBoundsForInterface queries the interface by name,
// finds the first IPv4 address, calculates the network and broadcast addresses,
// and returns the first usable IP (network + 1) and the last usable IP (broadcast - 1).
func getSubnetBoundsForInterface(ifaceName string) (net.IP, net.IP, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get addresses for interface %s: %w", ifaceName, err)
	}

	var ipnet *net.IPNet
	for _, addr := range addrs {
		if tmp, ok := addr.(*net.IPNet); ok && tmp.IP.To4() != nil {
			ipnet = tmp
			break
		}
	}
	if ipnet == nil {
		return nil, nil, errors.New("no IPv4 address found on interface")
	}

	// Compute the network IP by applying the mask.
	networkIP := ipnet.IP.Mask(ipnet.Mask)

	// Compute the broadcast address:
	// broadcast = network IP OR (inverse of subnet mask)
	broadcast := make(net.IP, len(networkIP))
	for i := 0; i < len(networkIP); i++ {
		broadcast[i] = networkIP[i] | ^ipnet.Mask[i]
	}

	// Convert to uint32 for simple arithmetic.
	networkUint := ipToUint32(networkIP)
	broadcastUint := ipToUint32(broadcast)

	if broadcastUint <= networkUint+1 {
		return nil, nil, errors.New("invalid subnet range, no usable addresses")
	}

	lowerIP := uint32ToIP(networkUint + 1)
	upperIP := uint32ToIP(broadcastUint - 1)

	return lowerIP, upperIP, nil
}

// adjustSubnetRange takes a lower IP, an upper IP and the default gateway IP,
// and returns a new subnet range that excludes the gateway IP, plus a sensible
// local IP (at the end of the range) that does not clash with the default gateway.
func adjustSubnetRange(lowerIP, upperIP, gateway net.IP) (net.IP, net.IP, net.IP, error) {
	// Convert IP addresses to uint32.
	lw := ipToUint32(lowerIP)
	up := ipToUint32(upperIP)
	gw := ipToUint32(gateway)

	// Ensure that the range is valid.
	if lw >= up {
		return nil, nil, nil, fmt.Errorf("invalid range: lower IP must be less than upper IP")
	}

	// If the gateway is not within the range, no adjustments are necessary.
	if gw < lw || gw > up {
		return lowerIP, upperIP, upperIP, nil
	}

	var newLower, newUpper, chosenIP uint32

	// Handle cases when the gateway is at the very beginning or end.
	if gw == lw {
		if lw+1 > up {
			return nil, nil, nil, fmt.Errorf("no usable addresses available after excluding the gateway")
		}
		newLower = lw + 1
		newUpper = up
		chosenIP = newUpper
	} else if gw == up {
		if up-1 < lw {
			return nil, nil, nil, fmt.Errorf("no usable addresses available after excluding the gateway")
		}
		newLower = lw
		newUpper = up - 1
		chosenIP = newUpper
	} else {
		// The gateway is somewhere in the middle.
		// Split the range into two segments:
		//  - Lower segment: [lw, gw-1]
		//  - Upper segment: [gw+1, up]
		segLowerSize := gw - lw // size of the lower segment
		segUpperSize := up - gw // size of the upper segment

		// Choose the segment with more addresses (or the upper segment if they are equal)
		if segUpperSize >= segLowerSize && segUpperSize > 0 {
			newLower = gw + 1
			newUpper = up
			chosenIP = newUpper
		} else if segLowerSize > 0 {
			newLower = lw
			newUpper = gw - 1
			chosenIP = newUpper
		} else {
			return nil, nil, nil, fmt.Errorf("no usable addresses available after excluding the gateway")
		}
	}

	return uint32ToIP(newLower), uint32ToIP(newUpper), uint32ToIP(chosenIP), nil
}

// ipToUint32 converts a net.IP (IPv4) to a uint32.
func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return binary.BigEndian.Uint32(ip)
}

// uint32ToIP converts a uint32 to a net.IP (IPv4).
func uint32ToIP(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}

// incrementIP returns a new IP incremented by one.
func incrementIP(ip net.IP) net.IP {
	ip = append(net.IP(nil), ip...)
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
	return ip
}

// compareIP returns -1 if a < b, 0 if a == b, and 1 if a > b.
func compareIP(a, b net.IP) int {
	for i := 0; i < len(a); i++ {
		if a[i] < b[i] {
			return -1
		} else if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// ipToBigInt converts an IP to a big.Int
func ipToBigInt(ip net.IP) *big.Int {
	ip = ip.To16() // Ensure IPv6 representation (even for IPv4)
	return new(big.Int).SetBytes(ip)
}

// bigIntToIP converts a big.Int to an IP address
func bigIntToIP(ipInt *big.Int) net.IP {
	b := ipInt.Bytes()
	if len(b) < 16 {
		// Pad to 16 bytes for IPv6
		padded := make([]byte, 16)
		copy(padded[16-len(b):], b)
		b = padded
	}
	return net.IP(b)
}

// ChooseIPFromBottom picks the lowest IP in the range and updates the range.
func chooseIPFromBottom(lower, upper net.IP) (chosenIP, newLower, newUpper net.IP, err error) {
	if len(lower) != len(upper) || (lower.To4() == nil) != (upper.To4() == nil) {
		return nil, nil, nil, errors.New("IP versions (IPv4/IPv6) do not match")
	}
	if compareIP(lower, upper) > 0 {
		return nil, nil, nil, errors.New("lower IP is greater than upper IP")
	}
	chosenIP = append(net.IP(nil), lower...)
	newLower = incrementIP(lower)
	if compareIP(newLower, upper) > 0 { // if the range is now empty...
		return chosenIP, nil, nil, fmt.Errorf("range exhausted: lower IP is greater than upper IP")
	}
	return chosenIP, newLower, upper, nil
}

// generateDnsmasqConfig builds the full dnsmasq configuration as a string.
func generateDnsmasqConfig(interfaceName string, thisGateway, subnetLower, subnetUpper net.IP, thisGatewayHardwareAddress string, dnsIPS []net.IP, reservations []Reservation) (string, error) {
	// Global configuration settings.
	if len(dnsIPS) != 2 {
		return "", fmt.Errorf("expected two DNS IPs: %v", dnsIPS)
	}

	var ipStrings []string
	for _, ip := range dnsIPS {
		ipStrings = append(ipStrings, ip.String())
	}

	lines := []string{
		"# dnsmasq configuration generated programmatically",
		fmt.Sprintf("interface=%v", interfaceName),
		fmt.Sprintf("dhcp-range=%v,%v,%v", subnetLower, subnetUpper, defaultLeaseDuration),
		fmt.Sprintf("dhcp-option=option:router,%v", thisGateway),
		fmt.Sprintf("dhcp-option=option:dns-server,%v", strings.Join(ipStrings, ",")),
		"no-resolv", // no-resolv will use server entries below as the upstream DNS servers, instead of resolv.conf.
		fmt.Sprintf("server=%v", dnsIPS[0]),
		fmt.Sprintf("server=%v", dnsIPS[1]),
		"",
	}

	// # Static IP reservations take the form:
	// dhcp-host=dc:a6:32:68:47:ea,192.168.1.52
	// dhcp-host=dc:a6:32:68:47:e9,192.168.1.53
	// dhcp-host=2c:cf:67:b6:37:7e,192.168.1.54
	// dhcp-host=58:ef:68:e5:f5:8c,192.168.1.55

	// Reserve an IP for thisGateway.
	reservationsPattern := "dhcp-host=%v,%v # %v"
	lines = append(lines, "# static IP reservations")
	lines = append(lines, fmt.Sprintf(reservationsPattern, thisGatewayHardwareAddress, thisGateway, "this gateway"))
	for _, r := range reservations {
		lines = append(lines, fmt.Sprintf(reservationsPattern, r.MacAddr.WithColons(), r.IpAddr, r.Name))
	}

	// Custom exclusions to use the default gw:
	// TODO: consider given the real gateway to MACs not explicitly configured to use tubetimeout.
	// Configure a tag to use for custom host entries for each supplied known MAC; assign a tag and set a custom router.
	// lines = append(lines, fmt.Sprintf("dhcp-option=tag:customgw,option:router,%s # this gateway", thisGateway))
	// for _, v := range namedMACs {
	// 	name := v.Name
	// 	if name == "" {
	// 		name = "un-named"
	// 	}
	// 	lines = append(lines, fmt.Sprintf("dhcp-host=%s,set:customgw # %v", v.MAC, name))
	// }
	// lines = append(lines, "")

	return strings.Join(lines, "\n"), nil
}

// writeDnsmasqConfig writes the generated config to the given file path.
func writeDnsmasqConfig(configPath string, configContent string) error {
	return os.WriteFile(configPath, []byte(configContent), 0644)
}

type cidrFinderFunc func(startIP, endIP net.IP) (string, string)

// findSmallestSingleCIDR finds the smallest CIDR block that fully covers the given range.
// It returns the "<IP>/<CIDR>" and the "<CIDR>".
func findSmallestSingleCIDR(startIP, endIP net.IP) (string, string) {
	start := ipToBigInt(startIP)
	end := ipToBigInt(endIP)

	maxSize := 32 // Assume IPv4 (modify for IPv6 if needed)
	if startIP.To4() == nil {
		maxSize = 128
	}

	// Determine the smallest (i.e. minimal covering) CIDR block by checking from longest prefix
	block := ""
	for prefixLen := maxSize; prefixLen >= 0; prefixLen-- {
		mask := net.CIDRMask(prefixLen, maxSize)
		maskedIP := startIP.Mask(mask)
		maskedInt := ipToBigInt(maskedIP)
		blockSize := new(big.Int).Lsh(big.NewInt(1), uint(maxSize-prefixLen))
		upperBound := new(big.Int).Add(maskedInt, blockSize)
		upperBound.Sub(upperBound, big.NewInt(1))

		if maskedInt.Cmp(start) <= 0 && upperBound.Cmp(end) >= 0 {
			block = fmt.Sprintf("%d", prefixLen)
			return fmt.Sprintf("%v/%v", maskedIP.String(), block), block
		}
	}

	return "", ""
}
