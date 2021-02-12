package main

import (
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

var socketLock sync.RWMutex
var sockets map[string]*websocket.Conn

var serverList []string

var centralIdent = "UNSET"

func sendTo(target string, msg []byte) bool {
	socketLock.RLock()
	socket := sockets[target]
	socketLock.RUnlock()
	if socket == nil {
		return false
	}
	go socket.WriteMessage(websocket.TextMessage, msg)
	return true
}

func broadcast(msg []byte) {
	socketLock.RLock()
	for _, socket := range sockets {
		go socket.WriteMessage(websocket.TextMessage, msg)
	}
	socketLock.RUnlock()
}

func handleDisconn(c *websocket.Conn, ident string) bool {
	isThis := false
	socketLock.Lock()
	if sockets[ident] == c {
		isThis = true
		delete(sockets, ident)
	}
	socketLock.Unlock()
	makeServerList()
	return isThis
}

func handleConn(c *websocket.Conn, ident string) bool {
	c.WriteJSON(&wsMesg{
		ID:      "ID_DUMMY",
		Ident:   centralIdent,
		Command: "welcome",
		Data:    ident,
	})

	alreadyConnected := false
	socketLock.Lock()
	oldC := sockets[ident]
	if oldC != nil {
		alreadyConnected = true
		delete(sockets, ident)
		go oldC.Close()
	}
	sockets[ident] = c
	socketLock.Unlock()
	makeServerList()

	return alreadyConnected
}

func makeServerList() {
	socketLock.RLock()
	list := make([]string, 0, len(sockets))
	for name := range sockets {
		list = append(list, name)
	}
	socketLock.RUnlock()
	serverList = list
}

func main() {
	centralIdent = os.Getenv("NAME")

	sockets = make(map[string]*websocket.Conn)
	makeServerList()

	http.HandleFunc("/ws/server", serverhandler)
	//http.HandleFunc("/ws/interlink", interlinkhandler)
	err := http.ListenAndServe("127.0.0.1:9888", nil)
	if err != nil {
		panic(err)
	}
}
