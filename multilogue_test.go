package main

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	ps "github.com/libp2p/go-libp2p-peerstore"

	libp2p "github.com/libp2p/go-libp2p"
	crypto "github.com/libp2p/go-libp2p-crypto"
	ma "github.com/multiformats/go-multiaddr"
)

func makeTestNodePort(port int) *Node {
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

// helper method - create a lib-p2p host to listen on a port
func makeTestNode() *Node {
	// Generating random post
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	return makeTestNodePort(port)
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
		t.Errorf("DeleteChannel failed to delete channel.")
	}
}

func TestJoinChannel(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)

	host1.CreateChannel("test", DefaultChannelConfig())

	host2IDString := host2.ID().String()

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	var host2Notified bool

	_, channelExists := host2.channels["test"]
	if !channelExists {
		t.Errorf("Test channel was not added to chanel 2 ")
	}

	select {
	case host2Notified = <-req.success:
		break
	case <-time.After(1 * time.Second):
		host2Notified = false
		break
	}

	_, host1AddedChannel := host1.channels["test"].peers[host2IDString]

	if !host1AddedChannel || !host2Notified {
		t.Errorf("Failed to join channel. host1AddedChannel: %t host2Notified: %t ", host1AddedChannel, host2Notified)
	}
}

func TestLeaveChannel(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)

	host1.CreateChannel("test", DefaultChannelConfig())

	host2IDString := host2.ID().String()

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	select {
	case <-req.success:
		host2.LeaveChannel(host2Peer, host1.ID(), "test")
		break
	case <-time.After(1 * time.Second):
		break
	}

	<-time.After(2 * time.Second)
	channel := host1.channels["test"]

	_, peerExists := channel.peers[host2IDString]
	if peerExists {
		t.Errorf("Host2 was not remove from channel. ")
	}

}

func TestReqestTransmission(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)

	host1.CreateChannel("test", DefaultChannelConfig())

	//host2IDString := host2.ID().String()

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	requestRecieved := false

	select {
	case <-req.success:
		req2, _ := host2.SendTransmissionRequest(host2Peer, host1.ID(), "test")
		select {
		// Just testing if the request was recieved at all. Ideally it should be
		// accepted in this scenario (first user starting transmisison), but
		// that's not what we're testing
		case <-req2.success:
			requestRecieved = true
			break
		case <-req2.fail:
			requestRecieved = true
			break
		case <-time.After(1 * time.Second):
			break
		}
		break
	case <-time.After(1 * time.Second):
		break
	}

	if !requestRecieved {
		t.Errorf("Host2 request not sent or recieved. ")
	}

}

func TestSendMessage(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)

	host1.CreateChannel("test", DefaultChannelConfig())

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	messageRecieved := false

	select {
	case <-req.success:
		req2, _ := host2.SendTransmissionRequest(host2Peer, host1.ID(), "test")
		select {
		// Just testing if the request was recieved at all. Ideally it should be
		// accepted in this scenario (first user starting transmisison), but
		// that's not what we're testing
		case <-req2.success:
			req3, _ := host2.SendMessage(host2Peer, host1.ID(), "test", "Hello World!")
			select {
			case <-req3.success:
				messageRecieved = true
				break
			case <-host1.channels["test"].output.messageQueue:
				messageRecieved = true
				break
			case <-time.After(3 * time.Second):
				break
			}
			break
		case <-time.After(3 * time.Second):
			break
		}
		break
	case <-time.After(3 * time.Second):
		break
	}

	if !messageRecieved {
		t.Errorf("Host2 message not recieved. ")
	}

}

func TestEndTransmission(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)

	host1.CreateChannel("test", DefaultChannelConfig())

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	transmissionEnded := false

	select {
	case <-req.success:
		req2, _ := host2.SendTransmissionRequest(host2Peer, host1.ID(), "test")
		select {
		// Just testing if the request was recieved at all. Ideally it should be
		// accepted in this scenario (first user starting transmisison), but
		// that's not what we're testing
		case <-req2.success:
			req3, _ := host2.SendMessage(host2Peer, host1.ID(), "test", "Hello World!")
			select {
			case <-req3.success:
				host2.EndTransmission(host2Peer, host1.ID(), "test")
				select {
				case <-time.After(3 * time.Second):
					if host1.channels["test"].currentTransmission == nil {
						transmissionEnded = true
					}
					break
				}
				break
			case <-time.After(3 * time.Second):
				break
			}
			break
		case <-time.After(3 * time.Second):
			break
		}
		break
	case <-time.After(3 * time.Second):
		break
	}

	if !transmissionEnded {
		t.Errorf("Host2 transmission not ended. ")
	}

}
