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

type simpleResp struct {
	ID      string `json:"id"`
	Ident   string `json:"ident"`
	Command string `json:"command"`
	Data    string `json:"data"`
}

type simpleArrayResp struct {
	ID      string   `json:"id"`
	Ident   string   `json:"ident"`
	Command string   `json:"command"`
	Data    []string `json:"data"`
}

func sendError(c *websocket.Conn, id string, err error) {
	c.WriteJSON(&simpleResp{
		ID:      id,
		Ident:   centralIdent,
		Command: "error",
		Data:    err.Error(),
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
		socketLock.Lock()
		if sockets[ident] == c {
			delete(sockets, ident)
		}
		socketLock.Unlock()
		makeServerList()
	}()
	defer c.Close()

	c.WriteJSON(&simpleResp{
		ID:      "ID_WELCOME",
		Ident:   centralIdent,
		Command: "welcome",
		Data:    ident,
	})

	for {
		decoded := make(map[string]interface{})
		err := c.ReadJSON(&decoded)
		if err != nil {
			sendError(c, "UNKNOWN", err)
			break
		}
		decoded["ident"] = ident

		encoded, err := json.Marshal(decoded)

		id, ok := decoded["id"].(string)
		if !ok {
			sendError(c, "UNKNOWN", errors.New("No ID"))
			continue
		}

		cmd, cmdOk := decoded["command"].(string)

		if !cmdOk || cmd == "" {
			sendError(c, id, errors.New("No command"))
			continue
		}

		target, targetOk := decoded["target"].(string)
		if targetOk && target == "" {
			targetOk = false
		}

		if targetOk && target == centralIdent {
			if cmd == "servers" {
				c.WriteJSON(&simpleArrayResp{
					ID:      id,
					Ident:   centralIdent,
					Command: cmd,
					Data:    serverList,
				})
			} else if cmd == "ping" {
				c.WriteJSON(&simpleResp{
					ID:      id,
					Ident:   centralIdent,
					Command: "reply",
				})
			} else if cmd == "reply" {
				// Go reply, ignore it...
			} else {
				sendError(c, id, errors.New("Invalid command"))
			}
			continue
		}

		if targetOk {
			if sendTo(target, encoded) {
				log.Printf("[> %s] %s", target, encoded)
			} else {
				log.Printf("[> %s ???] %s", target, encoded)
				sendError(c, id, errors.New("No server found with ident"))
			}
		} else {
			log.Printf("[>>>] %s", encoded)
			go broadcast(encoded)
			if cmd == "ping" {
				c.WriteJSON(&simpleResp{
					ID:      id,
					Ident:   centralIdent,
					Command: "reply",
				})
			}
		}
	}
}

func main() {
	sockets = make(map[string]*websocket.Conn)
	makeServerList()

	http.HandleFunc("/ws/central", wshandler)
	err := http.ListenAndServe("127.0.0.1:9888", nil)
	if err != nil {
		panic(err)
	}
}
