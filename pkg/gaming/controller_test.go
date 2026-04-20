package gaming

import (
	"context"
	"mitsu/pkg/common"
	"net"
	"testing"
	"time"
)

func TestGameController_Connection(t *testing.T) {
	// Start a dummy server
	listener, listenerError := net.Listen("tcp", "127.0.0.1:0")
	if listenerError != nil {
		t.Fatal(listenerError)
	}
	defer listener.Close()

	address := listener.Addr().String()
	gameController := NewGameController(common.Address(address))
	gameController.Configuration.Control.Enabled.Store(true)

	applicationContext, cancelApplication := context.WithCancel(context.Background())
	defer cancelApplication()

	go gameController.Start(applicationContext)

	// Accept connection
	connection, acceptError := listener.Accept()
	if acceptError != nil {
		t.Fatal(acceptError)
	}
	defer connection.Close()

	// Wait for connection to be established in gameController
	time.Sleep(100 * time.Millisecond)

	gameController.SendCommand("BUTTON_A")
	
	// Verify command sent
	buffer := make([]byte, 10)
	bytesRead, readError := connection.Read(buffer)
	if readError != nil {
		t.Fatal(readError)
	}
	if string(buffer[:bytesRead]) != "BUTTON_A\n" {
		t.Errorf("Expected BUTTON_A\\n, got %q", string(buffer[:bytesRead]))
	}
}
