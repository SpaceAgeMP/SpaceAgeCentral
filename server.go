package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type identHTTPResp struct {
	Name   string `json:"name"`
	Hidden bool   `json:"hidden"`
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

func getIdent(w http.ResponseWriter, r *http.Request) (string, bool) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://api.spaceage.mp/v2/servers/self", nil)
	if err != nil {
		w.WriteHeader(400)
		return "", false
	}
	req.Header.Add("Authorization", r.Header.Get("Authorization"))
	resp, err := client.Do(req)
	if err != nil {
		w.WriteHeader(400)
		return "", false
	}

	if resp.StatusCode != 200 {
		w.WriteHeader(resp.StatusCode)
		return "", false
	}

	var respData identHTTPResp
	err = json.NewDecoder(resp.Body).Decode(&respData)
	resp.Body.Close()
	if err != nil {
		w.WriteHeader(500)
		return "", false
	}

	return respData.Name, respData.Hidden
}

func serverhandler(w http.ResponseWriter, r *http.Request) {
	ident, hidden := getIdent(w, r)
	if ident == "" {
		return
	}

	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		w.WriteHeader(400)
		return
	}

	handleServerConn(ident, hidden, c)
}

func handleServerConn(ident string, hidden bool, c *websocket.Conn) {
	var sockStruct wsSocket
	sockStruct.c = c
	sockStruct.hidden = hidden
	sockStruct.nextHop = ""
	sockStruct.isServer = true
	sockStruct.ident = ident

	serverAlreadyConnected := handleConn(&sockStruct)

	defer handleDisconn(&sockStruct)

	if !serverAlreadyConnected && !hidden {
		d, _ := json.Marshal(&wsMesg{
			ID:      "ID_DUMMY",
			Ident:   centralIdent,
			Command: "serverjoin",
			Data:    ident,
		})
		go broadcast(d)
	}

	for {
		var decoded wsMesg
		err := c.ReadJSON(&decoded)
		if err != nil {
			log.Printf("[< %s] Error: %v", ident, err)
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
			if hidden {
				continue
			}

			if !sendTo(*decoded.Target, encoded) {
				log.Printf("[> %s ???] %s", *decoded.Target, encoded)
				sendError(c, decoded.ID, errors.New("No server found with ident"))
			}
		} else {
			if !hidden {
				go broadcast(encoded)
			}
			if decoded.Command == "ping" {
				sendReply(c, decoded.ID, nil)
			}
		}
	}
}
