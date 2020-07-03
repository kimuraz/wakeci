package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"github.com/sasha-s/go-deadlock"
)

// ClientList is a list of connected clients
type ClientList struct {
	Clients []*Client
	deadlock.Mutex
}

// Append creates new Client and appends it to the list
func (cl *ClientList) Append(ws *websocket.Conn) *Client {
	// Get IP address of a user
	addr := ws.RemoteAddr().String()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		Logger.Println(err)
		host = addr
	}

	logID := GenerateRandomString(5)

	client := Client{
		WS:           ws,
		SubscribedTo: []string{},
		Logger:       log.New(os.Stdout, "["+logID+" "+host+"] ", log.Lmicroseconds|log.Lshortfile),
		buffer:       make(chan []byte),
	}
	cl.Lock()
	defer cl.Unlock()
	cl.Clients = append(cl.Clients, &client)
	client.Logger.Println("Client connected")
	return &client
}

// Remove removes a client from connected list
func (cl *ClientList) Remove(ws *websocket.Conn) {
	cl.Lock()
	defer cl.Unlock()
	for i, v := range cl.Clients {
		if v.WS == ws {
			v.Logger.Println("Client disconnected")
			cl.Clients[i] = nil
			cl.Clients = append(cl.Clients[:i], cl.Clients[i+1:]...)
			return
		}
	}
	Logger.Println("Unable to find a client")
}

// ConnectedClients ...
var ConnectedClients ClientList

// Client represents a websocket conection and subscriptions
type Client struct {
	WS           *websocket.Conn
	SubscribedTo []string
	Logger       *log.Logger
	buffer       chan []byte
	deadlock.Mutex
}

// IsSubscribed checks if a client is subscribed for this type of messages
func (c *Client) IsSubscribed(tag string) (bool, int) {
	c.Lock()
	defer c.Unlock()
	for i, v := range c.SubscribedTo {
		if strings.HasPrefix(tag, v) {
			return true, i
		}
	}
	return false, 0
}

// Subscribe subscribes a client to message
func (c *Client) Subscribe(mt string) {
	ok, _ := c.IsSubscribed(mt)
	if !ok {
		c.Lock()
		defer c.Unlock()
		c.SubscribedTo = append(c.SubscribedTo, mt)
		c.Logger.Printf("Has subscribed to %s\n", mt)
	}
}

// Unsubscribe ...
func (c *Client) Unsubscribe(mt string) {
	ok, index := c.IsSubscribed(mt)
	if ok {
		c.Lock()
		defer c.Unlock()
		c.SubscribedTo[index] = ""
		c.SubscribedTo = append(c.SubscribedTo[:index], c.SubscribedTo[index+1:]...)
		c.Logger.Printf("Has unsubscribed from %s\n", mt)
	}
}

// Send listens on buffer channel for new messages and sends them over websocket
func (c *Client) Send() {
	for {
		select {
		case msg := <-c.buffer:
			err := c.WS.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
				c.Logger.Printf("error: %v\n", err)
			}
		}
	}
}

// HandleIncomingMessage ...
func (c *Client) HandleIncomingMessage(msg *MsgIncoming) {
	switch msg.Type {
	case MsgTypeInSubscribe:
		var data InSubscribeData
		err := json.Unmarshal(msg.Data, &data)
		if err != nil {
			c.Logger.Println(err)
			return
		}
		for _, item := range data.To {
			c.Subscribe(item)
		}
		break
	case MsgTypeInUnsubscribe:
		var data InSubscribeData
		err := json.Unmarshal(msg.Data, &data)
		if err != nil {
			c.Logger.Println(err)
			return
		}
		for _, item := range data.To {
			c.Unsubscribe(item)
		}
		break
	default:
		c.Logger.Printf("Unhandled msg: %v\n", msg)
	}
}

// BroadcastChannel contains messages to send to all connected clients
var BroadcastChannel = make(chan *MsgBroadcast)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// BroadcastMessage broadcasts messages to all connected clients
func BroadcastMessage() {
	for {
		msg := <-BroadcastChannel
		msgB, err := json.Marshal(msg)
		if err != nil {
			Logger.Println(err)
		} else {
			ConnectedClients.Lock()
			for _, client := range ConnectedClients.Clients {
				ok, _ := client.IsSubscribed(msg.Type)
				if ok {
					client.buffer <- msgB
				}
			}
			ConnectedClients.Unlock()
		}
	}
}

func handleWSConnection(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Upgrade initial GET request to a websocket
	Logger.Println("New ws connection...")
	ws, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		Logger.Fatal(err)
	}

	defer func() {
		ws.Close()
		ConnectedClients.Remove(ws)
	}()

	client := ConnectedClients.Append(ws)
	go client.Send()

	for {
		var msg MsgIncoming
		err := ws.ReadJSON(&msg)
		if err != nil {
			Logger.Println(err)
			// Connection will be  removed in defer
			return
		}
		client.HandleIncomingMessage(&msg)
	}
}
