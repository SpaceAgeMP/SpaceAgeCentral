package main

import (
	"net/http"

	"github.com/gorilla/websocket"
)

func interlinkhandler(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		w.WriteHeader(400)
		return
	}

	ident := r.Header.Get("Interlink-Ident")

	handleInterlinkConn(ident, c)
}

func handleInterlinkConn(ident string, c *websocket.Conn) {

}
