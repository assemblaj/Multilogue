package main

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	crypto "github.com/libp2p/go-libp2p-crypto"
	ps "github.com/libp2p/go-libp2p-peerstore"
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

	host.CreateChannel(&Peer{}, "test", DefaultChannelConfig())

	_, exists := host.channels.Get("test")
	if !exists {
		t.Errorf("CreateChannel failed to create channel.")
	}
}

func TestDeleteChannel(t *testing.T) {
	host := makeTestNode()

	host.CreateChannel(&Peer{}, "test", DefaultChannelConfig())
	host.DeleteChannel("test")

	_, exists := host.channels.Get("test")
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

	host1.CreateChannel(&Peer{}, "test", DefaultChannelConfig())

	host2IDString := host2.ID().String()

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	var host2Notified *Response

	_, channelExists := host2.channels.Get("test")
	if !channelExists {
		t.Errorf("Test channel was not added to chanel 2 ")
	}

	select {
	case host2Notified = <-req.success:
		break
	case <-time.After(1 * time.Second):
		break
	}

	val, _ := host1.channels.Get("test")
	channel := val.(*Channel)
	_, host1AddedChannel := channel.peers.Get(host2IDString)

	if !host1AddedChannel || host2Notified == nil {
		t.Errorf("Failed to join channel. host1AddedChannel: %t host2Notified: %d ", host1AddedChannel, host2Notified.errorCode)
	}
}

func TestLeaveChannel(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)

	host1.CreateChannel(&Peer{}, "test", DefaultChannelConfig())

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
	val, _ := host1.channels.Get("test")
	channel := val.(*Channel)

	_, peerExists := channel.peers.Get(host2IDString)
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

	host1.CreateChannel(&Peer{}, "test", DefaultChannelConfig())

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

	host1.CreateChannel(&Peer{}, "test", DefaultChannelConfig())

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	messageRecieved := false

	select {
	case <-req.success:
		req2, _ := host2.SendTransmissionRequest(host2Peer, host1.ID(), "test")
		val, _ := host2.channels.Get("test")
		channel := val.(*Channel)
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
			case <-channel.output.messageQueue:
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

	host1.CreateChannel(&Peer{}, "test", DefaultChannelConfig())

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
			val, _ := host1.channels.Get("test")
			channel := val.(*Channel)

			select {
			case <-req3.success:
				host2.EndTransmission(host2Peer, host1.ID(), "test")
				select {
				case <-time.After(3 * time.Second):
					channel.RLock()
					if channel.currentTransmission == nil {
						transmissionEnded = true
					}
					channel.RUnlock()
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

// need to have 3 hosts here
// the 2nd  compete
func TestCooldown(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)
	config := DefaultChannelConfig()
	config.HistorySize = 0
	config.CooldownPeriod = 8

	host1.CreateChannel(&Peer{}, "test", config)

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	var transmissionDenied *Response

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
					req4, _ := host2.SendTransmissionRequest(host2Peer, host1.ID(), "test")
					select {
					case transmissionDenied = <-req4.fail:
						break
					case <-time.After(1 * time.Second):
						break
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

	if transmissionDenied == nil || transmissionDenied.errorCode != CooldownError {
		t.Errorf("Host2 transmission was not denied upon violating cooldown.  ")
	}

}

func TestMessageLimit(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)

	config := DefaultChannelConfig()
	config.MaxMessageRatio = 10000

	host1.CreateChannel(&Peer{}, "test", config)

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	var messageLimitFailed *Response

	select {
	case <-req.success:
		req2, _ := host2.SendTransmissionRequest(host2Peer, host1.ID(), "test")
		select {
		// Just testing if the request was recieved at all. Ideally it should be
		// accepted in this scenario (first user starting transmisison), but
		// that's not what we're testing
		case <-req2.success:
			for i := 0; i < config.MessageLimit; i++ {
				host2.SendMessage(host2Peer, host1.ID(), "test", "Hello World!")
			}
			req3, _ := host2.SendMessage(host2Peer, host1.ID(), "test", "Hello World!")
			select {
			case messageLimitFailed = <-req3.fail:
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

	if messageLimitFailed == nil || messageLimitFailed.errorCode != MessageLimitError {
		t.Errorf("Host limit did not deny. ")
	}

}

func TestTimeLimit(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)
	config := DefaultChannelConfig()
	config.TimeLimit = config.TimeLimit + 1

	host1.CreateChannel(&Peer{}, "test", DefaultChannelConfig())

	//host2IDString := host2.ID().String()

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	var timeLimitDeny *Response

	select {
	case <-req.success:
		host2.SendTransmissionRequest(host2Peer, host1.ID(), "test")
		timeLimit := time.Duration(config.TimeLimit) * time.Second
		<-time.After(timeLimit)
		req3, _ := host2.SendMessage(host2Peer, host1.ID(), "test", "Hello World!")

		select {
		// Just testing if the request was recieved at all. Ideally it should be
		// accepted in this scenario (first user starting transmisison), but
		// that's not what we're testing
		case timeLimitDeny = <-req3.fail:
			break
		}
		break
	case <-time.After(1 * time.Second):
		break
	}

	if timeLimitDeny == nil || timeLimitDeny.errorCode != TimeLimitError {
		t.Errorf("Message was not denied after time limit . ")
	}

}

func TestRatioLimit(t *testing.T) {
	rand.Seed(666)
	port := rand.Intn(100) + 10000

	host1 := makeTestNodePort(port)
	host2 := makeTestNodePort(port + 1)

	host1.Peerstore().AddAddrs(host2.ID(), host2.Addrs(), ps.PermanentAddrTTL)
	host2.Peerstore().AddAddrs(host1.ID(), host1.Addrs(), ps.PermanentAddrTTL)

	config := DefaultChannelConfig()
	config.MessageLimit = 10000

	host1.CreateChannel(&Peer{}, "test", config)

	host2Peer := &Peer{
		peerID:   host2.ID(),
		username: "host2"}

	req, _ := host2.JoinChannel(host2Peer, host1.ID(), "test")

	var ratioLimitFailed *Response

	select {
	case <-req.success:
		req2, _ := host2.SendTransmissionRequest(host2Peer, host1.ID(), "test")
		select {
		// Just testing if the request was recieved at all. Ideally it should be
		// accepted in this scenario (first user starting transmisison), but
		// that's not what we're testing
		case <-req2.success:
			for i := 1; i < 20; i++ {
				host2.SendMessage(host2Peer, host1.ID(), "test", "Hello World!")
			}
			req3, _ := host2.SendMessage(host2Peer, host1.ID(), "test", "Hello World!")
			select {
			case ratioLimitFailed = <-req3.fail:
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

	if ratioLimitFailed == nil || ratioLimitFailed.errorCode != RatioError {
		t.Errorf("Ratio limit did not deny. ")
	}

}
