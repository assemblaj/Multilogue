package main

// pattern: /protocol-name/request-or-response-message/version
const clientSendMessage = "/multilogue/clientsendmessage/0.0.1"
const clientTransmissionStart = "/multilogue/clienttransmissionstart/0.0.1"
const clientTransmissionEnd = "/multilogue/clienttransmissionend/0.0.1"
const hostAcceptTransmission = "/multilogue/hostaccepttransmission/0.0.1"
const hostDenyTransmission = "/multilogue/hostdenytransmission/0.0.1"
const hostBroadcastMessage = "/multilogue/hostbroadcastmessage/0.0.1"

// MultilogueProtocol type
type MultilogueProtocol struct {
	node *Node // local host
}

// NewMultilogueProtocol Create instance of protocol
// Might want a data object
func NewMultilogueProtocol(node *Node) *MultilogueProtocol {
	p := &MultilogueProtocol{
		node: node}

	//node.SetStreamHandler(gravitationRequest, p.onGravitationRequest)
	//node.SetStreamHandler(gravitationResponse, p.onGravitationResponse)
	return p
}
