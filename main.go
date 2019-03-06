package main

import (
	"context"

	libp2p "github.com/libp2p/go-libp2p"
	crypto "github.com/libp2p/go-libp2p-crypto"
	peerstore "github.com/libp2p/go-libp2p-peerstore"
	multiaddr "github.com/multiformats/go-multiaddr"
)

func makeHost(config Config) *Node {
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

	return NewNode(host)
}

func startMultilogue(config Config) {
	client := makeHost(config)

	clientPeer := &Peer{
		peerID:   client.ID(),
		username: config.Username}

	var chatUI *ChatUI
	if config.HostMultiAddress != "" {
		maddr, err := multiaddr.NewMultiaddr(config.HostMultiAddress)
		if err != nil {
			client.debugPrintln(err)
		}

		info, err := peerstore.InfoFromP2pAddr(maddr)
		if err != nil {
			client.debugPrintln(err)
		}

		client.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)

		client.JoinChannel(clientPeer, info.ID, config.ChannelName)
		chatUI = NewChatUI(client, config.ChannelName, clientPeer, info.ID)
	} else {
		host := makeHost(config)
		hostID := host.ID()

		host.Peerstore().AddAddrs(client.ID(), client.Addrs(), peerstore.PermanentAddrTTL)
		client.Peerstore().AddAddrs(host.ID(), host.Addrs(), peerstore.PermanentAddrTTL)

		var channelConfig *ChannelConfig
		if config.ChannelConfigFile == "" {
			channelConfig = DefaultChannelConfig()
		} else {
			ReadChannelConfigs(config.ChannelConfigFile, channelConfig)
		}
		host.CreateChannel(clientPeer, config.ChannelName, channelConfig)
		client.JoinChannel(clientPeer, hostID, config.ChannelName)
		chatUI = NewChatUI(client, config.ChannelName, clientPeer, hostID)
	}

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

	startMultilogue(config)
}
