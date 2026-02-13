package gaming

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

type GameController struct {
	Enabled     atomic.Bool
	Address     string
	StateChan   chan string
	CommandChan chan string
	conn        net.Conn
}

func NewGameController(address string) *GameController {
	return &GameController{
		Address:     address,
		StateChan:   make(chan string, 10),
		CommandChan: make(chan string, 10),
	}
}

func (g *GameController) Start(ctx context.Context) {
	fmt.Println("Gaming Controller routine started.")
	
	for {
		select {
		case <-ctx.Done():
			if g.conn != nil {
				g.conn.Close()
			}
			return
		default:
			if !g.Enabled.Load() {
				if g.conn != nil {
					fmt.Println("Gamer Mode disabled. Closing connection.")
					g.conn.Close()
					g.conn = nil
				}
				time.Sleep(1 * time.Second)
				continue
			}

			if g.conn == nil {
				fmt.Printf("Attempting to connect to mGBA at %s...\n", g.Address)
				conn, err := net.DialTimeout("tcp", g.Address, 2*time.Second)
				if err != nil {
					fmt.Printf("Gaming Bridge not found: %v. Retrying in 5s...\n", err)
					time.Sleep(5 * time.Second)
					continue
				}
				g.conn = conn
				fmt.Println("Connected to Mitsu Pokémon Bridge!")
				go g.readLoop(ctx)
			}

			// Handle outbound commands
			select {
			case cmd := <-g.CommandChan:
				if g.conn != nil {
					fmt.Fprintf(g.conn, "%s\n", cmd)
				}
			case <-time.After(100 * time.Millisecond):
				// Just loop
			}
		}
	}
}

func (g *GameController) readLoop(ctx context.Context) {
	reader := bufio.NewReader(g.conn)
	for {
		if !g.Enabled.Load() || g.conn == nil {
			return
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Gaming Bridge connection lost: %v\n", err)
			g.conn = nil
			return
		}

		select {
		case g.StateChan <- strings.TrimSpace(line):
		default:
			// Buffer full, skip
		}
	}
}

func (g *GameController) SendCommand(cmd string) {
	if g.Enabled.Load() {
		g.CommandChan <- cmd
	}
}
