package gaming

import (
	"bufio"
	"context"
	"fmt"
	"mitsu/pkg/common"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	GamingConnectionTimeout = 2 * time.Second
	GamingRetryDelay        = 5 * time.Second
	GamingLoopInterval      = 100 * time.Millisecond
	GamingDisabledDelay     = 1 * time.Second
)

// SynchronizedConnection provides thread-safe access to a network connection.
type SynchronizedConnection struct {
	mutex      sync.Mutex
	connection net.Conn
}

// GameController manages the connection and data flow with a game bridge.
type GameController struct {
	Configuration *GamingConfiguration
	Runtime       *GamingRuntime
}

// GamingConfiguration holds the static and stateful configuration for gaming.
type GamingConfiguration struct {
	Network *GamingNetwork
	Control *GamingControl
}

// GamingNetwork manages the connection address.
type GamingNetwork struct {
	Address common.Address
}

// GamingControl manages the operational state of gaming features.
type GamingControl struct {
	Enabled atomic.Bool
}

// GamingRuntime handles the communication channels and active connection.
type GamingRuntime struct {
	Channels   *GamingChannels
	Connection *SynchronizedConnection
}

// GamingChannels holds the communication channels for gaming state and commands.
type GamingChannels struct {
	StateChannel   chan string
	CommandChannel chan string
}

// NewGameController creates a new initialized GameController.
func NewGameController(address common.Address) *GameController {
	return &GameController{
		Configuration: &GamingConfiguration{
			Network: &GamingNetwork{Address: address},
			Control: &GamingControl{},
		},
		Runtime: &GamingRuntime{
			Channels: &GamingChannels{
				StateChannel:   make(chan string, 10),
				CommandChannel: make(chan string, 10),
			},
			Connection: &SynchronizedConnection{},
		},
	}
}

// Start begins the gaming controller processing loop.
func (gameController *GameController) Start(applicationContext context.Context) {
	fmt.Println("Gaming Controller routine started.")
	
	for {
		if gameController.runIteration(applicationContext) {
			return
		}
	}
}

func (gameController *GameController) runIteration(applicationContext context.Context) bool {
	if applicationContext.Err() != nil {
		gameController.closeConnection()
		return true
	}

	if !gameController.Configuration.Control.Enabled.Load() {
		gameController.handleDisabledState()
		return false
	}

	gameController.ensureConnection(applicationContext)
	gameController.processCommands()
	
	return false
}

func (gameController *GameController) handleDisabledState() {
	gameController.Runtime.Connection.mutex.Lock()
	if gameController.Runtime.Connection.connection != nil {
		fmt.Println("Gamer Mode disabled. Closing connection.")
		gameController.Runtime.Connection.connection.Close()
		gameController.Runtime.Connection.connection = nil
	}
	gameController.Runtime.Connection.mutex.Unlock()
	time.Sleep(GamingDisabledDelay)
}

func (gameController *GameController) ensureConnection(applicationContext context.Context) {
	gameController.Runtime.Connection.mutex.Lock()
	hasConnection := gameController.Runtime.Connection.connection != nil
	gameController.Runtime.Connection.mutex.Unlock()

	if !hasConnection {
		gameController.attemptConnection(applicationContext)
	}
}

func (gameController *GameController) attemptConnection(applicationContext context.Context) {
	address := string(gameController.Configuration.Network.Address)
	fmt.Printf("Attempting to connect to mGBA at %s...\n", address)
	
	connection, connectionError := net.DialTimeout("tcp", address, GamingConnectionTimeout)
	if connectionError != nil {
		fmt.Printf("Gaming Bridge not found: %v. Retrying in 5s...\n", connectionError)
		time.Sleep(GamingRetryDelay)
		return
	}

	gameController.Runtime.Connection.mutex.Lock()
	gameController.Runtime.Connection.connection = connection
	gameController.Runtime.Connection.mutex.Unlock()
	
	fmt.Println("Connected to Mitsu Pokémon Bridge!")
	go gameController.readLoop(applicationContext)
}

func (gameController *GameController) processCommands() {
	select {
	case command := <-gameController.Runtime.Channels.CommandChannel:
		gameController.sendCommandToBridge(command)
	case <-time.After(GamingLoopInterval):
	}
}

func (gameController *GameController) sendCommandToBridge(command string) {
	gameController.Runtime.Connection.mutex.Lock()
	defer gameController.Runtime.Connection.mutex.Unlock()
	if gameController.Runtime.Connection.connection != nil {
		fmt.Fprintf(gameController.Runtime.Connection.connection, "%s\n", command)
	}
}

func (gameController *GameController) closeConnection() {
	gameController.Runtime.Connection.mutex.Lock()
	defer gameController.Runtime.Connection.mutex.Unlock()
	if gameController.Runtime.Connection.connection != nil {
		gameController.Runtime.Connection.connection.Close()
		gameController.Runtime.Connection.connection = nil
	}
}

func (gameController *GameController) readLoop(applicationContext context.Context) {
	gameController.Runtime.Connection.mutex.Lock()
	connection := gameController.Runtime.Connection.connection
	gameController.Runtime.Connection.mutex.Unlock()
	if connection == nil {
		return
	}

	reader := bufio.NewReader(connection)
	for {
		if gameController.shouldStopReading(applicationContext) {
			return
		}

		line, readError := reader.ReadString('\n')
		if readError != nil {
			gameController.handleReadError(readError, connection)
			return
		}

		gameController.dispatchState(applicationContext, line)
	}
}

func (gameController *GameController) shouldStopReading(applicationContext context.Context) bool {
	return applicationContext.Err() != nil || !gameController.Configuration.Control.Enabled.Load()
}

func (gameController *GameController) handleReadError(readError error, currentConnection net.Conn) {
	fmt.Printf("Gaming Bridge connection lost: %v\n", readError)
	gameController.Runtime.Connection.mutex.Lock()
	if gameController.Runtime.Connection.connection == currentConnection {
		gameController.Runtime.Connection.connection = nil
	}
	gameController.Runtime.Connection.mutex.Unlock()
}

func (gameController *GameController) dispatchState(applicationContext context.Context, line string) {
	select {
	case gameController.Runtime.Channels.StateChannel <- strings.TrimSpace(line):
	case <-applicationContext.Done():
	default:
	}
}

// Toggle switches the operational state of gaming and returns the new status string.
func (gameController *GameController) Toggle() string {
	enabled := gameController.Configuration.Control.Enabled.Load()
	gameController.Configuration.Control.Enabled.Store(!enabled)
	if !enabled {
		return "ON"
	}
	return "OFF"
}

// SendCommand queues a command to be sent to the gaming bridge.
func (gameController *GameController) SendCommand(command string) {
	if gameController.Configuration.Control.Enabled.Load() {
		gameController.Runtime.Channels.CommandChannel <- command
	}
}
