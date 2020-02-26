# Multilogue
Multilogue: non-simultaneous p2p chat protocol  


## Summary: 
- One user can speak in a channel at a time. 
- Users must request a transmission before starting their turn. 
- After certain conditions are met (time limit, message limit, etc), the user's turn ends.
- The user can also voluntarily end their turn. 
- After each users turn, they are subject to a cooldown in which their requests are deprioritized. 

## How to use: 
Clone the project
make deps 
go build 

./Multilogue  -user {username} -channel {channel} : Create and join new channel.

Multiadress listed on title bar. 

./Multilogue  -user {username} -channel {channel} -host {multiaddress} : Join channel at that specific multiadress 

## Controls: 
[Insert] : Start turn   
[End]  : End Turn   
[CTRL+C] : Exit program    

## Multilogue Protocol Functions: 
```
// Host functions
CreateChannel(clientPeer *Peer, channelId string, config *ChannelConfig)

// Client functions
JoinChannel(clientPeer *Peer, hostPeerID peer.ID, channelId string) 
LeaveChannel(clientPeer *Peer, hostPeerID peer.ID, channelId string)

SendTransmissionRequest(clientPeer *Peer, hostPeerID peer.ID, channelId string) 
EndTransmission(clientPeer *Peer, hostPeerID peer.ID, channelId string)

SendMessage(clientPeer *Peer, hostPeerID peer.ID, channelId string, message string)
```

Requests that require an asynchronous response are handled as follows 

```
select {
    case <-request.success:
       ...
    case <-request.fail:
       ...
}
```

Each request channel returns a Response struct, which contains the rror code for the 
request. 

See ui.go and main.go for examples. 
