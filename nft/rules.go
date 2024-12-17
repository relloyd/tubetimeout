package nft

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"

	"example.com/youtube-nfqueue/models"
	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

func init() {
	if os.Geteuid() != 0 {
		log.Fatal("You must be root to run this program.")
	}
}

var (
	defaultTableName = "crazydeer-table"
	defaultChainName = "crazydeer-chain"
	defaultQueueNum  = uint16(100)
)

type NFTRules struct {
	tableName string
	chainName string
	conn      *nftables.Conn
	table     *nftables.Table
	chain     *nftables.Chain
	destIPs   models.IpSlice
	srcIPs    models.IpSlice
}

func NewNFTRules() (*NFTRules, error) {
	var err error
	rules := &NFTRules{
		conn:      &nftables.Conn{},
		tableName: defaultTableName,
		chainName: defaultChainName,
	}
	rules.table, err = getOrCreateTable(rules.conn, rules.tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to create nftables table: %v", err)
	}
	rules.chain, err = getOrCreateChain(rules.conn, rules.table, rules.chainName)
	if err != nil {
		return nil, fmt.Errorf("failed to create nftables chain: %v", err)
	}
	rules.destIPs.Data = make([]models.Ip, 0) // start with empty data
	rules.srcIPs.Data = make([]models.Ip, 0)
	return rules, nil
}

// UpdateDestIpDomains is a callback that saves the supplied Ip addresses and updates the nft rules using them.
func (q *NFTRules) UpdateDestIpDomains(newData models.MapIpDomain) {
	// Convert to slice and save.
	var newIps []models.Ip
	for k := range newData {
		newIps = append(newIps, k)
	}
	// Save the new Ip addresses.
	q.destIPs.Mu.Lock()
	defer q.destIPs.Mu.Unlock()
	q.destIPs.Data = newIps
	// Refresh the NFTables rules.
	err := q.sendIP4PacketsToDefaultNFQueue()
	if err != nil {
		log.Printf("failed to send Ip addresses to default NFQUEUE: %v\n", err)
	}
}

// UpdateSourceIpGroups is a callback that saves the supplied Ip addresses and updates the nft rules using them.
// TODO: do the nft rules need to be updated with the source Ip addresses?
func (q *NFTRules) UpdateSourceIpGroups(newData models.MapIpGroups) {
	// Convert to slice and save.
	var newIps []models.Ip
	for k := range newData {
		newIps = append(newIps, k)
	}
	// Save the new Ip addresses.
	q.destIPs.Mu.Lock()
	defer q.destIPs.Mu.Unlock()
	q.destIPs.Data = newIps
	// Refresh the NFTables rules.
	err := q.sendIP4PacketsToDefaultNFQueue() // TODO: make this do the right thing
	if err != nil {
		log.Printf("failed to send Ip addresses to default NFQUEUE: %v\n", err)
	}
}

// addNFTablesRule add a rule to send traffic to the NFQUEUE for this app.
// The caller should flush the changes to the kernel after.
// # iptables -I OUTPUT -p icmp -j NFQUEUE --queue-num 100
func (q *NFTRules) addNFTablesRule(dAddr models.Ip) error {
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
			log.Printf("Skipped IP6 address %q\n", dAddr)
		} else {
			log.Printf("Skipped bad IP address %q\n", dAddr)
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
				Num:   defaultQueueNum, // NFQUEUE number
				Total: 1,               // Single queue
				Flag:  0,               // 0 = block; use expr.QueueFlagBypass (1) to bypass if the net filter is not running or if the queue is full
			},
		},
	}
	q.conn.AddRule(rule)
	return nil
}

// addNFTablesRuleSet add a rule to send traffic to the NFQUEUE for this app.
// The caller should flush the changes to the kernel after.
// # iptables -I OUTPUT -p icmp -j NFQUEUE --queue-num 100
func (q *NFTRules) addNFTablesRuleSet() error {
	// Create a set for YouTube IPs
	remoteSet := &nftables.Set{
		Name:    "public_ips",
		Table:   q.table,
		KeyType: nftables.TypeIPAddr,
	}
	err := q.conn.AddSet(remoteSet, []nftables.SetElement{
		{Key: net.ParseIP("203.0.113.1")},
		{Key: net.ParseIP("203.0.113.2")},
	})
	if err != nil {
		return fmt.Errorf("failed to add remote IP set: %v", err)
	}

	// Create a set for destination hosts
	destSet := &nftables.Set{
		Name:    "network_ips",
		Table:   q.table,
		KeyType: nftables.TypeIPAddr,
	}
	err = q.conn.AddSet(destSet, []nftables.SetElement{
		{Key: net.ParseIP("192.168.1.10")}, // Replace with your network IPs
		{Key: net.ParseIP("192.168.1.11")},
	})
	if err != nil {
		return fmt.Errorf("failed to add destination host set: %v", err)
	}

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
				SetName:        "youtube_ips",
				Table:          table.Name,
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
				SetName:        "dest_hosts",
				Table:          table.Name,
			},
			// Send matching packets to NFQUEUE for further processing
			&expr.Queue{
				Num:   0, // NFQUEUE number
				Total: 1,
			},
		},
	}
	c.AddRule(rule)

	var offset, length uint32
	var ipBytes []byte

	if ip.To4() != nil {
		offset = 16
		length = 4
		ipBytes = ip.To4()
	} else {
		if ip.To16() != nil {
			log.Printf("Skipped IP6 address %q\n", dAddr)
		} else {
			log.Printf("Skipped bad IP address %q\n", dAddr)
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
				Num:   defaultQueueNum, // NFQUEUE number
				Total: 1,               // Single queue
				Flag:  0,               // 0 = block; use expr.QueueFlagBypass (1) to bypass if the net filter is not running or if the queue is full
			},
		},
	}
	q.conn.AddRule(rule)
	return nil
}

// sendIP4PacketsToDefaultNFQueue adds nftables rules to send packets to the default NFQUEUE.
// TODO: do rule set replacement atomically
func (q *NFTRules) sendIP4PacketsToDefaultNFQueue() error {
	// Empty the default chain.
	q.conn.FlushChain(q.chain)
	// Add rules for each Ip address.
	err := q.addNFTablesRuleSet()
	if err != nil {
		return fmt.Errorf("failed to add nftables rule for Ip address %q: %v", ip, err)
	}
	// Flush changes to the kernel.
	if err := q.conn.Flush(); err != nil {
		return fmt.Errorf("failed to flush nftables rules: %v", err)
	}
	return nil
}

// Clean deletes the nftables table and therefore all its chains and rules.
func (q *NFTRules) Clean() error {
	return deleteTable(q.conn, q.table.Name)
}

func tableExists(conn *nftables.Conn, tableName string) bool {
	tables, err := conn.ListTables()
	if err != nil {
		log.Fatalf("Failed to list nftables tables: %v\n", err)
	}
	for _, v := range tables {
		if v.Name == tableName {
			return true
		}
	}
	return false
}

func chainExists(conn *nftables.Conn, chainName string) bool {
	chains, err := conn.ListChains()
	if err != nil {
		log.Fatalf("Failed to list nftables chains: %v\n", err)
	}
	for _, v := range chains {
		if v.Name == chainName {
			return true
		}
	}
	return false
}

func getOrCreateTable(conn *nftables.Conn, tableName string) (*nftables.Table, error) {
	var err error
	table := &nftables.Table{
		Family: nftables.TableFamilyIPv4, // TODO: work out if we can use family inet instead for both ip4 and ip16 addresses
		// NOTE: Family: nftables.TableFamilyINet doesn't work for both IPv4 and IPv6 addresses (it produces lower-level rules)
		Name: tableName,
	}
	if !tableExists(conn, tableName) { // TODO: decide if we want to delete/replace the table if it exists already
		conn.AddTable(table)
		err = conn.Flush()
	}
	return table, err
}

func getOrCreateChain(conn *nftables.Conn, table *nftables.Table, chainName string) (*nftables.Chain, error) {
	var err error
	chain := &nftables.Chain{
		Name:     chainName,
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookForward, // input chain is for packets destined for the local machine; forward chain is for packets that are being routed through the local machine; output chain is for packets originating from the local machine
		Priority: nftables.ChainPriorityFilter,
	}
	if !chainExists(conn, table.Name) { // TODO: decide if we want to delete/replace the chain if it exists already
		conn.AddChain(chain)
		err = conn.Flush()
	}
	return chain, err
}

func deleteTable(conn *nftables.Conn, tableName string) error {
	// Delete the table and all its chains and rules.
	conn.DelTable(&nftables.Table{Name: tableName})
	err := conn.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush nft: %v", err)
	}
	if tableExists(conn, tableName) {
		return fmt.Errorf("nft table %q not deleted", defaultTableName)
	}
	return nil
}
