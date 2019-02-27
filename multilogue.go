package main

import (
	"bufio"
	"context"
	"log"
	"time"

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
	peerID           peer.ID
	username         string
	lastMessage      time.Time // unix timestamp of last message
	lastTransmission time.Time // unix timestamp of last transmission
}

type JoinState struct {
	accepted chan bool
	denied   chan bool
}

func NewJoinState() *JoinState {
	es := new(JoinState)
	es.accepted = make(chan bool)
	es.denied = make(chan bool)
	return es
}

type InputSession struct {
	start   chan bool
	stop    chan bool
	current bool
	total   int
}

func NewInputSession() *InputSession {
	is := new(InputSession)
	is.start = make(chan bool)
	is.stop = make(chan bool)
	return is
}

type OutputSession struct {
	messageQueue chan string
	queueSize    int
}

func NewOutputSession(queueSize int) *OutputSession {
	os := new(OutputSession)
	os.queueSize = queueSize
	os.messageQueue = make(chan string, os.queueSize)
	return os
}

type Transmission struct {
	channelId string
	peer      *Peer
	totalMsgs int
	startTime time.Time
	done      chan bool // notifies timer when transmission is ended
	timeEnded bool
}

type Channel struct {
	channelId string
	history   []string         // peer Ids of users who last spoke
	peers     map[string]*Peer // slice of peer objects
	// probably need a map of peerId to cooldown
	currentTransmission *Transmission
	output              *OutputSession
	input               *InputSession
	join                *JoinState
	config              *ChannelConfig
}

func NewChannel(channelId string, config *ChannelConfig) *Channel {
	if config == nil {
		config = DefaultChannelConfig()
	}

	c := &Channel{
		channelId: channelId,
		history:   []string{},
		peers:     make(map[string]*Peer),
		output:    NewOutputSession(1), //TODO: Replace with some default/config
		input:     NewInputSession(),
		join:      NewJoinState(),
		config:    config}
	return c
}

// Protocol Error States
type ProtocolErrorState int32

const (
	NoError           ProtocolErrorState = 100
	GenericError      ProtocolErrorState = 200
	MessageLimitError ProtocolErrorState = 210
	TimeLimitError    ProtocolErrorState = 220
	CooldownError     ProtocolErrorState = 230
	HistoryError      ProtocolErrorState = 240
	RatioError        ProtocolErrorState = 250
)

// Prorbably want useful statistics in this eventually too
type Response struct {
	errorCode ProtocolErrorState
}

func NewResponse(errorCode ProtocolErrorState) *Response {
	return &Response{
		errorCode: errorCode}
}

type Request struct {
	success chan *Response
	fail    chan *Response
}

func NewRequest() *Request {
	return &Request{
		success: make(chan *Response),
		fail:    make(chan *Response)}
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
	requests map[string]*Request // used to access request data from response handlers
	debug    bool
}

// NewMultilogueProtocol Create instance of protocol
// Might want a data object
func NewMultilogueProtocol(node *Node) *MultilogueProtocol {
	p := &MultilogueProtocol{
		node:     node,
		channels: make(map[string]*Channel),
		requests: make(map[string]*Request),
		debug:    true}

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

func (p *MultilogueProtocol) debugPrintln(v ...interface{}) {
	if p.debug {
		log.Println(v...)
	}
}

// Handled when hosting
// Validate and broadcast this message to all peers
// deny if necesary
func (p *MultilogueProtocol) onClientSendMessage(s inet.Stream) {
	p.debugPrintln("In onClientSendMessage: Message accepted.")

	// get request data
	data := &p2p.ClientSendMessage{}
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

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		log.Println("Failed to obtain Client Peer ID")
		return
	}

	// Protocol Logic
	accepted := false

	errorCode := GenericError

	var currentChannelClient string
	var givenChannelClient string
	var remoteRequester string
	var sessionLength time.Duration
	var messageRatio float64

	channel, exists := p.channels[data.HostData.ChannelId]
	if !exists {
		_, hasPeer := channel.peers[clientPeerIDString]
		if !hasPeer {
			p.debugPrintln("In onClientSendMessage: Denied because peer not found.")
			goto Response
		}
	}

	if channel.currentTransmission == nil {
		p.debugPrintln("In onClientSendMessage: Denied because transmission doesn't exist.")
		goto Response
	}

	// valid peer id
	currentChannelClient = channel.currentTransmission.peer.peerID.String()
	givenChannelClient = clientPeerIDString
	remoteRequester = s.Conn().RemotePeer().String()
	if !(currentChannelClient == givenChannelClient && currentChannelClient == remoteRequester) {
		p.debugPrintln("In onClientSendMessage: Denied because transmisison peer id, given peer id and remote peer id are not equal.")
		p.debugPrintln(currentChannelClient, " ", givenChannelClient, " ", remoteRequester)
		goto Response
	}

	if channel.currentTransmission.totalMsgs+1 > channel.config.MessageLimit {
		p.debugPrintln("In onClientSendMessage: Denied because meessage limit reached.")
		errorCode = MessageLimitError
		goto Response
	}

	sessionLength = time.Now().Sub(channel.currentTransmission.startTime)
	messageRatio = float64(channel.currentTransmission.totalMsgs) / sessionLength.Seconds()
	if messageRatio > channel.config.MaxMessageRatio {
		p.debugPrintln("In onClientSendMessage: Denied because message ratio passed.")
		errorCode = RatioError
		goto Response
	}

	if channel.currentTransmission.timeEnded {
		p.debugPrintln("In onClientSendMessage: Denied time limit for transmission passed.")
		errorCode = TimeLimitError
		goto Response
	}

	accepted = true

Response:

	// generate response message
	// Returning separate messages based on accepted or not
	var resp proto.Message
	var respErr error

	if accepted {
		// Updating increment messages
		channel.currentTransmission.totalMsgs = channel.currentTransmission.totalMsgs + 1

		for peerID, currentPeer := range channel.peers {
			resp = &p2p.HostBroadcastMessage{MessageData: p.node.NewMessageData(data.MessageData.Id, false),
				ClientData: data.ClientData,
				HostData:   data.HostData,
				Message:    data.Message}

			// sign the data
			signature, err := p.node.signProtoMessage(resp)
			if err != nil {
				log.Println("failed to sign response")
				return
			}

			// Cannot take the above code outside of the if because I need to do the following
			broadcastResp := resp.(*p2p.HostBroadcastMessage)

			// add the signature to the message
			broadcastResp.MessageData.Sign = signature

			// Have to use the constant for the protocol message name string because reasons
			// So that had to be done within this if
			s, respErr = p.node.NewStream(context.Background(), currentPeer.peerID, hostBroadcastMessage)

			if respErr != nil {
				log.Println(respErr)
				return
			}

			// send the response
			ok := p.node.sendProtoMessage(resp, s)
			if ok {
				log.Printf("%s: Message from %s recieved.", s.Conn().LocalPeer().String(), peerID)
			}
		}
	} else {
		//peer := channel.peers[clientPeerIDString]

		resp = &p2p.HostDenyClient{MessageData: p.node.NewMessageData(data.MessageData.Id, false),
			ClientData: data.ClientData,
			HostData:   data.HostData,
			ErrorCode:  int32(errorCode)}

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

		if respErr != nil {
			log.Println(respErr)
			return
		}

		// send the response
		ok := p.node.sendProtoMessage(resp, s)
		if ok {
			log.Printf("%s: Denying Message from %s.", s.Conn().LocalPeer().String(), s.Conn().RemotePeer().String())
		}
	}
}

// verify this and set new transmission
// deny if necesary
func (p *MultilogueProtocol) onClientTransmissionStart(s inet.Stream) {
	// get request data
	data := &p2p.ClientTransmissionStart{}
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

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		log.Println("Failed to obtain Client Peer ID")
		return
	}

	errorCode := GenericError

	// Protocol Logic
	accepted := true
	channel, exists := p.channels[data.HostData.ChannelId]
	if exists {
		peer, hasPeer := channel.peers[clientPeerIDString]
		if !hasPeer {
			accepted = false
		}
		if channel.currentTransmission != nil {
			accepted = false
		}

		// If its in the last X transmissions
		if channel.config.HistorySize > 0 { // last 0 would equal the entire array
			var currentHistory []string
			end := len(channel.history)
			start := end - channel.config.HistorySize

			if start < 0 {
				currentHistory = channel.history[0:end]
			} else {
				currentHistory = channel.history[start:end]
			}

			for _, peerID := range currentHistory {
				if clientPeerIDString == peerID {
					accepted = false
					errorCode = HistoryError
				}
			}
		}

		if !peer.lastTransmission.IsZero() {
			p.debugPrintln("In onClientTransmissionStart, Debugging cooldown ")
			p.debugPrintln("Time since last transmission: ", time.Since(peer.lastTransmission))
			p.debugPrintln("Cool down period: ", time.Duration(channel.config.CooldownPeriod)*time.Second)
			if time.Since(peer.lastTransmission) < time.Duration(channel.config.CooldownPeriod)*time.Second {
				p.debugPrintln("Transmission denied, sent within cooldown period.")
				accepted = false
				errorCode = CooldownError
			}
		}
	} else {
		accepted = false
	}

	// generate response message
	// Returning separate messages based on accepted or not
	var resp proto.Message
	var respErr error

	if accepted {
		channel.history = append(channel.history, clientPeerIDString)
		peer := channel.peers[clientPeerIDString]

		peer.lastTransmission = time.Now()

		// Create new transmission
		channel.currentTransmission = &Transmission{
			channelId: data.HostData.ChannelId,
			peer:      peer,
			startTime: time.Now(),
			done:      make(chan bool)}

		// Time limit handling
		go func() {
			timeLimit := time.Duration(channel.config.TimeLimit) * time.Second
			transmissionTimer := time.NewTimer(timeLimit)

			select {
			case <-channel.currentTransmission.done:
				log.Println("timer returning, transmission done")
				return
			case <-transmissionTimer.C:
				channel.currentTransmission.timeEnded = true
			}
		}()

		resp = &p2p.HostAcceptTransmission{MessageData: p.node.NewMessageData(data.MessageData.Id, false),
			ClientData: data.ClientData,
			HostData:   data.HostData}

		// sign the data
		signature, err := p.node.signProtoMessage(resp)
		if err != nil {
			log.Println("failed to sign response")
			return
		}

		// Cannot take the above code outside of the if because I need to do the following
		acceptClientResp := resp.(*p2p.HostAcceptTransmission)

		// add the signature to the message
		acceptClientResp.MessageData.Sign = signature

		// Have to use the constant for the protocol message name string because reasons
		// So that had to be done within this if
		s, respErr = p.node.NewStream(context.Background(), s.Conn().RemotePeer(), hostAcceptTransmission)
	} else {
		resp = &p2p.HostDenyClient{MessageData: p.node.NewMessageData(data.MessageData.Id, false),
			ClientData: data.ClientData,
			HostData:   data.HostData,
			ErrorCode:  int32(errorCode)}

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
		log.Printf("%s: Transmisison attempted from  %s.", s.Conn().LocalPeer().String(), s.Conn().RemotePeer().String())
	}

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

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		log.Println("Failed to obtain Client Peer ID")
		return
	}

	// Protocol Logic
	// Leaving channel
	channel, exists := p.channels[data.HostData.ChannelId]
	if exists {
		// remove request from map as we have processed it here
		_, hasPeer := channel.peers[clientPeerIDString]
		if hasPeer {
			currentChannelClient := channel.currentTransmission.peer.peerID.String()
			givenChannelClient := clientPeerIDString
			remoteRequester := s.Conn().RemotePeer().String()
			if currentChannelClient == givenChannelClient && currentChannelClient == remoteRequester {
				channel.currentTransmission.done <- true

				log.Println("timer returned, making current transmisison nil ")

				channel.currentTransmission = nil
				log.Println(" current transmisison nil ", channel.currentTransmission)

			} else {
				p.debugPrintln("In onClientTransmissionEnd: currentChannelClient, givenChannelClient , remoteRequester  Not the same.")
				p.debugPrintln("In onClientTransmissionEnd: ", currentChannelClient, " ", givenChannelClient, " ", remoteRequester)
			}
		} else {
			p.debugPrintln("In onClientTransmissionEnd: Peer not found .")
		}
	} else {
		p.debugPrintln("In onClientTransmissionEnd: Channel not found .")

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

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		log.Println("Failed to obtain Client Peer ID")
		return
	}

	// Protocol Logic
	// Adding to channel
	accepted := false
	channel, exists := p.channels[data.HostData.ChannelId]
	if exists {
		// Accept if the peerId and username aren't already in the channel
		_, hasPeer := channel.peers[clientPeerIDString]
		if !hasPeer {
			accepted = true
			for _, peer := range channel.peers {
				if data.ClientData.Username == peer.username {
					accepted = false
				}
			}
		}
		if accepted {
			channel.peers[clientPeerIDString] = &Peer{
				peerID:   clientPeerID,
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

	log.Printf("%s: User: %s (%s) Leaving Channel %s. (Client)", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		log.Println("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		log.Println("Failed to obtain Client Peer ID")
		return
	}

	// Protocol Logic
	// Leaving channel
	channel, exists := p.channels[data.HostData.ChannelId]
	if exists {
		// remove request from map as we have processed it here
		_, hasPeer := channel.peers[clientPeerIDString]
		if hasPeer {
			delete(channel.peers, clientPeerIDString)
		}
	}

	log.Printf("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

// Handled when Client
// Start sending messages
func (p *MultilogueProtocol) onHostAcceptTransmission(s inet.Stream) {
	// get request data
	data := &p2p.HostAcceptTransmission{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("%s: User: %s (%s) Transmission Accepted from host on channel %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		log.Println("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		log.Println("Failed to obtain Client Peer ID")
		return
	}

	// If there's no request, then simply print an error and return
	request, requestExists := p.requests[data.MessageData.Id]
	if !requestExists {
		log.Println("Request not found ")
		return
	}

	// Protocol Logic
	// Leaving channel
	channel, channelExists := p.channels[data.HostData.ChannelId]

	if !channelExists {
		p.debugPrintln("In onHostAcceptTransmission: Denied because channel doesn't exist")
		request.fail <- NewResponse(GenericError)
		return
	}

	_, hasPeer := channel.peers[clientPeerIDString]
	if !hasPeer {
		p.debugPrintln("In onHostAcceptTransmission: Denied because peer doesn't exist: ", data.ClientData.PeerId)
		request.fail <- NewResponse(GenericError)
		return
	}
	p.debugPrintln("In onHostAcceptTransmission: Success")

	request.success <- NewResponse(NoError)

	// remove request from map as we have processed it here
	if channel.currentTransmission == nil {
		channel.currentTransmission = &Transmission{}
		channel.input = NewInputSession()
		p.debugPrintln("In onHostAcceptTransmission: Input Session starting ")

		// Alert the UI that the client is the speaker
		channel.input.current = true
		channel.input.start <- true
	}

	log.Printf("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

func (p *MultilogueProtocol) onHostDenyTransmission(s inet.Stream) {
	// get request data
	data := &p2p.HostDenyTransmission{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("%s: User: %s (%s) Transmision denied on channel %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		log.Println("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		log.Println("Failed to obtain Client Peer ID")
		return
	}

	// If there's no request, then simply print an error and return
	request, requestExists := p.requests[data.MessageData.Id]
	if !requestExists {
		log.Println("Request not found ")
		return
	}

	channel, channelExists := p.channels[data.HostData.ChannelId]
	if !channelExists {
		request.fail <- NewResponse(GenericError)
		return
	}

	_, hasPeer := channel.peers[clientPeerIDString]
	if !hasPeer {
		request.fail <- NewResponse(GenericError)
		return
	}

	// Alert that input has been
	channel.input.stop <- true

	// After UI has acknowledged the above message, delete the channel
	channel.input.stop <- true
	delete(p.channels, data.HostData.ChannelId)

	log.Printf("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

// Start reading messages
func (p *MultilogueProtocol) onHostBroadcastMessage(s inet.Stream) {
	// get request data
	data := &p2p.HostBroadcastMessage{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		log.Println(err)
		return
	}

	clientPeerId := p.node.ID().String()

	log.Printf("%s: User: %s (%s) Message being broadcasted on channel  %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		log.Println("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		log.Println("Failed to obtain Client Peer ID")
		return
	}

	request, requestExists := p.requests[data.MessageData.Id]
	if !requestExists {
		log.Println("Request not found ")
		return
	}
	// Protocol Logic
	// Leaving channel
	channel, exists := p.channels[data.HostData.ChannelId]
	if exists {
		_, hasPeer := channel.peers[clientPeerIDString]
		if hasPeer {
			if clientPeerId == clientPeerIDString {
				request.success <- NewResponse(NoError)
			}
			if channel.output == nil {
				channel.output = NewOutputSession(1) //TODO: Replace with some default/config
			}
			// Alert the UI that a new message has been recieved in the channel
			channel.output.messageQueue <- data.Message
		}
	}

	log.Printf("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

// TODO: Create Channel etc on the cleint side
// As well as InputSession
func (p *MultilogueProtocol) onHostAcceptClient(s inet.Stream) {
	// get request data
	data := &p2p.HostAcceptClient{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("%s: User: %s (%s) Joining Channel %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		log.Println("Failed to authenticate message")
		return
	}

	_, channelExists := p.channels[data.HostData.ChannelId]
	request, requestExists := p.requests[data.MessageData.Id]

	// Update request data
	if requestExists {
		if channelExists {
			request.success <- NewResponse(NoError)
		} else {
			request.fail <- NewResponse(GenericError)
		}
	} else {
		log.Println("Failed to locate request data boject for response")
		return
	}

	log.Printf("%s: User: %s (%s) Joined Channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)
}

func (p *MultilogueProtocol) onHostDenyClient(s inet.Stream) {
	// get request data
	data := &p2p.HostDenyClient{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("%s: User: %s (%s) Client denied of channel %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		log.Println("Failed to authenticate message")
		return
	}
	// If there's no request, then simply print an error and return
	request, requestExists := p.requests[data.MessageData.Id]
	if !requestExists {
		log.Println("Request not found ")
		return
	}

	// Update request data
	request.fail <- NewResponse(ProtocolErrorState(data.ErrorCode))

	log.Printf("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

// TODO: Design proper API
func (p *MultilogueProtocol) SendMessage(clientPeer *Peer, hostPeerID peer.ID, channelId string, message string) (*Request, bool) {
	log.Printf("%s: Sending message to %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

	// create message data
	req := &p2p.ClientSendMessage{
		MessageData: p.node.NewMessageData(uuid.New().String(), false),
		ClientData: &p2p.ClientData{
			PeerId:   []byte(string(clientPeer.peerID)),
			Username: clientPeer.username},
		HostData: &p2p.HostData{
			ChannelId: channelId,
			PeerId:    []byte(string(hostPeerID))},
		Message: message}

	// sign the data
	signature, err := p.node.signProtoMessage(req)
	if err != nil {
		log.Println("failed to sign pb data: ", err)
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientSendMessage)
	if err != nil {
		log.Println(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Add request to request list
	p.requests[req.MessageData.Id] = NewRequest()
	return p.requests[req.MessageData.Id], true
}

func (p *MultilogueProtocol) SendTransmissionRequest(clientPeer *Peer, hostPeerID peer.ID, channelId string) (*Request, bool) {
	log.Printf("%s: Sending request to %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

	// create message data
	req := &p2p.ClientTransmissionStart{
		MessageData: p.node.NewMessageData(uuid.New().String(), false),
		ClientData: &p2p.ClientData{
			PeerId:   []byte(string(clientPeer.peerID)),
			Username: clientPeer.username},
		HostData: &p2p.HostData{
			ChannelId: channelId,
			PeerId:    []byte(string(hostPeerID))}}

	// sign the data
	signature, err := p.node.signProtoMessage(req)
	if err != nil {
		log.Println("failed to sign pb data")
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientTransmissionStart)
	if err != nil {
		log.Println(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Add request to request list
	p.requests[req.MessageData.Id] = NewRequest()

	return p.requests[req.MessageData.Id], true

}

func (p *MultilogueProtocol) CreateChannel(channelId string, config *ChannelConfig) {
	// Creating Channel Obj
	_, exists := p.channels[channelId]
	// remove the channel
	if !exists {
		p.channels[channelId] = NewChannel(channelId, config)
	}
}

func (p *MultilogueProtocol) DeleteChannel(channelId string) {
	_, exists := p.channels[channelId]
	if exists {
		delete(p.channels, channelId)
	}
}

func (p *MultilogueProtocol) JoinChannel(clientPeer *Peer, hostPeerID peer.ID, channelId string) (*Request, bool) {
	log.Printf("%s: Joining %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

	// create message data
	req := &p2p.ClientJoinChannel{
		MessageData: p.node.NewMessageData(uuid.New().String(), false),
		ClientData: &p2p.ClientData{
			PeerId:   []byte(string(clientPeer.peerID)),
			Username: clientPeer.username},
		HostData: &p2p.HostData{
			ChannelId: channelId,
			PeerId:    []byte(string(hostPeerID))}}

	// sign the data
	signature, err := p.node.signProtoMessage(req)
	if err != nil {
		log.Println("failed to sign pb data: ", err)
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientJoinChannel)
	if err != nil {
		log.Println(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Creating Channel Obj
	p.channels[channelId] = NewChannel(channelId, nil)
	p.channels[channelId].peers[clientPeer.peerID.String()] = clientPeer

	// Add request to request list
	p.requests[req.MessageData.Id] = NewRequest()

	return p.requests[req.MessageData.Id], true
}

func (p *MultilogueProtocol) LeaveChannel(clientPeer *Peer, hostPeerID peer.ID, channelId string) (*Request, bool) {
	log.Printf("%s: Leaving %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

	// create message data
	req := &p2p.ClientLeaveChannel{
		MessageData: p.node.NewMessageData(uuid.New().String(), false),
		ClientData: &p2p.ClientData{
			PeerId:   []byte(string(clientPeer.peerID)),
			Username: clientPeer.username},
		HostData: &p2p.HostData{
			ChannelId: channelId,
			PeerId:    []byte(string(hostPeerID))}}

	// sign the data
	signature, err := p.node.signProtoMessage(req)
	if err != nil {
		log.Println("failed to sign pb data")
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientLeaveChannel)
	if err != nil {
		log.Println(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Protocol logic
	_, exists := p.channels[channelId]
	// remove the channel
	if exists {
		delete(p.channels, channelId)
	}

	// Add request to request list
	p.requests[req.MessageData.Id] = NewRequest()

	return p.requests[req.MessageData.Id], true
}

func (p *MultilogueProtocol) EndTransmission(clientPeer *Peer, hostPeerID peer.ID, channelId string) (*Request, bool) {
	log.Printf("%s: Ending transmission %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

	// create message data
	req := &p2p.ClientTransmissionEnd{
		MessageData: p.node.NewMessageData(uuid.New().String(), false),
		ClientData: &p2p.ClientData{
			PeerId:   []byte(string(clientPeer.peerID)),
			Username: clientPeer.username},
		HostData: &p2p.HostData{
			ChannelId: channelId,
			PeerId:    []byte(string(hostPeerID))}}

	// sign the data
	signature, err := p.node.signProtoMessage(req)
	if err != nil {
		log.Println("failed to sign pb data")
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientTransmissionEnd)
	if err != nil {
		log.Println(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Add request to request list
	p.requests[req.MessageData.Id] = NewRequest()
	return p.requests[req.MessageData.Id], true
}
