package main

type wsMesg struct {
	ID      string      `json:"id"`
	Target  *string     `json:"target"`
	Ident   string      `json:"ident"`
	Command string      `json:"command"`
	Data    interface{} `json:"data"`
}
