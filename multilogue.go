package main

import (
	"bufio"
	"context"
	"log"

	p2p "github.com/assemblaj/Multilogue/pb"
	"github.com/gogo/protobuf/proto"
	protobufCodec "github.com/multiformats/go-multicodec/protobuf"

	uuid "github.com/google/uuid"
	inet "github.com/libp2p/go-libp2p-net"
	peer "github.com/libp2p/go-libp2p-peer"
)

// pattern: /protocol-name/request-or-response-message/version
const clientJoinChannel = "/multilogue/clientjoinchannel/0.0.1"
const clientLeaveChannel = "/multilogue/clientleavechannel/0.0.1"
const clientSendMessage = "/multilogue/clientsendmessage/0.0.1"
const clientTransmissionStart = "/multilogue/clienttransmissionstart/0.0.1"
const clientTransmissionEnd = "/multilogue/clienttransmissionend/0.0.1"

const hostAcceptClient = "/multilogue/hostacceptclient/0.0.1"
const hostDenyClient = "/multilogue/hostdenyclient/0.0.1"
const hostAcceptTransmission = "/multilogue/hostaccepttransmission/0.0.1"
const hostDenyTransmission = "/multilogue/hostdenytransmission/0.0.1"
const hostBroadcastMessage = "/multilogue/hostbroadcastmessage/0.0.1"

// might add more data for statistics later
// join timestamp
// total messages
// etc etc
type Peer struct {
	peerId           string
	username         string
	lastMessage      int // unix timestamp of last message
	lastTransmission int // unix timestamp of last transmission
}

type Transmission struct {
	channelId string
	peer      *Peer
	totalMsgs int
	startTime int
}

type Channel struct {
	channelId string
	history   []string         // peer Ids of users who last spoke
	peers     map[string]*Peer // slice of peer objects
	// probably need a map of peerId to cooldown
	currentTransmission *Transmission
}

// Protocol state enum
type ProtocolState int

const (
	HostRequestWait ProtocolState = iota
	HostMessageWait
	ClientAcceptWait
	ClientSendMessage
)

// MultilogueProtocol type
type MultilogueProtocol struct {
	node     *Node               // local host
	channels map[string]*Channel // channelId : *Channel
}

// NewMultilogueProtocol Create instance of protocol
// Might want a data object
func NewMultilogueProtocol(node *Node) *MultilogueProtocol {
	p := &MultilogueProtocol{
		node: node}

	node.SetStreamHandler(clientJoinChannel, p.onClientJoinChannel)
	node.SetStreamHandler(clientLeaveChannel, p.onClientLeaveChannel)
	node.SetStreamHandler(clientSendMessage, p.onClientSendMessage)
	node.SetStreamHandler(clientTransmissionStart, p.onClientTransmissionStart)
	node.SetStreamHandler(clientTransmissionEnd, p.onClientTransmissionEnd)

	node.SetStreamHandler(hostAcceptClient, p.onHostAcceptClient)
	node.SetStreamHandler(hostDenyClient, p.onHostDenyClient)
	node.SetStreamHandler(hostAcceptTransmission, p.onHostAcceptTransmission)
	node.SetStreamHandler(hostDenyTransmission, p.onHostDenyTransmission)
	node.SetStreamHandler(hostBroadcastMessage, p.onHostBroadcastMessage)
	return p
}

// Handled when hosting
// Validate and broadcast this message to all peers
// deny if necesary
func (p *MultilogueProtocol) onClientSendMessage(s inet.Stream) {

}

// verify this and set new transmission
// deny if necesary
func (p *MultilogueProtocol) onClientTransmissionStart(s inet.Stream) {

}

// verify this and delete transmission
func (p *MultilogueProtocol) onClientTransmissionEnd(s inet.Stream) {
	// get request data
	data := &p2p.ClientTransmissionEnd{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("%s: User: %s (%s) ending transmission. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer())

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		log.Println("Failed to authenticate message")
		return
	}

	// Protocol Logic
	// Leaving channel
	channel, exists := p.channels[data.HostData.ChannelId]
	if exists {
		// remove request from map as we have processed it here
		_, hasPeer := channel.peers[data.ClientData.PeerId]
		if hasPeer {
			currentChannelClient := channel.currentTransmission.peer.peerId
			givenChannelClient := data.HostData.PeerId
			remoteRequester := s.Conn().RemotePeer().String()
			if currentChannelClient == givenChannelClient && currentChannelClient == remoteRequester {
				channel.currentTransmission = nil
			}
		}
	}

	log.Printf("%s: User: %s (%s) ended transmission. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer())

}

// add to channel
// deny if necesary
func (p *MultilogueProtocol) onClientJoinChannel(s inet.Stream) {
	// get request data
	data := &p2p.ClientJoinChannel{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		log.Println(err)
		return
	}

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		log.Println("Failed to authenticate message")
		return
	}

	// Protocol Logic
	// Adding to channel
	accepted := false
	channel, exists := p.channels[data.HostData.ChannelId]
	if exists {
		// Accept if the peerId and username aren't already in the channel
		_, hasPeer := channel.peers[data.ClientData.PeerId]
		if !hasPeer {
			accepted = true
			for _, peer := range channel.peers {
				if data.ClientData.Username == peer.username {
					accepted = false
				}
			}
		}
		if accepted {
			channel.peers[data.ClientData.PeerId] = &Peer{
				peerId:   data.ClientData.PeerId,
				username: data.ClientData.Username,
			}
		}
	}

	// generate response message
	// Returning separate messages based on accepted or not
	var resp proto.Message
	var respErr error

	if accepted {
		resp = &p2p.HostAcceptClient{MessageData: p.node.NewMessageData(data.MessageData.Id, false),
			ClientData: data.ClientData,
			HostData:   data.HostData}

		// sign the data
		signature, err := p.node.signProtoMessage(resp)
		if err != nil {
			log.Println("failed to sign response")
			return
		}

		// Cannot take the above code outside of the if because I need to do the following
		acceptClientResp := resp.(*p2p.HostAcceptClient)

		// add the signature to the message
		acceptClientResp.MessageData.Sign = signature

		// Have to use the constant for the protocol message name string because reasons
		// So that had to be done within this if
		s, respErr = p.node.NewStream(context.Background(), s.Conn().RemotePeer(), hostAcceptClient)
	} else {
		resp = &p2p.HostDenyClient{MessageData: p.node.NewMessageData(data.MessageData.Id, false),
			ClientData: data.ClientData,
			HostData:   data.HostData}

		// sign the data
		signature, err := p.node.signProtoMessage(resp)
		if err != nil {
			log.Println("failed to sign response")
			return
		}
		denyClientResp := resp.(*p2p.HostDenyClient)

		// add the signature to the message
		denyClientResp.MessageData.Sign = signature
		s, respErr = p.node.NewStream(context.Background(), s.Conn().RemotePeer(), hostDenyClient)
	}
	if respErr != nil {
		log.Println(respErr)
		return
	}

	// send the response
	ok := p.node.sendProtoMessage(resp, s)

	if ok {
		log.Printf("%s: Join Channel response to %s sent.", s.Conn().LocalPeer().String(), s.Conn().RemotePeer().String())
	}

}

// delete form channel
func (p *MultilogueProtocol) onClientLeaveChannel(s inet.Stream) {
	// get request data
	data := &p2p.ClientLeaveChannel{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("%s: User: %s (%s) Leaving Channel %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		log.Println("Failed to authenticate message")
		return
	}

	// Protocol Logic
	// Leaving channel
	channel, exists := p.channels[data.HostData.ChannelId]
	if exists {
		// remove request from map as we have processed it here
		_, hasPeer := channel.peers[data.ClientData.PeerId]
		if hasPeer {
			delete(channel.peers, data.ClientData.PeerId)
		}
	}

	log.Printf("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

// Handled when Client
// Start sending messages
func (p *MultilogueProtocol) onHostAcceptTransmission(s inet.Stream) {

}

func (p *MultilogueProtocol) onHostDenyTransmission(s inet.Stream) {}

// Start reading messages
func (p *MultilogueProtocol) onHostBroadcastMessage(s inet.Stream) {

}

func (p *MultilogueProtocol) onHostAcceptClient(s inet.Stream) {}

func (p *MultilogueProtocol) onHostDenyClient(s inet.Stream) {}

// TODO: Design proper API
func (p *MultilogueProtocol) SendMessage(clientPeer *Peer, hostPeerID peer.ID, channelId string, message string) bool {
	log.Printf("%s: Sending message to % Channel : %s....", p.node.ID(), hostPeerID, channelId)

	// create message data
	req := &p2p.ClientSendMessage{
		MessageData: p.node.NewMessageData(uuid.New().String(), false),
		ClientData: &p2p.ClientData{
			PeerId:   clientPeer.peerId,
			Username: clientPeer.username},
		HostData: &p2p.HostData{
			ChannelId: channelId,
			PeerId:    hostPeerID.String()},
		Message: message}

	// sign the data
	signature, err := p.node.signProtoMessage(req)
	if err != nil {
		log.Println("failed to sign pb data")
		return false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientSendMessage)
	if err != nil {
		log.Println(err)
		return false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return false
	}

	// store ref request so response handler has access to it
	//p.requests[req.MessageData.Id] = req
	//log.Printf("%s: Gravitation to: %s was sent. Message Id: %s", p.node.ID(), hostPeerID, req.MessageData.Id, req.Profile, req.SubOrbit)
	return true

}

func (p *MultilogueProtocol) JoinChannel(clientPeer *Peer, hostPeerID peer.ID, channelId string) bool {
	log.Printf("%s: Joining %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

	// create message data
	req := &p2p.ClientJoinChannel{
		MessageData: p.node.NewMessageData(uuid.New().String(), false),
		ClientData: &p2p.ClientData{
			PeerId:   clientPeer.peerId,
			Username: clientPeer.username},
		HostData: &p2p.HostData{
			ChannelId: channelId,
			PeerId:    hostPeerID.String()}}

	// sign the data
	signature, err := p.node.signProtoMessage(req)
	if err != nil {
		log.Println("failed to sign pb data")
		return false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientJoinChannel)
	if err != nil {
		log.Println(err)
		return false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return false
	}

	// store ref request so response handler has access to it
	//p.requests[req.MessageData.Id] = req
	//log.Printf("%s: Gravitation to: %s was sent. Message Id: %s", p.node.ID(), hostPeerID, req.MessageData.Id, req.Profile, req.SubOrbit)
	return true

}

func (p *MultilogueProtocol) LeaveChannel(clientPeer *Peer, hostPeerID peer.ID, channelId string) bool {
	log.Printf("%s: Leaving %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

	// create message data
	req := &p2p.ClientLeaveChannel{
		MessageData: p.node.NewMessageData(uuid.New().String(), false),
		ClientData: &p2p.ClientData{
			PeerId:   clientPeer.peerId,
			Username: clientPeer.username},
		HostData: &p2p.HostData{
			ChannelId: channelId,
			PeerId:    hostPeerID.String()}}

	// sign the data
	signature, err := p.node.signProtoMessage(req)
	if err != nil {
		log.Println("failed to sign pb data")
		return false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientLeaveChannel)
	if err != nil {
		log.Println(err)
		return false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return false
	}

	// store ref request so response handler has access to it
	//p.requests[req.MessageData.Id] = req
	//log.Printf("%s: Gravitation to: %s was sent. Message Id: %s", p.node.ID(), hostPeerID, req.MessageData.Id)
	return true

}
