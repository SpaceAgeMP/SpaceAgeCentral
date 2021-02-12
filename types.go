package main

import "github.com/gorilla/websocket"

type wsMesg struct {
	ID      string      `json:"id"`
	Target  *string     `json:"target"`
	Ident   string      `json:"ident"`
	Command string      `json:"command"`
	Data    interface{} `json:"data"`
}

type wsSocket struct {
	c        *websocket.Conn
	nextHop  string
	isServer bool
	hidden   bool
	ident    string
}
