package main

import (
	"fmt"
	"github.com/dean2021/psevent"
	"log"
)

func main() {

	event, err := psevent.Listen()
	if err != nil {
		log.Fatal(err)
	}

	// Process events
	go func() {
		for {
			select {
			case ev := <-event.Fork:
				if ev != nil {
					fmt.Println("============Fork", int32(ev.ChildPid))
				}
			case ev := <-event.Exec:
				if ev != nil {
					fmt.Println("============Fork", int32(ev.Pid))
				}
			case ev := <-event.Exit:
				if ev != nil {
					fmt.Println("============Fork", int32(ev.Pid))
				}
			case err := <-event.Error:
				if err != nil {
					log.Println("error:", err)
				}
			}
		}
	}()

	select {}
}
