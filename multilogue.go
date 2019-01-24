package main

import inet "github.com/libp2p/go-libp2p-net"

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
	history   []string // peer Ids of users who last spoke
	peers     []Peer   // slice of peer objects
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
	node *Node // local host
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

func (p *MultilogueProtocol) onClientSendMessage(s inet.Stream) {
}

func (p *MultilogueProtocol) onClientTransmissionStart(s inet.Stream) {
}

func (p *MultilogueProtocol) onClientTransmissionEnd(s inet.Stream) {
}

func (p *MultilogueProtocol) onHostAcceptTransmission(s inet.Stream) {
}

func (p *MultilogueProtocol) onHostDenyTransmission(s inet.Stream) {
}

func (p *MultilogueProtocol) onHostBroadcastMessage(s inet.Stream) {
}

func (p *MultilogueProtocol) onClientJoinChannel(s inet.Stream) {
}

func (p *MultilogueProtocol) onClientLeaveChannel(s inet.Stream) {
}

func (p *MultilogueProtocol) onHostAcceptClient(s inet.Stream) {
}

func (p *MultilogueProtocol) onHostDenyClient(s inet.Stream) {
}

// TODO: Design proper API
func (p *MultilogueProtocol) SendMessage() {
}

func (p *MultilogueProtocol) JoinChannel() {
}

func (p *MultilogueProtocol) ExitChannel() {
}
