// From: github.com/libp2p/go-libp2p-examples/blob/master/chat-with-rendezvous/flags.go
// Authors
// Abhishek Upperwal
// Mantas Vidutis
// TODO: Read config file form here too
package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"strings"

	maddr "github.com/multiformats/go-multiaddr"
)

// A new type we need for writing a custom flag parser
type addrList []maddr.Multiaddr

func (al *addrList) String() string {
	strs := make([]string, len(*al))
	for i, addr := range *al {
		strs[i] = addr.String()
	}
	return strings.Join(strs, ",")
}

func (al *addrList) Set(value string) error {
	addr, err := maddr.NewMultiaddr(value)
	if err != nil {
		return err
	}
	*al = append(*al, addr)
	return nil
}

// IPFS bootstrap nodes. Used to find other peers in the network.
var defaultBootstrapAddrStrings = []string{
	"/ip4/104.131.131.82/tcp/4001/ipfs/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",
	"/ip4/104.236.179.241/tcp/4001/ipfs/QmSoLPppuBtQSGwKDZT2M73ULpjvfd3aZ6ha4oFGL1KrGM",
	"/ip4/104.236.76.40/tcp/4001/ipfs/QmSoLV4Bbm51jM9C4gDYZQ9Cy3U6aXMJDAbzgu2fzaDs64",
	"/ip4/128.199.219.111/tcp/4001/ipfs/QmSoLSafTMBsPKadTEgaXctDQVcqN88CNLHXMkTNwMKPnu",
	"/ip4/178.62.158.247/tcp/4001/ipfs/QmSoLer265NRgSp2LA3dPaeykiS1J6DifTC88f5uVQKNAd",
}

func StringsToAddrs(addrStrings []string) (maddrs []maddr.Multiaddr, err error) {
	for _, addrString := range addrStrings {
		addr, err := maddr.NewMultiaddr(addrString)
		if err != nil {
			return maddrs, err
		}
		maddrs = append(maddrs, addr)
	}
	return
}

type Config struct {
	RendezvousString string
	BootstrapPeers   addrList
	ListenAddresses  addrList
	ProtocolID       string
}

type ChannelConfig struct {
	MessageLimit    int
	CooldownPeriod  int
	TimeLimit       int
	MaxMessageRatio float64
	HistorySize     int
}

func ParseFlags() (Config, error) {
	config := Config{}
	flag.StringVar(&config.RendezvousString, "rendezvous", "gravitation",
		"Unique string to identify group of nodes. Share this with your friends to let them connect with you")
	flag.Var(&config.BootstrapPeers, "peer", "Adds a peer multiaddress to the bootstrap list")
	flag.Var(&config.ListenAddresses, "listen", "Adds a multiaddress to the listen list")
	flag.StringVar(&config.ProtocolID, "pid", "/chat/1.1.0", "Sets a protocol id for stream headers")
	flag.Parse()

	if len(config.BootstrapPeers) == 0 {
		bootstrapPeerAddrs, err := StringsToAddrs(defaultBootstrapAddrStrings)
		if err != nil {
			return config, err
		}
		config.BootstrapPeers = bootstrapPeerAddrs
	}

	return config, nil
}

func ReadChannelConfigs(fname string, config *ChannelConfig) {
	b, err := ioutil.ReadFile(fname)
	if err != nil {
		log.Println("Error reading data from file. ")
	}
	err = json.Unmarshal(b, config)
	if err != nil {
		log.Println("Error loading data. ")
	}
}

func DefaultChannelConfig() *ChannelConfig {

	defaultChannelConfig := &ChannelConfig{
		MessageLimit:    5,
		CooldownPeriod:  30,  // Seconds
		TimeLimit:       3,   // Minutes
		MaxMessageRatio: .05, // Between 0, 1
		HistorySize:     2}

	return defaultChannelConfig
}
