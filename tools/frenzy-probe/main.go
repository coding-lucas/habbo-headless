package main

import (
	"encoding/hex"
	"log"
	"os"
	"time"

	g "github.com/thauanvargas/goearth"
)

var ext = g.NewExt(g.ExtInfo{
	Title:       "Consulta Frenzy temporária",
	Description: "Consulta o estado do Derby Frenzy sem alterar a sessão",
	Author:      "Codex",
	Version:     "0.1.0",
})

func main() {
	file, err := os.OpenFile("frenzy-probe.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	log.SetOutput(file)

	ext.Intercept(g.In.Id("FRENZY_DERBY_STATE"), g.In.Id("DERBY_REGISTRATION_STATE"), g.In.Id("DERBY_REGISTRATION_RESULT")).With(func(i *g.Intercept) {
		first := i.Packet.ReadInt()
		second := i.Packet.ReadInt()
		log.Printf("IN name=%s payload=%s campos=%d,%d", i.Name(), hex.EncodeToString(i.Packet.Data), first, second)
	})
	ext.Connected(func(_ g.ConnectArgs) {
		go func() {
			time.Sleep(250 * time.Millisecond)
			log.Print("OUT name=GET_FRENZY_DERBY_STATE")
			ext.Send(g.Out.Id("GET_FRENZY_DERBY_STATE"))
			time.Sleep(500 * time.Millisecond)
			log.Print("OUT name=ATTEMPT_TO_REGISTER_FOR_DERBY derbyId=1346")
			packet := ext.NewPacket(g.Out.Id("GET_FRENZY_DERBY_STATE"))
			packet.Header = g.Header{Dir: g.Out, Value: 1108}
			packet.Data = nil
			packet.Pos = 0
			packet.WriteInt(1346)
			ext.SendPacket(packet)
			time.Sleep(800 * time.Millisecond)
			log.Print("OUT name=GET_DERBY_REGISTRATION_STATE")
			ext.Send(g.Out.Id("GET_DERBY_REGISTRATION_STATE"))
		}()
	})
	ext.MustConnect(62673)
	ext.Run()
}
