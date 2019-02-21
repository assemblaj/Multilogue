package main

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	libp2p "github.com/libp2p/go-libp2p"
	crypto "github.com/libp2p/go-libp2p-crypto"
	ma "github.com/multiformats/go-multiaddr"
)

// helper method - create a lib-p2p host to listen on a port
func makeTestNode() *Node {
	// Generating random post
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	// Ignoring most errors for brevity
	// See echo example for more details and better implementation
	priv, _, _ := crypto.GenerateKeyPair(crypto.Secp256k1, 256)
	listen, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))
	host, _ := libp2p.New(
		context.Background(),
		libp2p.ListenAddrs(listen),
		libp2p.Identity(priv),
		libp2p.DisableRelay(),
	)

	return NewNode(host)
}

// API Tests
func TestCreateChannel(t *testing.T) {
	host := makeTestNode()

	host.CreateChannel("test", DefaultChannelConfig())

	_, exists := host.channels["test"]
	if !exists {
		t.Errorf("CreateChannel failed to create channel.")
	}
}

func TestDeleteChannel(t *testing.T) {
	host := makeTestNode()

	host.CreateChannel("test", DefaultChannelConfig())
	host.DeleteChannel("test")

	_, exists := host.channels["test"]
	if exists {
		t.Errorf("CreateChannel failed to delete channel.")
	}
}
