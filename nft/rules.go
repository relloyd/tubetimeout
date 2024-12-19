package nft

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"

	"example.com/youtube-nfqueue/models"
	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

func init() {
	if os.Geteuid() != 0 {
		log.Fatal("You must be root to run this program.")
	}
}

const (
	defaultTableName     = "crazydeer-table"
	defaultChainName     = "crazydeer-chain"
	defaultQueueNumDest  = uint16(100) // TODO: do we need one queue per NFT rule?
	defaultSrcIpSetName  = "local_ip_set"
	defaultDestIpSetName = "remote_ip_set"
	// defaultQueueNumSrcDest = uint16(101)
	// defaultQueueNumDestSrc = uint16(102)
)

type NFTRules struct {
	conn          *nftables.Conn
	tableName     string
	chainName     string
	table         *nftables.Table
	chain         *nftables.Chain
	nameSetLocal  string
	nameSetRemote string
	setLocal      *nftables.Set
	setRemote     *nftables.Set
	remoteIPs     []nftables.SetElement
	localIPs      []nftables.SetElement
	mu            sync.Mutex
}

func NewNFTRules() (*NFTRules, error) {
	var err error
	rules := &NFTRules{
		conn:          &nftables.Conn{},
		tableName:     defaultTableName,
		chainName:     defaultChainName,
		nameSetLocal:  defaultSrcIpSetName,
		nameSetRemote: defaultDestIpSetName,
		localIPs:      make([]nftables.SetElement, 0),
		remoteIPs:     make([]nftables.SetElement, 0),
	}

	rules.table, err = getOrCreateTable(rules.conn, rules.tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to create nftables table: %v", err)
	}

	rules.chain, err = getOrCreateChain(rules.conn, rules.table, rules.chainName)
	if err != nil {
		return nil, fmt.Errorf("failed to create nftables chain: %v", err)
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

	// Create NFTables rules for src-dest and dest-src combinations.
	err = rules.addNFTablesRuleForSets(defaultQueueNumDest, rules.nameSetLocal, rules.nameSetRemote)
	if err != nil {
		return nil, fmt.Errorf("failed to create NFT rule for src-dest combination")
	}
	err = rules.addNFTablesRuleForSets(defaultQueueNumDest, rules.nameSetRemote, rules.nameSetLocal)
	if err != nil {
		return nil, fmt.Errorf("failed to create NFT rule for dest-src combination")
	}

	// Flush changes to the kernel.
	if err = rules.conn.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush nftables rules: %v", err)
	}

	return rules, nil
}

// UpdateDestIpDomains is a callback that saves the supplied Ip addresses and updates the nft rules using them.
func (q *NFTRules) UpdateDestIpDomains(newData models.MapIpDomain) {
	log.Printf("NFT callback with new destination IPs: %v", newData)

	// Convert to set elements and save.
	var newIps []nftables.SetElement
	for k := range newData {
		ip := net.ParseIP(string(k)).To4()
		if ip != nil {
			newIps = append(newIps, nftables.SetElement{Key: ip})
		} else {
			log.Printf("NFT destination IP callback discarded IPv6 address %v", k)
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	q.remoteIPs = newIps

	// Refresh the NFTables rules.
	err := q.updateIpSets()
	if err != nil {
		log.Printf("NFT callback with new destination IPs couldn't make the update: %v", err)
	}
}

// UpdateSourceIpGroups is a callback that saves the supplied Ip addresses and updates the nft rules using them.
func (q *NFTRules) UpdateSourceIpGroups(newData models.MapIpGroups) {
	log.Printf("NFT callback with new source IPs: %v", newData)

	// Convert to set elements and save.
	var newIps []nftables.SetElement
	for k := range newData {
		ip := net.ParseIP(string(k)).To4()
		if ip != nil {
			newIps = append(newIps, nftables.SetElement{Key: ip})
		} else {
			log.Printf("NFT source IP callback discarded IPv6 address %v", k)
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	q.localIPs = newIps

	err := q.updateIpSets()
	if err != nil {
		log.Printf("NFT callback with new source IPs couldn't make the update: %v", err)
	}
}

// updateIpSets adds nftables rules to send packets to the default NFQs.
// This should be done under a mutex since it reads the NFTRules srcIps and destIps.
func (q *NFTRules) updateIpSets() error {
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
	return nil
}

// addNFTablesRuleSet creates NFTables rules by creating a rule that sends traffic to the given NFQueue number.
// It uses a set for each of the source and dest IP slices supplied.
// The caller should flush the changes to the kernel after.
// # iptables -I OUTPUT -p icmp -j NFQUEUE --queue-num 100
func (q *NFTRules) addNFTablesRuleForSets(nfqNumber uint16, srcSetName, destSetName string) error {
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
func (q *NFTRules) addNFTablesRuleForSingleDestAddr(dAddr models.Ip) error {
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
				Num:   defaultQueueNumDest, // NFQUEUE number
				Total: 1,                   // Single queue
				Flag:  0,                   // 0 = block; use expr.QueueFlagBypass (1) to bypass if the net filter is not running or if the queue is full
			},
		},
	}
	q.conn.AddRule(rule)
	return nil
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

// Clean deletes the nftables table and therefore all its chains and rules.
func (q *NFTRules) Clean() error {
	return deleteTable(q.conn, q.table.Name)
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
