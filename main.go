package main

import (
	"context"
	"sync"

	libp2p "github.com/libp2p/go-libp2p"
	crypto "github.com/libp2p/go-libp2p-crypto"
	discovery "github.com/libp2p/go-libp2p-discovery"
	libp2pdht "github.com/libp2p/go-libp2p-kad-dht"
	peer "github.com/libp2p/go-libp2p-peer"
	peerstore "github.com/libp2p/go-libp2p-peerstore"
	multiaddr "github.com/multiformats/go-multiaddr"
)

func connectToHost(config Config) {
	ctx := context.Background()

	// libp2p.New constructs a new libp2p Host. Other options can be added
	// here.
	priv, _, _ := crypto.GenerateKeyPair(crypto.Secp256k1, 256)

	host, err := libp2p.New(
		ctx,
		libp2p.ListenAddrs([]multiaddr.Multiaddr(config.ListenAddresses)...),
		libp2p.Identity(priv),
		libp2p.DisableRelay(),
	)
	if err != nil {
		panic(err)
	}

	node := NewNode(host)

	// ----------------------------
	// Start a DHT, for use in peer discovery. We can't just make a new DHT
	// client because we want each peer to maintain its own local copy of the
	// DHT, so that the bootstrapping node of the DHT can go down without
	// inhibiting future peer discovery.
	kademliaDHT, err := libp2pdht.New(ctx, host)
	if err != nil {
		panic(err)
	}

	// Bootstrap the DHT. In the default configuration, this spawns a Background
	// thread that will refresh the peer table every five minutes.
	node.debugPrintln("Bootstrapping the DHT")
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		panic(err)
	}

	// Let's connect to the bootstrap nodes first. They will tell us about the
	// other nodes in the network.
	var wg sync.WaitGroup
	for _, peerAddr := range config.BootstrapPeers {
		peerinfo, _ := peerstore.InfoFromP2pAddr(peerAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := host.Connect(ctx, *peerinfo); err != nil {
				node.debugPrintln(err)
			} else {
				node.debugPrintln("Connection established with bootstrap node:", *peerinfo)
			}
		}()
	}
	wg.Wait()

	// We use a rendezvous point "meet me here" to announce our location.
	// This is like telling your friends to meet you at the Eiffel Tower.

	node.debugPrintln("Announcing ourselves...")

	routingDiscovery := discovery.NewRoutingDiscovery(kademliaDHT)
	discovery.Advertise(ctx, routingDiscovery, config.RendezvousString)

	node.debugPrintln("Successfully announced!")

	// Now, look for others who have announced
	// This is like your friend telling you the location to meet you.

	node.debugPrintln("Searching for other peers...")

	peerChan, err := routingDiscovery.FindPeers(ctx, config.RendezvousString)
	if err != nil {
		panic(err)
	}

	clientPeer := &Peer{
		peerID:   host.ID(),
		username: config.Username}

	if config.HostPeerID != "" {
		hostID, err := peer.IDB58Decode(config.HostPeerID)
		if err != nil {
			node.debugPrintln("Invalid peerId.")
			return
		}
		for peer := range peerChan {
			if peer.ID == host.ID() {
				continue
			}
			if peer.ID == hostID {
				node.JoinChannel(clientPeer, peer.ID, config.ChannelName)
			}
			// if peer ID == peer.ID
		}
	} else {
		var channelConfig *ChannelConfig
		if config.ChannelConfigFile == "" {
			channelConfig = DefaultChannelConfig()
		} else {
			ReadChannelConfigs(config.ChannelConfigFile, channelConfig)
		}
		node.CreateChannel(clientPeer, config.ChannelName, DefaultChannelConfig())
	}

	chatUI := NewChatUI(node, config.ChannelName, clientPeer)
	chatUI.StartUI()
	// there was a way to enter peer id lol
	select {}

}

func main() {
	// // Parse flags
	config, err := ParseFlags()
	if err != nil {
		panic(err)
	}

	connectToHost(config)
}
