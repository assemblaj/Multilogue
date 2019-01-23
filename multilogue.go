package main

import inet "github.com/libp2p/go-libp2p-net"

// pattern: /protocol-name/request-or-response-message/version
const clientSendMessage = "/multilogue/clientsendmessage/0.0.1"
const clientTransmissionStart = "/multilogue/clienttransmissionstart/0.0.1"
const clientTransmissionEnd = "/multilogue/clienttransmissionend/0.0.1"
const hostAcceptTransmission = "/multilogue/hostaccepttransmission/0.0.1"
const hostDenyTransmission = "/multilogue/hostdenytransmission/0.0.1"
const hostBroadcastMessage = "/multilogue/hostbroadcastmessage/0.0.1"

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

	node.SetStreamHandler(clientSendMessage, p.onClientSendMessage)
	node.SetStreamHandler(clientTransmissionStart, p.onClientTransmissionStart)
	node.SetStreamHandler(clientTransmissionEnd, p.onClientTransmissionEnd)
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
