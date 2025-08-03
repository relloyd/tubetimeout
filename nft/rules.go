package nft

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

func init() {
	if os.Geteuid() != 0 {
		config.MustGetLogger().Fatalf("You must be root to run this program.")
	}
}

var (
	defaultTableName = "tubetimeout-table"
)

const (
	defaultFilterChainName = "filter"
	defaultNATChainName    = "post-routing"
	defaultSrcIpSetName    = "local_ip_set"
	defaultDestIpSetName   = "remote_ip_set"
	defaultProtocolSetName = "protocol_set"
	defaultQueueNumDest    = uint16(100) // defaultQueueNumDest only used by unused code 🤣
)

type Rules struct {
	logger        *zap.SugaredLogger
	conn          *nftables.Conn
	tableName     string
	chainName     string
	table         *nftables.Table
	chain         *nftables.Chain
	nameSetLocal  string
	nameSetRemote string
	setLocal      *nftables.Set
	setRemote     *nftables.Set
	setProto      *nftables.Set
	remoteIPs     []nftables.SetElement
	localIPs      []nftables.SetElement
	mu            sync.Mutex
}

func NewNFTRules(logger *zap.SugaredLogger, cfg *config.FilterConfig) (*Rules, error) {
	var err error
	rules := &Rules{
		logger:        logger,
		conn:          &nftables.Conn{},
		tableName:     defaultTableName,
		chainName:     defaultFilterChainName,
		nameSetLocal:  defaultSrcIpSetName,
		nameSetRemote: defaultDestIpSetName,
		localIPs:      make([]nftables.SetElement, 0),
		remoteIPs:     make([]nftables.SetElement, 0),
	}

	rules.table, err = getOrCreateTable(rules.logger, rules.conn, rules.tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to create nftables table: %v", err)
	}

	rules.chain, err = getOrCreateFilterChain(rules.logger, rules.conn, rules.table, rules.chainName)
	if err != nil {
		return nil, fmt.Errorf("failed to create nftables chain: %v", err)
	}

	nat, err := getOrCreateNATPostRoutingChain(rules.logger, rules.conn, rules.table, defaultNATChainName)
	if err != nil {
		return nil, fmt.Errorf("failed to create nftables NAT chain: %v", err)
	}

	// // Get the interface index for "wlan0"
	// oif, err := net.InterfaceByName("wlan0") // TODO: make masquerading interface configurable
	// if err != nil {
	// 	panic(err)
	// }

	// Add NAT in post routing chain, to rewrite source IP address. This should be masquerading.
	rules.conn.AddRule(&nftables.Rule{
		Table: rules.table,
		Chain: nat,
		Exprs: []expr.Any{
			// &expr.Meta{
			// 	Key:      expr.MetaKeyOIF, // Match interface name
			// 	Register: 2,
			// },
			// &expr.Cmp{
			// 	Op:       expr.CmpOpEq,
			// 	Register: 2,
			// 	Data:     []byte{byte(oif.Index), 0, 0, 0}, // Match index   // []byte("wlan0\x00"), // "wlan0" null-terminated
			// },
			// TODO: figure out how to mark packets by using tracing!
			// &expr.Meta{
			// 	Key:            expr.MetaKeyMARK,
			// 	Register:       1,
			// 	SourceRegister: true,
			// },
			// &expr.Cmp{
			// 	Op:       expr.CmpOpEq,
			// 	Register: 1,
			// 	Data:     []byte{1, 0, 0, 0}, // match the mark 1
			// },
			&expr.Masq{},
		},
	})

	// Create TCP/UDP set.
	rules.setProto = &nftables.Set{
		Name:    defaultProtocolSetName,
		Table:   rules.table,
		KeyType: nftables.TypeInetProto,
	}
	err = rules.conn.AddSet(rules.setProto, []nftables.SetElement{
		{Key: []byte{6}},  // TCP
		{Key: []byte{17}}, // UDP
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create protocol set")
	}

	// Create local IP address set.
	rules.setLocal = &nftables.Set{
		Name:    rules.nameSetLocal,
		Table:   rules.table,
		KeyType: nftables.TypeIPAddr,
		Dynamic: true,
	}
	err = rules.conn.AddSet(rules.setLocal, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create local IP set")
	}

	// Create remote IP address set.
	rules.setRemote = &nftables.Set{
		Name:    rules.nameSetRemote,
		Table:   rules.table,
		KeyType: nftables.TypeIPAddr,
		Dynamic: true,
	}
	err = rules.conn.AddSet(rules.setRemote, nil) // start with empty sets so we can update them later
	if err != nil {
		return nil, fmt.Errorf("failed to create remote IP set")
	}

	rules.dropUDPFromToLocalIPs(cfg.OutboundQueueNumber, cfg.InboundQueueNumber) // drop UDP to/from the local IP set.

	// Create NFTables rules for src-dest and dest-src combinations.
	err = rules.addNFTablesRuleForSets(cfg.OutboundQueueNumber, rules.nameSetLocal, rules.nameSetRemote)
	if err != nil {
		return nil, fmt.Errorf("failed to create NFT rule for src-dest combination")
	}
	err = rules.addNFTablesRuleForSets(cfg.InboundQueueNumber, rules.nameSetRemote, rules.nameSetLocal)
	if err != nil {
		return nil, fmt.Errorf("failed to create NFT rule for dest-src combination")
	}

	// Flush changes to the kernel.
	if err = rules.conn.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush nftables rules: %v", err)
	}

	return rules, nil
}

// dropUDPPorts creates a rule to drop UDP packets for IPs in the source/local IP set.
// TODO: test that UDP packets are dropped from local IPs.
func (q *Rules) dropUDPPorts() {
	// Define a set for UDP ports to match
	udpPortSet := &nftables.Set{
		Table:   q.table,
		Name:    "udp_ports",
		KeyType: nftables.TypeInetService, // Port number type
	}
	err := q.conn.AddSet(udpPortSet, nil)
	if err != nil {
		q.logger.Fatalf("Failed to create set of UDP ports: %v", err)
	}

	// Add elements to the set (UDP ports to block)
	elements := []nftables.SetElement{
		{Key: []byte{0x01, 0xf4}}, // Port 500 NAT-T
		{Key: []byte{0x11, 0x94}}, // Port 4500 NAT-T
		{Key: []byte{0x01, 0xBB}}, // Port 443
	}
	if err := q.conn.SetAddElements(udpPortSet, elements); err != nil {
		log.Fatalf("Failed to add elements to set: %v", err)
	}

	// Create a rule to match UDP packets with destination ports in the set
	q.conn.AddRule(&nftables.Rule{
		Table: q.table,
		Chain: q.chain,
		Exprs: []expr.Any{
			// Match UDP protocol
			&expr.Meta{
				Key:      expr.MetaKeyL4PROTO,
				Register: 1,
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     []byte{unix.IPPROTO_UDP},
			},
			// Match destination port in set
			&expr.Payload{
				DestRegister: 2,
				Base:         expr.PayloadBaseTransportHeader,
				Offset:       2, // UDP header destination port offset
				Len:          2, // Length of port
			},
			&expr.Lookup{
				SourceRegister: 2,
				SetName:        udpPortSet.Name,
			},
			// Drop the packet
			&expr.Verdict{
				Kind: expr.VerdictDrop,
			},
		},
	})
}

func (q *Rules) dropUDPFromToLocalIPs(outboundQueueNumber uint16, inboundQueueNumber uint16) {
	data := []struct {
		direction   uint32
		queueNumber uint16
	}{
		{12, inboundQueueNumber},  // 12 for source IP
		{16, outboundQueueNumber}, // 16 for destination IP
	}

	// Define a set for UDP ports to match
	udpPortSet := &nftables.Set{
		Table:   q.table,
		Name:    "udp_ports",
		KeyType: nftables.TypeInetService, // Port number type
	}
	err := q.conn.AddSet(udpPortSet, nil)
	if err != nil {
		q.logger.Fatalf("Failed to create set of UDP ports: %v", err)
	}

	// Add elements to the set (UDP ports to block)
	elements := []nftables.SetElement{
		{Key: []byte{0x01, 0xf4}}, // Port 500 NAT-T
		{Key: []byte{0x11, 0x94}}, // Port 4500 NAT-T
		{Key: []byte{0x01, 0xBB}}, // Port 443
	}
	if err := q.conn.SetAddElements(udpPortSet, elements); err != nil {
		log.Fatalf("Failed to add elements to set: %v", err)
	}

	// Drop UDP
	for _, direction := range data {
		rule := &nftables.Rule{
			Table: q.table,
			Chain: q.chain,
			Exprs: []expr.Any{
				// Match source IP in the set
				&expr.Payload{
					DestRegister: 1,                             // Extract the source IP address into register 1
					Base:         expr.PayloadBaseNetworkHeader, // Network header
					Offset:       direction.direction,           // Offset based on direction
					Len:          4,                             // Length: 4 bytes for IPv4 address
				},
				// Check if the source IP is in the YouTube IP set
				&expr.Lookup{
					SourceRegister: 1,
					SetName:        q.nameSetLocal, // drop UDP to/from the local IPs.
				},

				// Match UDP protocol
				&expr.Payload{
					DestRegister: 2,                             // Store in register 2
					Base:         expr.PayloadBaseNetworkHeader, // Network header
					Offset:       9,                             // Protocol field offset in IPv4 header
					Len:          1,                             // Length: 1 byte
				},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 2,
					Data:     []byte{unix.IPPROTO_UDP}, // 17 = UDP
				},

				// Match destination port in set
				&expr.Payload{
					DestRegister: 3,
					Base:         expr.PayloadBaseTransportHeader,
					Offset:       2, // UDP header destination port offset
					Len:          2, // Length of port
				},
				&expr.Lookup{
					SourceRegister: 3,
					SetName:        udpPortSet.Name,
				},

				// Match UDP destination port range (0-65535)
				// &expr.Payload{
				// 	DestRegister: 3,
				// 	Base:         expr.PayloadBaseTransportHeader,
				// 	Offset:       2, // UDP header - Destination Port (2 bytes at offset 2)
				// 	Len:          2, // Match 2 bytes (16-bit port)
				// },
				// &expr.Range{
				// 	Register: 3,
				// 	FromData: []byte{0x00, 0x00}, // Port 0
				// 	ToData:   []byte{0xFF, 0xFF}, // Port 65535
				// },

				// Drop action
				// &expr.Verdict{
				// 	Kind: expr.VerdictDrop,
				// },

				&expr.Queue{
					Num:   direction.queueNumber,
					Total: 1,
					Flag:  0, // 0 = block; use expr.QueueFlagBypass (1) to bypass if the net filter is not running or if the queue is full
				},
			},
		}
		q.conn.AddRule(rule)
	}
}

// UpdateDestIpDomains is a callback that saves the supplied Ip addresses and updates the nft rules using them.
func (q *Rules) UpdateDestIpDomains(newData models.MapIpDomain) {
	q.logger.Debugf("NFT callback with new destination IPs: %v", newData)

	// Convert to set elements and save.
	discarded := 0
	var newIps []nftables.SetElement
	for k := range newData {
		ip := net.ParseIP(string(k)).To4()
		if ip != nil {
			newIps = append(newIps, nftables.SetElement{Key: ip})
		} else {
			discarded++
		}
	}

	if discarded > 0 {
		q.logger.Infof("NFT destination IP callback discarded %v address(es)", discarded)
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	q.remoteIPs = newIps

	// Refresh the NFTables rules.
	err := q.updateIpSets()
	if err != nil {
		q.logger.Warnf("NFT callback with new destination IPs couldn't make the update: %v", err)
	}
}

// UpdateSourceIpGroups is a callback that saves the supplied Ip addresses and updates the nft rules using them.
func (q *Rules) UpdateSourceIpGroups(newData models.MapIpGroups) {
	q.logger.Debugf("NFT callback with new source IPs: %v", newData)

	// Convert to set elements and save.
	discarded := 0
	var newIps []nftables.SetElement
	for k := range newData {
		ip := net.ParseIP(string(k)).To4()
		if ip != nil {
			newIps = append(newIps, nftables.SetElement{Key: ip})
		} else {
			discarded++
		}
	}

	if discarded > 0 {
		q.logger.Infof("NFT destination IP callback discarded %v address(es)", discarded)
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	q.localIPs = newIps

	err := q.updateIpSets()
	if err != nil {
		q.logger.Warnf("NFT callback with new source IPs couldn't make the update: %v", err)
	}
}

// updateIpSets adds nftables rules to send packets to the default NFQs.
// This should be done under a mutex since it reads the Rules srcIps and destIps.
func (q *Rules) updateIpSets() error {
	if len(q.localIPs) == 0 {
		return fmt.Errorf("local IPs aren't ready")
	}
	if len(q.remoteIPs) == 0 {
		return fmt.Errorf("remote IPs aren't ready")
	}

	// Clear all existing local IP in the set.
	existingSetLocalIps, err := q.conn.GetSetElements(q.setLocal)
	if err != nil {
		return fmt.Errorf("unable to get existing local IPs from set: %w", err)
	}
	err = q.conn.SetDeleteElements(q.setLocal, existingSetLocalIps)
	if err != nil {
		return fmt.Errorf("unable to delete local set contents: %w", err)
	}

	// Clear all existing remote IPs in the set.
	existingSetRemoteIps, err := q.conn.GetSetElements(q.setRemote)
	if err != nil {
		return fmt.Errorf("unable to get existing local IPs from set: %w", err)
	}
	err = q.conn.SetDeleteElements(q.setRemote, existingSetRemoteIps)
	if err != nil {
		return fmt.Errorf("unable to delete remote set contents: %w", err)
	}

	// Add local IPs to set.
	err = q.conn.SetAddElements(q.setLocal, q.localIPs)
	if err != nil {
		return fmt.Errorf("unable to add new local IPs to set: %w", err)
	}

	// Add remote IPs to set.
	err = q.conn.SetAddElements(q.setRemote, q.remoteIPs)
	if err != nil {
		return fmt.Errorf("unable to add new remote IPs to set: %w", err)
	}

	// Flush changes to the kernel.
	if err := q.conn.Flush(); err != nil {
		return fmt.Errorf("failed to flush nftables sets: %v", err)
	}

	q.logger.Infof("NFT rules updated with %d local IPs and %d remote IPs", len(q.localIPs), len(q.remoteIPs))
	return nil
}

// addNFTablesRuleSet creates NFTables rules by creating a rule that sends traffic to the given NFQueue number.
// It uses a set for each of the source and dest IP slices supplied.
// The caller should flush the changes to the kernel after.
// # iptables -I OUTPUT -p icmp -j NFQUEUE --queue-num 100
func (q *Rules) addNFTablesRuleForSets(nfqNumber uint16, srcSetName, destSetName string) error {
	// Add a rule to match traffic from YouTube IPs to destination hosts
	rule := &nftables.Rule{
		Table: q.table,
		Chain: q.chain,
		Exprs: []expr.Any{
			// Extract the source IP address into register 1
			&expr.Payload{
				DestRegister: 1,                             // Store in register 1
				Base:         expr.PayloadBaseNetworkHeader, // Network header
				Offset:       12,                            // Offset 12 for IPv4 source IP
				Len:          4,                             // Length: 4 bytes for IPv4 address
			},
			// Check if the source IP is in the YouTube IP set
			&expr.Lookup{
				SourceRegister: 1,
				SetName:        srcSetName,
			},
			// Extract the destination IP address into register 2
			&expr.Payload{
				DestRegister: 2,                             // Store in register 2
				Base:         expr.PayloadBaseNetworkHeader, // Network header
				Offset:       16,                            // Offset 16 for IPv4 destination IP
				Len:          4,                             // Length: 4 bytes for IPv4 address
			},
			// Check if the destination IP is in the destination hosts set
			&expr.Lookup{
				SourceRegister: 2,
				SetName:        destSetName,
			},
			// Extract the protocol into register 3
			&expr.Payload{
				DestRegister: 3,                             // Store in register 3
				Base:         expr.PayloadBaseNetworkHeader, // Network header
				Offset:       9,                             // Protocol field offset in IPv4 header
				Len:          1,                             // Length: 1 byte
			},
			// Check if the protocol is in the protocol set
			&expr.Lookup{
				SourceRegister: 3,
				SetName:        q.setProto.Name,
			},
			// TODO: figure out how to mark packets by using tracing!
			// // Add a mark to the packet.
			// &expr.Meta{
			// 	Key:            expr.MetaKeyMARK,
			// 	Register:       4,
			// },
			// &expr.Immediate{
			// 	Register: 4,
			// 	Data:     []byte{1, 0, 0, 0}, // Set a mark; see also the reading of this mark in the NAT chain.
			// },
			// Send matching packets to NFQUEUE for further processing
			&expr.Queue{
				Num:   nfqNumber,
				Total: 1,
				Flag:  0, // 0 = block; use expr.QueueFlagBypass (1) to bypass if the net filter is not running or if the queue is full
			},
		},
	}
	q.conn.AddRule(rule)
	return nil
}

// addNFTablesRuleForSingleDestAddr adds a rule to send traffic to the NFQUEUE for this app.
// This accepts one destination IP address and creates a rule for it in the table/chain found in the bound struct.
// The caller should flush the changes to the kernel after.
// It uses the default queue number.
// # iptables -I OUTPUT -p icmp -j NFQUEUE --queue-num 100
func (q *Rules) addNFTablesRuleForSingleDestAddr(dAddr models.Ip) error {
	ip := net.ParseIP(string(dAddr))
	if ip == nil {
		return errors.New("invalid net IP address")
	}

	var offset, length uint32
	var ipBytes []byte

	if ip.To4() != nil {
		offset = 16
		length = 4
		ipBytes = ip.To4()
	} else {
		if ip.To16() != nil {
			q.logger.Infof("Skipped IP6 address %q\n", dAddr)
		} else {
			q.logger.Infof("Skipped bad IP address %q\n", dAddr)
		}
		return nil
	}

	// Add a rule to send traffic to NFQUEUE
	rule := &nftables.Rule{
		Table: q.table,
		Chain: q.chain,
		Exprs: []expr.Any{
			// Match destination Ip address
			&expr.Payload{
				DestRegister: 1,                             // Store the payload in register 1
				Base:         expr.PayloadBaseNetworkHeader, // Match the network header
				Offset:       offset,
				Len:          length,
				// TODO: handle IPv6 in nftables rules
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     ipBytes,
			},
			// // Send matched packets to NFQUEUE
			&expr.Queue{
				Num:   defaultQueueNumDest, // NFQUEUE number
				Total: 1,                   // Single queue
				Flag:  0,                   // 0 = block; use expr.QueueFlagBypass (1) to bypass if the net filter is not running or if the queue is full
			},
		},
	}
	q.conn.AddRule(rule)
	return nil
}

func tableExists(logger *zap.SugaredLogger, conn *nftables.Conn, tableName string) bool {
	tables, err := conn.ListTables()
	if err != nil {
		logger.Fatalf("Failed to list nftables tables: %v\n", err)
	}
	for _, v := range tables {
		if v.Name == tableName {
			return true
		}
	}
	return false
}

func chainExists(logger *zap.SugaredLogger, conn *nftables.Conn, chainName string) bool {
	chains, err := conn.ListChains()
	if err != nil {
		logger.Fatalf("Failed to list nftables chains: %v\n", err)
	}
	for _, v := range chains {
		if v.Name == chainName {
			return true
		}
	}
	return false
}

func getOrCreateTable(logger *zap.SugaredLogger, conn *nftables.Conn, tableName string) (*nftables.Table, error) {
	var err error
	table := &nftables.Table{
		Family: nftables.TableFamilyIPv4, // TODO: work out if we can use family inet instead for both ip4 and ip16 addresses
		// NOTE: Family: nftables.TableFamilyINet doesn't work for both IPv4 and IPv6 addresses (it produces lower-level rules)
		Name: tableName,
	}
	if !tableExists(logger, conn, tableName) { // TODO: decide if we want to delete/replace the table if it exists already
		conn.AddTable(table)
		err = conn.Flush()
	}
	return table, err
}

func getOrCreateFilterChain(logger *zap.SugaredLogger, conn *nftables.Conn, table *nftables.Table, chainName string) (*nftables.Chain, error) {
	var err error
	chain := &nftables.Chain{
		Name:     chainName,
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookForward, // input chain is for packets destined for the local machine; forward chain is for packets that are being routed through the local machine; output chain is for packets originating from the local machine
		Priority: nftables.ChainPriorityFilter,
	}
	if !chainExists(logger, conn, table.Name) { // TODO: decide if we want to delete/replace the chain if it exists already
		conn.AddChain(chain)
		err = conn.Flush()
	}
	return chain, err
}

// func getOrCreatePreRoutingChain(conn *nftables.Conn, table *nftables.Table, chainName string) (*nftables.Chain, error) {
// 	var err error
// 	chain := &nftables.Chain{
// 		Name:     chainName,
// 		Table:    table,
// 		Type:     nftables.ChainTypeFilter,
// 		Hooknum:  nftables.ChainHookPrerouting,
// 		Priority: nftables.ChainPriorityRaw,
// 	}
// 	if !chainExists(conn, chainName) {
// 		conn.AddChain(chain)
// 		err = conn.Flush()
// 	}
// 	return chain, err
// }

func getOrCreateNATPostRoutingChain(logger *zap.SugaredLogger, conn *nftables.Conn, table *nftables.Table, chainName string) (*nftables.Chain, error) {
	var err error
	chain := &nftables.Chain{
		Name:     chainName,
		Table:    table,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPostrouting,
		Priority: nftables.ChainPriorityNATSource,
	}
	if !chainExists(logger, conn, chainName) {
		conn.AddChain(chain)
		err = conn.Flush()
	}
	return chain, err
}

func deleteTable(logger *zap.SugaredLogger, conn *nftables.Conn, tableName string) error {
	// Delete the table and all its chains and rules.
	conn.DelTable(&nftables.Table{Name: tableName})
	err := conn.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush nft: %v", err)
	}
	if tableExists(logger, conn, tableName) {
		return fmt.Errorf("nft table %q not deleted", defaultTableName)
	}
	logger.Infof("NFT table %q deleted", tableName)
	return nil
}

// Clean deletes the nftables table and therefore all its chains and rules.
func (q *Rules) Clean(logger *zap.SugaredLogger) error {
	return deleteTable(logger, q.conn, q.table.Name)
}

// // getDiffAMinusB returns all elements in a that are not in b.
// func getDiffAMinusB[T comparable](a, b []T) []T {
// 	var diff []T
// 	for _, item := range a {
// 		// Check if item is not in b
// 		if !slices.Contains(b, item) {
// 			diff = append(diff, item)
// 		}
// 	}
// 	return diff
// }
//
// func getSetIps(c *nftables.Conn, set *nftables.Set) ([]models.Ip, error) {
// 	elems, err := c.GetSetElements(set)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get set elements: %+v", set)
// 	}
// 	var ips []models.Ip
// 	for _, v := range elems {
// 		ips = append(ips, models.Ip(net.IP(v.Key).String()))
// 	}
// }
