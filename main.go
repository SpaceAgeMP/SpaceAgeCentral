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

const centralIdent = "CENTRAL"

func sendTo(target string, msg []byte) {
	socketLock.RLock()
	socket := sockets[target]
	socketLock.RUnlock()
	if socket == nil {
		return
	}
	socket.WriteMessage(websocket.TextMessage, msg)
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

type identResp struct {
	Name string `json:"name"`
}

type pingResp struct {
	Ident   string `json:"ident"`
	Command string `json:"command"`
}

type serverListResp struct {
	Ident   string   `json:"ident"`
	Command string   `json:"command"`
	List    []string `json:"list"`
}

type errorResp struct {
	Ident   string `json:"ident"`
	Command string `json:"command"`
	Error   string `json:"error"`
}

func sendError(c *websocket.Conn, err error) {
	c.WriteJSON(&errorResp{
		Ident:   centralIdent,
		Command: "error",
		Error:   err.Error(),
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

	var respData identResp
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

	for {
		decoded := make(map[string]interface{})
		err := c.ReadJSON(&decoded)
		if err != nil {
			sendError(c, err)
			break
		}
		decoded["ident"] = ident

		encoded, err := json.Marshal(decoded)

		target, ok := decoded["target"].(string)

		if ok && target == centralIdent {
			cmd, ok := decoded["command"].(string)
			if !ok {
				sendError(c, errors.New("No command"))
				continue
			}

			if cmd == "servers" {
				go c.WriteJSON(&serverListResp{
					Ident:   centralIdent,
					Command: cmd,
					List:    serverList,
				})
				continue
			} else if cmd == "ping" {
				go c.WriteJSON(&pingResp{
					Ident:   centralIdent,
					Command: "pong",
				})
			} else if cmd == "pong" {
				// Go pong, ignore it...
			} else {
				sendError(c, errors.New("Invalid command"))
			}
		}

		if ok {
			log.Printf("[> %s] %s", target, encoded)
			go sendTo(target, encoded)
		} else {
			log.Printf("[>>>] %s", encoded)
			go broadcast(encoded)
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
