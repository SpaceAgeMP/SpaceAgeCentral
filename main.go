package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

var socketLock sync.RWMutex
var sockets map[string]*websocket.Conn

var serverList []string

const centralIdent = "Geminga"

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

func makeServerList() {
	socketLock.RLock()
	list := make([]string, 0, len(sockets))
	for name := range sockets {
		list = append(list, name)
	}
	socketLock.RUnlock()
	serverList = list
}

type identHTTPResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type wsMesg struct {
	ID      string      `json:"id"`
	Target  *string     `json:"target"`
	Ident   string      `json:"ident"`
	Command string      `json:"command"`
	Data    interface{} `json:"data"`
}

func sendError(c *websocket.Conn, id string, err error) {
	c.WriteJSON(&wsMesg{
		ID:      id,
		Ident:   centralIdent,
		Command: "error",
		Data:    err.Error(),
	})
}

func sendReply(c *websocket.Conn, id string, data interface{}) {
	c.WriteJSON(&wsMesg{
		ID:      id,
		Ident:   centralIdent,
		Command: "reply",
		Data:    data,
	})
}

func getIdent(w http.ResponseWriter, r *http.Request) string {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://api.spaceage.mp/v2/servers/self", nil)
	if err != nil {
		w.WriteHeader(400)
		return ""
	}
	req.Header.Add("Authorization", r.Header.Get("Authorization"))
	resp, err := client.Do(req)
	if err != nil {
		w.WriteHeader(400)
		return ""
	}

	if resp.StatusCode != 200 {
		w.WriteHeader(resp.StatusCode)
		return ""
	}

	var respData identHTTPResp
	err = json.NewDecoder(resp.Body).Decode(&respData)
	resp.Body.Close()
	if err != nil {
		w.WriteHeader(500)
		return ""
	}

	return respData.Name
}

func wshandler(w http.ResponseWriter, r *http.Request) {
	ident := getIdent(w, r)
	if ident == "" {
		return
	}

	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		w.WriteHeader(400)
		return
	}

	handleServerConn(ident, c)
}

func handleServerConn(ident string, c *websocket.Conn) {
	c.WriteJSON(&wsMesg{
		ID:      "ID_DUMMY",
		Ident:   centralIdent,
		Command: "welcome",
		Data:    ident,
	})

	socketLock.Lock()
	oldC := sockets[ident]
	if oldC != nil {
		delete(sockets, ident)
		go oldC.Close()
	}
	sockets[ident] = c
	socketLock.Unlock()
	makeServerList()

	defer func() {
		sendServerLeave := false
		socketLock.Lock()
		if sockets[ident] == c {
			delete(sockets, ident)
			sendServerLeave = true
		}
		socketLock.Unlock()
		makeServerList()

		if !sendServerLeave {
			return
		}
		d, _ := json.Marshal(&wsMesg{
			ID:      "ID_DUMMY",
			Ident:   centralIdent,
			Command: "serverleave",
			Data:    ident,
		})
		go broadcast(d)
	}()
	defer c.Close()

	d, _ := json.Marshal(&wsMesg{
		ID:      "ID_DUMMY",
		Ident:   centralIdent,
		Command: "serverjoin",
		Data:    ident,
	})
	go broadcast(d)

	for {
		var decoded wsMesg
		err := c.ReadJSON(&decoded)
		if err != nil {
			sendError(c, "UNKNOWN", err)
			break
		}
		decoded.Ident = ident

		if decoded.ID == "" {
			sendError(c, "UNKNOWN", errors.New("No ID"))
			continue
		}

		if decoded.Command == "" {
			sendError(c, decoded.ID, errors.New("No command"))
			continue
		}

		target := ""
		if decoded.Target != nil {
			target = *decoded.Target
		}

		if target == centralIdent {
			if decoded.Command == "servers" {
				sendReply(c, decoded.ID, serverList)
			} else if decoded.Command == "ping" {
				sendReply(c, decoded.ID, nil)
			} else if decoded.Command == "reply" {
				// Go reply, ignore it...
			} else {
				sendError(c, decoded.ID, errors.New("Invalid command"))
			}
			continue
		}

		encoded, err := json.Marshal(decoded)

		if target != "" {
			if sendTo(*decoded.Target, encoded) {
				log.Printf("[> %s] %s", *decoded.Target, encoded)
			} else {
				log.Printf("[> %s ???] %s", *decoded.Target, encoded)
				sendError(c, decoded.ID, errors.New("No server found with ident"))
			}
		} else {
			log.Printf("[>>>] %s", encoded)
			go broadcast(encoded)
			if decoded.Command == "ping" {
				sendReply(c, decoded.ID, nil)
			}
		}
	}
}

func main() {
	sockets = make(map[string]*websocket.Conn)
	makeServerList()

	http.HandleFunc("/ws/server", wshandler)
	err := http.ListenAndServe("127.0.0.1:9888", nil)
	if err != nil {
		panic(err)
	}
}
