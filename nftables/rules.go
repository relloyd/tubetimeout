package nftables

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"

	"example.com/youtube-nfqueue/models"
	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/exp/maps"
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
	models.IpSet
}

func NewNFTRules() (*NFTRules, error) {
	var err error
	nfq := &NFTRules{
		conn:      &nftables.Conn{},
		tableName: defaultTableName,
		chainName: defaultChainName,
	}
	nfq.table, err = getOrCreateTable(nfq.conn, nfq.tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to create nftables table: %v", err)
	}
	nfq.chain, err = getOrCreateChain(nfq.conn, nfq.table, nfq.chainName)
	if err != nil {
		return nil, fmt.Errorf("failed to create nftables chain: %v", err)
	}
	nfq.Ips = make(map[string]struct{})
	return nfq, nil
}

// addNFTablesRule add a rule to send traffic to the NFQUEUE for this app.
// The caller should flush the changes to the kernel after.
func (q *NFTRules) addNFTablesRule(ipString string) error {
	// # iptables -I OUTPUT -p icmp -j NFQUEUE --queue-num 100
	// Example YouTube IP address
	ip := net.ParseIP(ipString)
	if ip == nil {
		return errors.New("invalid IP address")
	}

	if ip.To4() == nil && ip.To16() == nil { // if the address is not valid...
		log.Printf("Skipped IP address %q as it is not IPv4 or IPv6\n", ipString)
		return nil
	}

	var offset, length uint32
	var ipBytes []byte
	if ip.To4() != nil {
		offset = 16
		length = 4
		ipBytes = ip.To4()
	}
	if ip.To16() != nil{
		offset = 24
		length = 16
		ipBytes = ip.To16()
	}

	// Add a rule to send traffic to NFQUEUE
	rule := &nftables.Rule{
		Table: q.table,
		Chain: q.chain,
		Exprs: []expr.Any{
			// Match destination IP address
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
			// Send matched packets to NFQUEUE
			&expr.Queue{
				Num:   defaultQueueNum, // NFQUEUE number
				Total: 1,               // Single queue
				Flag:  0,               // 0 should block; expr.QueueFlagBypass will bypass if the net filter is not running or if the queue is full
			},
		},
	}
	q.conn.AddRule(rule)
	return nil
}

// sendIP4PacketsToDefaultNFQueue adds nftables rules to send packets to the default NFQUEUE.
func (q *NFTRules) sendIP4PacketsToDefaultNFQueue(ips []string) error {
	// Empty the default chain.
	q.conn.FlushChain(q.chain)
	// Add rules for each IP address.
	for _, ip := range ips {
		if ip != "" {
			err := q.addNFTablesRule(ip)
			if err != nil {
				return fmt.Errorf("failed to add nftables rule for IP address %q: %v", ip, err)
			}
		}
	}
	// Flush changes to the kernel.
	if err := q.conn.Flush(); err != nil {
		return fmt.Errorf("failed to flush nftables rules: %v", err)
	}
	return nil
}

// Notify is a callback that saves the supplied IP addresses and updates the nft rules using them.
func (q *NFTRules) Notify(newIps map[string]struct{}) {
	// TODO: don't trust the supplied map is good to just take as we want our own copy.
	q.Mu.Lock()
	defer q.Mu.Unlock()
	q.Ips = newIps

	err := q.sendIP4PacketsToDefaultNFQueue(maps.Keys(newIps))
	if err != nil {
		log.Printf("failed to send IP addresses to default NFQUEUE: %v\n", err)
	}
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
		Family: nftables.TableFamilyINet,
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
