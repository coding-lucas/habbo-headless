package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	g "github.com/thauanvargas/goearth"
	"github.com/thauanvargas/goearth/shockwave/profile"
)

type result struct {
	Name   string `json:"name"`
	Figure string `json:"figure"`
	Gender string `json:"gender"`
	Sent   bool   `json:"sent"`
}

func main() {
	if len(os.Args) < 2 {
		panic("uso: skin-ext <porta> [figura-servidor|--capture]")
	}
	port, err := strconv.Atoi(os.Args[1])
	if err != nil {
		panic(err)
	}
	figure := ""
	if len(os.Args) > 2 {
		figure = os.Args[2]
	}
	ext := g.NewExt(g.ExtInfo{Title: "Codex Skin", Description: "Aplicador de aparência sem interromper a sessão", Author: "Codex", Version: "1.0"})
	if figure == "--capture" {
		ext.Intercept(g.Out.Id("UPDATE")).With(func(event *g.Intercept) {
			fmt.Printf("CAPTURE header=%d name=%q data=%x\n", event.Packet.Header.Value, event.Name(), event.Packet.Data)
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"header": event.Packet.Header.Value,
				"name":   event.Name(),
				"data":   fmt.Sprintf("%x", event.Packet.Data),
			})
			go func() {
				time.Sleep(300 * time.Millisecond)
				os.Exit(0)
			}()
		})
		if err := ext.Connect(port); err != nil {
			panic(err)
		}
		ext.Run()
		return
	}
	if figure == "--hijack" {
		if len(os.Args) < 4 {
			panic("uso: skin-ext <porta> --hijack <figura-servidor>")
		}
		desired := os.Args[3]
		var swapped atomic.Bool
		ext.Intercept(g.Out.Id("PONG")).With(func(event *g.Intercept) {
			if !swapped.CompareAndSwap(false, true) {
				return
			}
			replacement := ext.NewPacket(
				g.Out.Id("UPDATE"),
				int16(4), desired,
				int16(10), false,
				int16(1), false,
				int16(18), "",
			)
			event.Packet.Header = replacement.Header
			event.Packet.Data = append(event.Packet.Data[:0], replacement.Data...)
			event.Packet.Pos = 0
			fmt.Printf("HIJACKED header=%d data=%x\n", event.Packet.Header.Value, event.Packet.Data)
			go func() {
				time.Sleep(time.Second)
				ext.Send(g.Out.Id("INFORETRIEVE"))
			}()
		})
		ext.Intercept(g.In.Id("UPDATEOK"), g.In.Id("USER_OBJ"), g.In.Id("ERROR")).With(func(event *g.Intercept) {
			if !swapped.Load() {
				return
			}
			fmt.Printf("RESULT header=%d name=%q data=%x\n", event.Packet.Header.Value, event.Name(), event.Packet.Data)
			if event.Is(g.In.Id("USER_OBJ")) {
				packet := event.Packet.Copy()
				var current profile.Profile
				packet.Read(&current)
				_ = json.NewEncoder(os.Stdout).Encode(result{Name: current.Name, Figure: current.Figure, Gender: current.Gender, Sent: true})
				if current.Figure == desired {
					go func() {
						time.Sleep(200 * time.Millisecond)
						os.Exit(0)
					}()
				}
			}
		})
		go func() {
			time.Sleep(75 * time.Second)
			fmt.Println("TIMEOUT aguardando PONG/UPDATEOK")
			os.Exit(2)
		}()
		if err := ext.Connect(port); err != nil {
			panic(err)
		}
		ext.Run()
		return
	}
	var applyOnce sync.Once
	var tracing atomic.Bool
	var traceCount atomic.Int32
	ext.InterceptAll(func(event *g.Intercept) {
		if !tracing.Load() || event.Dir() != g.In || traceCount.Add(1) > 80 {
			return
		}
		fmt.Printf("TRACE dir=%s header=%d name=%q len=%d\n", event.Dir().String(), event.Packet.Header.Value, event.Name(), len(event.Packet.Data))
	})
	ext.Intercept(g.In.Id("UPDATEOK"), g.In.Id("FIGURE_CHANGE"), g.In.Id("ERROR")).With(func(event *g.Intercept) {
		fmt.Printf("EVENT header=%d data=%x\n", event.Packet.Header.Value, event.Packet.Data)
	})
	mgr := profile.NewManager(ext)
	mgr.Updated(func(args profile.Args) {
		sent := false
		if figure != "" {
			applyOnce.Do(func() {
				if figure == "--wave" {
					tracing.Store(true)
					packet := ext.NewPacket(g.Out.Id("WAVE"))
					fmt.Printf("SEND header=%d name=WAVE data=%x\n", packet.Header.Value, packet.Data)
					ext.SendPacket(packet)
					sent = true
					return
				}
				tracing.Store(true)
				ext.Send(g.Out.Id("GETAVAILABLESETS"))
				go func() {
					time.Sleep(750 * time.Millisecond)
					// Espelha exatamente o UPDATE emitido pelo cliente Origins 344:
					// figura + aceite + consentimento parental + TOTP vazio.
					packet := ext.NewPacket(
						g.Out.Id("UPDATE"),
						int16(4), figure,
						int16(10), false,
						int16(1), false,
						int16(18), "",
					)
					fmt.Printf("SEND header=%d data=%x\n", packet.Header.Value, packet.Data)
					ext.SendPacket(packet)
				}()
				sent = true
			})
		}
		_ = json.NewEncoder(os.Stdout).Encode(result{Name: args.Profile.Name, Figure: args.Profile.Figure, Gender: args.Profile.Gender, Sent: sent})
		go func() {
			delay := 600 * time.Millisecond
			if sent {
				delay = 3 * time.Second
			}
			time.Sleep(delay)
			os.Exit(0)
		}()
	})
	if err := ext.Connect(port); err != nil {
		panic(err)
	}
	ext.Run()
}
