package nft

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

var (
	// conn             = &nftables.Conn{}
	// table            *nftables.Table
	// chain            *nftables.Chain
	defaultTableName = "crazydeer-table"
	defaultChainName = "crazydeer-chain"
	defaultQueueNum  = uint16(100)
)

type NFQueue struct {
	conn  *nftables.Conn
	table *nftables.Table
	chain *nftables.Chain
}

func init() {
	if os.Geteuid() != 0 {
		log.Fatal("You must be root to run this program.")
	}
}

func (q *NFQueue) tableExists(tableName string) bool {
	tables, err := q.conn.ListTables()
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

func (q *NFQueue) chainExists(chainName string) bool {
	chains, err := q.conn.ListChains()
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

func getOrCreateDefaultTable() (*nftables.Table, error) {
	var err error
	table := &nftables.Table{
		Family: nftables.TableFamilyINet,
		Name:   defaultTableName,
	}
	if !tableExists(defaultTableName) {
		conn.AddTable(table)
		err = conn.Flush()
	}
	return table, err
}

func getOrCreateDefaultChain(table *nftables.Table) (*nftables.Chain, error) {
	var err error
	chain := &nftables.Chain{
		Name:     defaultTableName,
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityFilter,
	}
	if !chainExists(defaultChainName) {
		conn.AddChain(chain)
		err = conn.Flush()
	}
	return chain, err
}

// addNFTablesRule add a rule to send traffic to the NFQUEUE for this app.
// The caller should flush the changes to the kernel after.
func (q *NFQueue) addNFTablesRule(ipString string) error {
	// # iptables -I OUTPUT -p icmp -j NFQUEUE --queue-num 100
	// Example YouTube IP address
	ip := net.ParseIP(ipString)
	if ip == nil {
		return errors.New("invalid IP address")
	}

	// Add a rule to send traffic to NFQUEUE
	rule := &nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			// Match destination IP address
			&expr.Payload{
				DestRegister: 1,                             // Store the payload in register 1
				Base:         expr.PayloadBaseNetworkHeader, // Match the network header
				Offset:       16,                            // Offset for IPv4 destination address
				Len:          4,                             // Length of an IPv4 address
				// TODO: handle IPv6 in nftables rules
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     ip.To4(), // Match the specific IPv4 address
			},
			// Send matched packets to NFQUEUE
			&expr.Queue{
				Num:   defaultQueueNum, // NFQUEUE number
				Total: 1,               // Single queue
				Flag:  0,               // 0 should block; expr.QueueFlagBypass will bypass if the net filter is not running or if the queue is full
			},
		},
	}
	conn.AddRule(rule)
	return nil
}

// SendIP4PacketsToDefaultNFQueue adds nftables rules to send packets to the default NFQUEUE.
func (q *NFQueue) SendIP4PacketsToDefaultNFQueue(ips []string) error {
	// Empty the default chain.
	conn.FlushChain(chain)
	// Add rules for each IP address.
	for _, ip := range ips {
		err := addNFTablesRule(ip)
		if err != nil {
			return fmt.Errorf("failed to add nftables rule for IP address %q: %v", ip, err)
		}
	}
	// Flush changes to the kernel.
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("failed to flush nftables rules: %v", err)
	}
	return nil
}

func (q *NFQueue) CleanAll() error {
	// Initialize a new nftables connection
	conn := &nftables.Conn{}

	// Delete the table and all its chains and rules
	conn.DelTable(&nftables.Table{Name: defaultTableName})

	err := conn.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush nft: %v", err)
	}

	if tableExists(defaultTableName) {
		return fmt.Errorf("nft table %q not deleted", defaultTableName)
	}

	return nil
}

func NewNFQueue() (NFQueue, error) {
	var err error

	nfq := NFQueue{}

	table, err =
	if err != nil {
		return NFQueue{}, fmt.Errorf("failed to create nftables table: %v", err)
	}

	chain, err = getOrCreateDefaultChain(table)
	if err != nil {
		return NFQueue{}, fmt.Errorf("failed to create nftables chain: %v", err)
	}

	return nil
}
