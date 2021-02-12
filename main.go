package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

var socketLock sync.RWMutex
var sockets map[string]*wsSocket

var serverList []string

var centralIdent = "UNSET"

func sendTo(target string, msg []byte) bool {
	socketLock.RLock()
	defer socketLock.RUnlock()

	socket := sockets[target]
	if socket == nil {
		return false
	}
	if socket.c == nil {
		socket = sockets[socket.nextHop]
		if socket == nil {
			return false
		}
	}
	go socket.c.WriteMessage(websocket.TextMessage, msg)
	return true
}

func broadcast(msg []byte) {
	for name := range sockets {
		go sendTo(name, msg)
	}
}

func handleDisconn(obj *wsSocket) bool {
	isThis := false
	socketLock.Lock()
	if sockets[obj.ident] == obj {
		isThis = true
		delete(sockets, obj.ident)
	}
	socketLock.Unlock()
	makeServerList()
	if obj.c != nil {
		obj.c.Close()
	}

	if isThis {
		typ := "Central"
		if obj.isServer {
			typ = "Server"
		}
		log.Printf("[+ %s] %s offline", obj.ident, typ)
	}

	if !obj.isServer || obj.hidden || !isThis {
		return isThis
	}
	d, _ := json.Marshal(&wsMesg{
		ID:      "ID_DUMMY",
		Ident:   centralIdent,
		Command: "serverleave",
		Data:    obj.ident,
	})
	go broadcast(d)

	return isThis
}

func handleConn(obj *wsSocket) bool {
	obj.c.WriteJSON(&wsMesg{
		ID:      "ID_DUMMY",
		Ident:   centralIdent,
		Command: "welcome",
		Data:    obj.ident,
	})

	alreadyConnected := false
	socketLock.Lock()
	oldC := sockets[obj.ident]
	if oldC != nil {
		alreadyConnected = true
		delete(sockets, obj.ident)
		go oldC.c.Close()
	}
	sockets[obj.ident] = obj
	socketLock.Unlock()
	makeServerList()

	typ := "Central"
	if obj.isServer {
		typ = "Server"
	}
	log.Printf("[+ %s] %s online", obj.ident, typ)

	return alreadyConnected
}

func makeServerList() {
	socketLock.RLock()
	list := make([]string, 0, len(sockets))
	for name, sock := range sockets {
		if sock.isServer {
			list = append(list, name)
		}
	}
	socketLock.RUnlock()
	serverList = list
}

func main() {
	centralIdent = os.Getenv("NAME")

	sockets = make(map[string]*wsSocket)
	makeServerList()

	http.HandleFunc("/ws/server", serverhandler)
	http.HandleFunc("/ws/interlink", interlinkhandler)
	err := http.ListenAndServe("127.0.0.1:9888", nil)
	if err != nil {
		panic(err)
	}
}
