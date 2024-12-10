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
	tableName      string
	chainName      string
	conn           *nftables.Conn
	table          *nftables.Table
	chain          *nftables.Chain
	destIPs        models.IpDomains
	srcIPMacGroups models.IpGroups
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
	rules.destIPs.Data = make(models.MapIpDomain)
	return rules, nil
}

// UpdateDestIpDomains is a callback that saves the supplied Ip addresses and updates the nft rules using them.
func (q *NFTRules) UpdateDestIpDomains(newIps models.MapIpDomain) {
	// TODO: don't trust the supplied map is good to just take as we want our own copy.
	q.destIPs.Mu.Lock()
	defer q.destIPs.Mu.Unlock()
	q.destIPs.Data = newIps

	var newKeys []models.Ip
	for k := range newIps {
		newKeys = append(newKeys, k)
	}

	err := q.sendIP4PacketsToDefaultNFQueue(newKeys)
	if err != nil {
		log.Printf("failed to send Ip addresses to default NFQUEUE: %v\n", err)
	}
}

// UpdateSourceIpGroups is a callback that saves the supplied Ip addresses and updates the nft rules using them.
func (q *NFTRules) UpdateSourceIpGroups(newData models.MapIpGroups) {
	// TODO: don't trust the supplied map is good to just take as we want our own copy.
	q.srcIPMacGroups.Mu.Lock()
	defer q.srcIPMacGroups.Mu.Unlock()
	q.srcIPMacGroups.Data = newData

	var newKeys []models.Ip
	for k := range newData {
		newKeys = append(newKeys, k)
	}

	// TODO: add src Ip rules or merge with destIP rules!
	// err := q.sendIP4PacketsToDefaultNFQueue(newKeys)
	// if err != nil {
	// 	log.Printf("failed to send Ip addresses to default NFQUEUE: %v\n", err)
	// }
}

// addNFTablesRule add a rule to send traffic to the NFQUEUE for this app.
// The caller should flush the changes to the kernel after.
func (q *NFTRules) addNFTablesRule(dAddr models.Ip) error {
	// # iptables -I OUTPUT -p icmp -j NFQUEUE --queue-num 100
	// Example YouTube Ip address
	ip := net.ParseIP(string(dAddr))
	if ip == nil {
		return errors.New("invalid Ip address")
	}

	if ip.To4() == nil && ip.To16() == nil { // if the address is not valid...
		log.Printf("Skipped Ip address %q as it is not IPv4 or IPv6\n", dAddr)
		return nil
	}

	var offset, length uint32
	var ipBytes []byte

	if ip.To4() != nil {
		offset = 16
		length = 4
		ipBytes = ip.To4()
	} else {
		log.Printf("Skipped bad Ip address (which may be IPv6) %q\n", dAddr)
		return nil
		// if ip.To16() != nil { // skip if IPv6
		// 	log.Printf("Skipped IPv6 address %q\n", ipString)
		// 	return nil
		// offset = 24
		// length = 16
		// ipBytes = ip.To16()
		// }
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
			// TODO: get TCP port filtering working for 80 and 443
			// &expr.Payload{
			// 	DestRegister: 2,
			// 	Base:         expr.PayloadBaseNetworkHeader,
			// 	Offset:       9, // Offset for protocol field in IPv4 header  // TODO: what is the offset for IPv6?
			// 	Len:          1, // Length of protocol field
			// },
			// // Compare the extracted protocol (in register 2) with TCP
			// &expr.Cmp{
			// 	Op:       expr.CmpOpEq,
			// 	Register: 2,         // Compare value in register 2
			// 	Data:     []byte{6}, // TCP protocol number; see also gopacket/layers.IPProtocolTCP
			// },
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
// TODO: do rule replacement atomically
// TODO: consider if meshing source and dest IPs to create the rules offers a benefit given the cartesian
//
//	product will impact performance. We can probably do without it for now until we know how performant we are.
func (q *NFTRules) sendIP4PacketsToDefaultNFQueue(ips []models.Ip) error {
	// Empty the default chain.
	q.conn.FlushChain(q.chain)
	// Add rules for each Ip address.
	for _, ip := range ips {
		if ip != "" {
			err := q.addNFTablesRule(ip)
			if err != nil {
				return fmt.Errorf("failed to add nftables rule for Ip address %q: %v", ip, err)
			}
		}
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
		Name:   tableName,
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
		Hooknum:  nftables.ChainHookOutput,
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
