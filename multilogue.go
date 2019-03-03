package main

import (
	"bufio"
	"context"
	"log"
	"os"
	"reflect"
	"sync"
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

type Message struct {
	peer      *Peer
	timestamp time.Time
	text      string
}

func NewMessage(peer *Peer, message string) *Message {
	return &Message{
		peer:      peer,
		timestamp: time.Now(),
		text:      message}
}

type SyncedMap struct {
	lock      sync.RWMutex
	values    map[string]interface{}
	valueType reflect.Type
}

func newSyncedMap(v interface{}) *SyncedMap {
	return &SyncedMap{
		values:    make(map[string]interface{}),
		valueType: reflect.TypeOf(v)}
}

func (t *SyncedMap) Get(key string) (interface{}, bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()
	value, found := t.values[key]
	return value, found
}
func (t *SyncedMap) Delete(key string) {
	t.lock.Lock()
	defer t.lock.Unlock()
	delete(t.values, key)
}

func (t *SyncedMap) Put(key string, value interface{}) {
	if reflect.TypeOf(value) == t.valueType {
		t.lock.Lock()
		defer t.lock.Unlock()
		t.values[key] = value
	}
}

func (t *SyncedMap) Range(f func(key, value interface{})) {
	t.lock.RLock()
	defer t.lock.RUnlock()
	for k, v := range t.values {
		t.lock.RUnlock()
		f(k, v)
		t.lock.RLock()
	}
}

type OutputSession struct {
	messageQueue chan *Message
	queueSize    int
}

func NewOutputSession(queueSize int) *OutputSession {
	os := new(OutputSession)
	os.queueSize = queueSize
	os.messageQueue = make(chan *Message, os.queueSize)
	return os
}

type Transmission struct {
	channelId string
	peer      *Peer
	totalMsgs int
	startTime time.Time
	done      chan bool // notifies timer when transmission is ended
	timeEnded bool
	sync.RWMutex
}

type Channel struct {
	channelId string
	history   []string   // peer Ids of users who last spoke
	peers     *SyncedMap // slice of peer objects
	// probably need a map of peerId to cooldown
	currentTransmission *Transmission
	output              *OutputSession
	config              *ChannelConfig
	sync.RWMutex
}

func NewChannel(channelId string, config *ChannelConfig) *Channel {
	if config == nil {
		config = DefaultChannelConfig()
	}

	c := &Channel{
		channelId: channelId,
		history:   []string{},
		peers:     newSyncedMap(&Peer{}),
		output:    NewOutputSession(1), //TODO: Replace with some default/config
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

// MultilogueProtocol type
type MultilogueProtocol struct {
	node     *Node      // local host
	channels *SyncedMap // channelId : *Channel
	requests *SyncedMap // used to access request data from response handlers
	debug    bool
	logger   *log.Logger
}

// NewMultilogueProtocol Create instance of protocol
// Might want a data object
func NewMultilogueProtocol(node *Node) *MultilogueProtocol {
	p := &MultilogueProtocol{
		node:     node,
		channels: newSyncedMap(&Channel{}),
		requests: newSyncedMap(&Request{}),
		debug:    true,
		logger:   initDebugLogger()}

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

func initDebugLogger() *log.Logger {
	timeStirng := time.Now().Format("1-2_2006_15-04")

	f, err := os.OpenFile("./log/log_"+timeStirng+".log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	//defer f.Close() // lets leave this to the os

	logger := log.New(f, "Debug:", log.Lshortfile)
	return logger
}

func (p *MultilogueProtocol) debugPrintln(v ...interface{}) {
	if p.debug {
		p.logger.Println(v...)
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
		p.debugPrintln(err)
		return
	}

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		p.debugPrintln("Failed to obtain Client Peer ID")
		return
	}

	// Protocol Logic
	accepted := false

	errorCode := GenericError
	var hasPeer bool
	var channel *Channel

	var currentChannelClient string
	var givenChannelClient string
	var remoteRequester string
	var sessionLength time.Duration
	var messageRatio float64

	value, exists := p.channels.Get(data.HostData.ChannelId)

	if !exists {
		p.debugPrintln("In onClientSendMessage: Denied because channel not found.")
		goto Response
	}

	channel = value.(*Channel)

	channel.currentTransmission.RLock()
	_, hasPeer = channel.peers.Get(clientPeerIDString)
	if !hasPeer {
		p.debugPrintln("In onClientSendMessage: Denied because peer not found.")
		goto Response
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
	channel.currentTransmission.RUnlock()

	accepted = true

Response:

	// generate response message
	// Returning separate messages based on accepted or not
	var resp proto.Message
	var respErr error

	if accepted {
		// Updating increment messages
		channel.currentTransmission.Lock()
		channel.currentTransmission.totalMsgs = channel.currentTransmission.totalMsgs + 1
		channel.currentTransmission.Unlock()

		//for peerID, currentPeer := range channel.peers {
		channel.peers.Range(func(key, value interface{}) {
			peerID := key
			currentPeer := value.(*Peer)

			resp = &p2p.HostBroadcastMessage{MessageData: p.node.NewMessageData(data.MessageData.Id, false),
				ClientData: data.ClientData,
				HostData:   data.HostData,
				Message:    data.Message}

			// sign the data
			signature, err := p.node.signProtoMessage(resp)
			if err != nil {
				p.debugPrintln("failed to sign response")
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
				p.debugPrintln(respErr)
				return
			}

			// send the response
			ok := p.node.sendProtoMessage(resp, s)
			if ok {
				p.debugPrintln("%s: Message from %s recieved.", s.Conn().LocalPeer().String(), peerID)
			}
		})
	} else {
		//peer := channel.peers[clientPeerIDString]

		resp = &p2p.HostDenyClient{MessageData: p.node.NewMessageData(data.MessageData.Id, false),
			ClientData: data.ClientData,
			HostData:   data.HostData,
			ErrorCode:  int32(errorCode)}

		// sign the data
		signature, err := p.node.signProtoMessage(resp)
		if err != nil {
			p.debugPrintln("failed to sign response")
			return
		}
		denyClientResp := resp.(*p2p.HostDenyClient)

		// add the signature to the message
		denyClientResp.MessageData.Sign = signature
		s, respErr = p.node.NewStream(context.Background(), s.Conn().RemotePeer(), hostDenyClient)

		if respErr != nil {
			p.debugPrintln(respErr)
			return
		}

		// send the response
		ok := p.node.sendProtoMessage(resp, s)
		if ok {
			p.debugPrintln("%s: Denying Message from %s.", s.Conn().LocalPeer().String(), s.Conn().RemotePeer().String())
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
		p.debugPrintln(err)
		return
	}

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		p.debugPrintln("Failed to obtain Client Peer ID")
		return
	}

	errorCode := GenericError

	// Protocol Logic
	accepted := true
	var channel *Channel

	value, exists := p.channels.Get(data.HostData.ChannelId)
	if exists {
		channel = value.(*Channel)

		value, hasPeer := channel.peers.Get(clientPeerIDString)
		var peer *Peer

		if !hasPeer {
			accepted = false
		} else {
			peer = value.(*Peer)
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
		value, _ := channel.peers.Get(clientPeerIDString)
		peer := value.(*Peer)

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
				p.debugPrintln("timer returning, transmission done")
				return
			case <-transmissionTimer.C:
				channel.currentTransmission.Lock()
				channel.currentTransmission.timeEnded = true
				channel.currentTransmission.Unlock()
			}
		}()

		resp = &p2p.HostAcceptTransmission{MessageData: p.node.NewMessageData(data.MessageData.Id, false),
			ClientData: data.ClientData,
			HostData:   data.HostData}

		// sign the data
		signature, err := p.node.signProtoMessage(resp)
		if err != nil {
			p.debugPrintln("failed to sign response")
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
			p.debugPrintln("failed to sign response")
			return
		}
		denyClientResp := resp.(*p2p.HostDenyClient)

		// add the signature to the message
		denyClientResp.MessageData.Sign = signature
		s, respErr = p.node.NewStream(context.Background(), s.Conn().RemotePeer(), hostDenyClient)
	}
	if respErr != nil {
		p.debugPrintln(respErr)
		return
	}

	// send the response
	ok := p.node.sendProtoMessage(resp, s)

	if ok {
		p.debugPrintln("%s: Transmisison attempted from  %s.", s.Conn().LocalPeer().String(), s.Conn().RemotePeer().String())
	}

}

// verify this and delete transmission
func (p *MultilogueProtocol) onClientTransmissionEnd(s inet.Stream) {
	// get request data
	data := &p2p.ClientTransmissionEnd{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		p.debugPrintln(err)
		return
	}

	p.debugPrintln("%s: User: %s (%s) ending transmission. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer())

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		p.debugPrintln("Failed to obtain Client Peer ID")
		return
	}

	// Protocol Logic
	// Leaving channel
	val, exists := p.channels.Get(data.HostData.ChannelId)
	if exists {
		channel := val.(*Channel)
		// remove request from map as we have processed it here
		_, hasPeer := channel.peers.Get(clientPeerIDString)
		if hasPeer {
			currentChannelClient := channel.currentTransmission.peer.peerID.String()
			givenChannelClient := clientPeerIDString
			remoteRequester := s.Conn().RemotePeer().String()
			if currentChannelClient == givenChannelClient && currentChannelClient == remoteRequester {
				channel.currentTransmission.Lock()
				channel.currentTransmission.done <- true
				channel.currentTransmission.Unlock()

				p.debugPrintln("timer returned, making current transmisison nil ")
				channel.Lock()
				channel.currentTransmission = nil
				channel.Unlock()
				p.debugPrintln(" current transmisison nil ", channel.currentTransmission)

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

	p.debugPrintln("%s: User: %s (%s) ended transmission. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer())

}

// add to channel
// deny if necesary
func (p *MultilogueProtocol) onClientJoinChannel(s inet.Stream) {
	// get request data
	data := &p2p.ClientJoinChannel{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		p.debugPrintln(err)
		return
	}

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		p.debugPrintln("Failed to obtain Client Peer ID")
		return
	}

	// Protocol Logic
	// Adding to channel
	accepted := false
	val, exists := p.channels.Get(data.HostData.ChannelId)
	if exists {
		channel := val.(*Channel)
		// Accept if the peerId and username aren't already in the channel
		_, hasPeer := channel.peers.Get(clientPeerIDString)
		if !hasPeer {
			accepted = true
			channel.peers.Range(func(key, value interface{}) {
				peer := value.(*Peer)

				//for _, peer := range channel.peers {
				if data.ClientData.Username == peer.username {
					accepted = false
				}
			})
		}
		if accepted {
			channel.peers.Put(clientPeerIDString, &Peer{
				peerID:   clientPeerID,
				username: data.ClientData.Username,
			})
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
			p.debugPrintln("failed to sign response")
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
			p.debugPrintln("failed to sign response")
			return
		}
		denyClientResp := resp.(*p2p.HostDenyClient)

		// add the signature to the message
		denyClientResp.MessageData.Sign = signature
		s, respErr = p.node.NewStream(context.Background(), s.Conn().RemotePeer(), hostDenyClient)
	}
	if respErr != nil {
		p.debugPrintln(respErr)
		return
	}

	// send the response
	ok := p.node.sendProtoMessage(resp, s)

	if ok {
		p.debugPrintln("%s: Join Channel response to %s sent.", s.Conn().LocalPeer().String(), s.Conn().RemotePeer().String())
	}

}

// delete form channel
func (p *MultilogueProtocol) onClientLeaveChannel(s inet.Stream) {
	// get request data
	data := &p2p.ClientLeaveChannel{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		p.debugPrintln(err)
		return
	}

	p.debugPrintln("%s: User: %s (%s) Leaving Channel %s. (Client)", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		p.debugPrintln("Failed to obtain Client Peer ID")
		return
	}

	// Protocol Logic
	// Leaving channel
	val, exists := p.channels.Get(data.HostData.ChannelId)
	if exists {
		channel := val.(*Channel)
		// remove request from map as we have processed it here
		_, hasPeer := channel.peers.Get(clientPeerIDString)
		if hasPeer {
			channel.peers.Delete(clientPeerIDString)
		}
	}

	p.debugPrintln("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

// Handled when Client
// Start sending messages
func (p *MultilogueProtocol) onHostAcceptTransmission(s inet.Stream) {
	// get request data
	data := &p2p.HostAcceptTransmission{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		p.debugPrintln(err)
		return
	}

	p.debugPrintln("%s: User: %s (%s) Transmission Accepted from host on channel %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		p.debugPrintln("Failed to obtain Client Peer ID")
		return
	}

	// If there's no request, then simply print an error and return
	val, requestExists := p.requests.Get(data.MessageData.Id)
	if !requestExists {
		p.debugPrintln("Request not found ")
		return
	}
	request := val.(*Request)

	// Protocol Logic
	// Leaving channel
	val, channelExists := p.channels.Get(data.HostData.ChannelId)

	if !channelExists {
		p.debugPrintln("In onHostAcceptTransmission: Denied because channel doesn't exist")
		request.fail <- NewResponse(GenericError)
		return
	}
	channel := val.(*Channel)

	_, hasPeer := channel.peers.Get(clientPeerIDString)
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
	}

	p.debugPrintln("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

func (p *MultilogueProtocol) onHostDenyTransmission(s inet.Stream) {
	// get request data
	data := &p2p.HostDenyTransmission{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		p.debugPrintln(err)
		return
	}

	p.debugPrintln("%s: User: %s (%s) Transmision denied on channel %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		p.debugPrintln("Failed to obtain Client Peer ID")
		return
	}

	// If there's no request, then simply print an error and return
	val, requestExists := p.requests.Get(data.MessageData.Id)
	if !requestExists {
		p.debugPrintln("Request not found ")
		return
	}
	request := val.(*Request)

	val, channelExists := p.channels.Get(data.HostData.ChannelId)
	if !channelExists {
		request.fail <- NewResponse(GenericError)
		return
	}
	channel := val.(*Channel)

	_, hasPeer := channel.peers.Get(clientPeerIDString)
	if !hasPeer {
		request.fail <- NewResponse(GenericError)
		return
	}
	request.fail <- NewResponse(ProtocolErrorState(data.ErrorCode))

	p.channels.Delete(data.HostData.ChannelId)
	p.debugPrintln("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

// Start reading messages
func (p *MultilogueProtocol) onHostBroadcastMessage(s inet.Stream) {
	// get request data
	data := &p2p.HostBroadcastMessage{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		p.debugPrintln(err)
		return
	}

	clientPeerId := p.node.ID().String()

	p.debugPrintln("%s: User: %s (%s) Message being broadcasted on channel  %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}

	clientPeerID, err := peer.IDFromBytes(data.ClientData.PeerId)
	clientPeerIDString := clientPeerID.String()
	if err != nil {
		p.debugPrintln("Failed to obtain Client Peer ID")
		return
	}

	val, requestExists := p.requests.Get(data.MessageData.Id)
	if !requestExists {
		p.debugPrintln("Request not found ")
		return
	}
	request := val.(*Request)

	// Protocol Logic
	// Leaving channel
	value, exists := p.channels.Get(data.HostData.ChannelId)
	if exists {
		channel := value.(*Channel)
		val, hasPeer := channel.peers.Get(clientPeerIDString)
		peer := val.(*Peer)
		if hasPeer {
			if clientPeerId == clientPeerIDString {
				request.success <- NewResponse(NoError)
			}
			if channel.output == nil {
				channel.output = NewOutputSession(1) //TODO: Replace with some default/config
			}
			// Alert the UI that a new message has been recieved in the channel
			channel.output.messageQueue <- NewMessage(peer, data.Message)
		}
	}

	p.debugPrintln("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

// TODO: Create Channel etc on the cleint side
// As well as InputSession
func (p *MultilogueProtocol) onHostAcceptClient(s inet.Stream) {
	// get request data
	data := &p2p.HostAcceptClient{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		p.debugPrintln(err)
		return
	}

	p.debugPrintln("%s: User: %s (%s) Joining Channel %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}

	_, channelExists := p.channels.Get(data.HostData.ChannelId)
	value, requestExists := p.requests.Get(data.MessageData.Id)

	// Update request data
	if requestExists {
		request := value.(*Request)
		if channelExists {
			request.success <- NewResponse(NoError)
		} else {
			request.fail <- NewResponse(GenericError)
		}
	} else {
		p.debugPrintln("Failed to locate request data boject for response")
		return
	}

	p.debugPrintln("%s: User: %s (%s) Joined Channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)
}

func (p *MultilogueProtocol) onHostDenyClient(s inet.Stream) {
	// get request data
	data := &p2p.HostDenyClient{}
	decoder := protobufCodec.Multicodec(nil).Decoder(bufio.NewReader(s))
	err := decoder.Decode(data)
	if err != nil {
		p.debugPrintln(err)
		return
	}

	p.debugPrintln("%s: User: %s (%s) Client denied of channel %s. ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

	valid := p.node.authenticateMessage(data, data.MessageData)

	if !valid {
		p.debugPrintln("Failed to authenticate message")
		return
	}
	// If there's no request, then simply print an error and return
	value, requestExists := p.requests.Get(data.MessageData.Id)
	request := value.(*Request)
	if !requestExists {
		p.debugPrintln("Request not found ")
		return
	}

	// Update request data
	request.fail <- NewResponse(ProtocolErrorState(data.ErrorCode))

	p.debugPrintln("%s: User: %s (%s) Left channel %s ", s.Conn().LocalPeer(), data.ClientData.Username, s.Conn().RemotePeer(), data.HostData.ChannelId)

}

// TODO: Design proper API
func (p *MultilogueProtocol) SendMessage(clientPeer *Peer, hostPeerID peer.ID, channelId string, message string) (*Request, bool) {
	p.debugPrintln("%s: Sending message to %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

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
		p.debugPrintln("failed to sign pb data: ", err)
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientSendMessage)
	if err != nil {
		p.debugPrintln(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Add request to request list
	request := NewRequest()
	p.requests.Put(req.MessageData.Id, request)

	return request, true
}

func (p *MultilogueProtocol) SendTransmissionRequest(clientPeer *Peer, hostPeerID peer.ID, channelId string) (*Request, bool) {
	p.debugPrintln("%s: Sending request to %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

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
		p.debugPrintln("failed to sign pb data")
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientTransmissionStart)
	if err != nil {
		p.debugPrintln(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Add request to request list
	request := NewRequest()
	p.requests.Put(req.MessageData.Id, request)

	return request, true

}

// Creates channnel and adds host to room
func (p *MultilogueProtocol) CreateChannel(clientPeer *Peer, channelId string, config *ChannelConfig) {
	// Creating Channel Obj
	_, exists := p.channels.Get(channelId)
	// remove the channel
	if !exists {
		channel := NewChannel(channelId, config)
		channel.peers.Put(clientPeer.peerID.String(), clientPeer)
		p.channels.Put(channelId, channel)
		p.debugPrintln("CreateChannel: Channel created ")
	} else {
		p.debugPrintln("CreateChannel: Channel already exists. ")
	}
}

func (p *MultilogueProtocol) DeleteChannel(channelId string) {
	_, exists := p.channels.Get(channelId)
	if exists {
		p.channels.Delete(channelId)
	}
}

func (p *MultilogueProtocol) JoinChannel(clientPeer *Peer, hostPeerID peer.ID, channelId string) (*Request, bool) {
	p.debugPrintln("%s: Joining %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

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
		p.debugPrintln("failed to sign pb data: ", err)
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientJoinChannel)
	if err != nil {
		p.debugPrintln(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Creating Channel Obj
	channel := NewChannel(channelId, nil)
	channel.peers.Put(clientPeer.peerID.String(), clientPeer)
	p.channels.Put(channelId, channel)

	// Add request to request list
	request := NewRequest()
	p.requests.Put(req.MessageData.Id, request)

	return request, true
}

func (p *MultilogueProtocol) LeaveChannel(clientPeer *Peer, hostPeerID peer.ID, channelId string) (*Request, bool) {
	p.debugPrintln("%s: Leaving %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

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
		p.debugPrintln("failed to sign pb data")
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientLeaveChannel)
	if err != nil {
		p.debugPrintln(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Protocol logic
	_, exists := p.channels.Get(channelId)
	// remove the channel
	if exists {
		p.channels.Delete(channelId)
	}

	// Add request to request list
	request := NewRequest()
	p.requests.Put(req.MessageData.Id, request)

	return request, true
}

func (p *MultilogueProtocol) EndTransmission(clientPeer *Peer, hostPeerID peer.ID, channelId string) (*Request, bool) {
	p.debugPrintln("%s: Ending transmission %s Channel : %s....", p.node.ID(), hostPeerID, channelId)

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
		p.debugPrintln("failed to sign pb data")
		return nil, false
	}

	// add the signature to the message
	req.MessageData.Sign = signature

	s, err := p.node.NewStream(context.Background(), hostPeerID, clientTransmissionEnd)
	if err != nil {
		p.debugPrintln(err)
		return nil, false
	}

	ok := p.node.sendProtoMessage(req, s)

	if !ok {
		return nil, false
	}

	// Add request to request list
	request := NewRequest()
	p.requests.Put(req.MessageData.Id, request)

	return request, true
}
