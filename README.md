
# Multilogue
Multilogue Chat protocol 


## Summary: 
- Local peers send a "gravitation" request which includes a Metadata ID (list of properties that describe the peer) and their "orbit" (a collection of peers they've collected and their properties). 
- Remote peers respond with the same information 
- Upon recieving a gravitation request or response, both peers determine whether to accept a peer into their orbit based on their Metadata ID, and their orbits, as well as performing this process on the remote peers' orbit as well. 

## How to use: 
Clone the project
make deps 
go build 

./Multilogue -channel {channel} : Create and join new channel.

Multiadress listed on title bar. 

./Multilogue -channel {channel} -host {multiaddress} : Join channel at that specific multiadress 

