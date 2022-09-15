# WS an open source peer to peer chat library

This is still a WIP, expect lots of breaking changes as we are live-experimenting with the API. WS uses gorilla's websocket lib to facilitate a simple chat client library that can be used to support:

- realtime peer to peer messaging with ID's (currently, we assume a mobile number, or username as an ID)
- backup for chat messages so that messages will be available even if one client is disconnected

## Roadmap

- [x] Implement peer to peer chatting
- [x] Add support to backup messages when a client is not connected
- [ ] Allow to specify which messages to retrieve, currently we retrieve all of the messages that they were not delivered
- [ ] Add support to push notifications, the idea is to provide API such that a user can hook it up with their current firebase or OneSignal credentials. This can be useful in cases of not both chat audience are connected at the same moment. 


### How to use ws

ws is built with net/http, even for our use cases that was not exactly useful, since we were using gin (noebs). However, you still can use ws even if your http library is not go's stdlib. The way for that is by writing a simple adapter that leverages http.HandlerFunc (which is shared amongst all of them)

- Example for gin. This will allow you to use a net/http handler in your gin router, with the help of gin.HandlerFun and `c *gin.Context`

``` go
func previousMessagesAdapter(msg chat.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		chat.PreviousMessages(msg, c.Writer, c.Request)
	}
}
```