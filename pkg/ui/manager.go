package ui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"mitsu/pkg/common"
	"mitsu/pkg/gaming"
	"net/http"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML string

const (
	ShutdownTimeout = 5 * time.Second
)

// UIManager manages the web-based user interface and event broadcasting.
type UIManager struct {
	Language      *common.LanguageState
	Game          *gaming.GameController
	SpeechToBrain common.SpeechChannel
	UiMessages    chan string
	Broker        *Broker
}

// NewUIManager creates a new initialized UIManager.
func NewUIManager(language *common.LanguageState, game *gaming.GameController, speech common.SpeechChannel, uiMessages chan string) *UIManager {
	return &UIManager{
		Language:      language,
		Game:          game,
		SpeechToBrain: speech,
		UiMessages:    uiMessages,
		Broker: &Broker{
			clients:        make(map[chan string]bool),
			newClients:     make(chan chan string),
			defunctClients: make(chan chan string),
			messages:       uiMessages,
		},
	}
}

// StartServer begins the web server and event broker.
func (uiManager *UIManager) StartServer(applicationContext context.Context, address string) {
	go uiManager.Broker.Start(applicationContext)

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", uiManager.handleIndex)
	serveMux.HandleFunc("/talk", uiManager.handleTalk)
	serveMux.HandleFunc("/hotswap", uiManager.handleHotswap)
	serveMux.HandleFunc("/gaming/toggle", uiManager.handleGamingToggle)
	serveMux.HandleFunc("/events", uiManager.handleEvents)

	server := &http.Server{
		Addr:    address,
		Handler: serveMux,
	}

	go func() {
		fmt.Printf("UI Manager: Starting web server on %s\n", address)
		if serverError := server.ListenAndServe(); serverError != nil && serverError != http.ErrServerClosed {
			fmt.Printf("UI Manager Error: %v\n", serverError)
		}
	}()

	<-applicationContext.Done()
	fmt.Println("UI Manager: Shutting down web server...")
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancelShutdown()
	if shutdownError := server.Shutdown(shutdownContext); shutdownError != nil {
		fmt.Printf("UI Manager Shutdown Error: %v\n", shutdownError)
	}
}

func (uiManager *UIManager) handleIndex(responseWriter http.ResponseWriter, request *http.Request) {
	responseWriter.Header().Set("Content-Type", "text/html")
	fmt.Fprint(responseWriter, indexHTML)
}

func (uiManager *UIManager) handleTalk(responseWriter http.ResponseWriter, request *http.Request) {
	text := request.URL.Query().Get("text")
	if text != "" {
		uiManager.SpeechToBrain <- common.SpeechEntry{
			Details: common.SpeechDetails{
				Text:     common.Transcription(text),
				Language: uiManager.Language.CurrentLanguage(),
			},
			Context: common.EntryContext{
				Timestamp: time.Now(),
				Profile:   common.NewProfile(),
			},
		}
		fmt.Fprint(responseWriter, "OK")
	}
}

func (uiManager *UIManager) handleHotswap(responseWriter http.ResponseWriter, request *http.Request) {
	language := common.Language(request.URL.Query().Get("lang"))
	if language != common.LanguageEnglish && language != common.LanguagePortuguese {
		http.Error(responseWriter, "Invalid language", http.StatusBadRequest)
		return
	}

	uiManager.Language.SwitchLanguage(language)
	message, _ := json.Marshal(map[string]string{"text": "HOTSWAP: Switched to " + string(language), "type": "status"})
	uiManager.UiMessages <- string(message)
	fmt.Fprintf(responseWriter, "Switched to %s", language)
}

func (uiManager *UIManager) handleGamingToggle(responseWriter http.ResponseWriter, request *http.Request) {
	enabled := uiManager.Game.Configuration.Control.Enabled.Load()
	uiManager.Game.Configuration.Control.Enabled.Store(!enabled)
	status := "OFF"
	if !enabled {
		status = "ON"
	}
	message, _ := json.Marshal(map[string]string{"status": status, "type": "gaming"})
	uiManager.UiMessages <- string(message)
	fmt.Fprint(responseWriter, "OK")
}

func (uiManager *UIManager) handleEvents(responseWriter http.ResponseWriter, request *http.Request) {
	flusher, ok := responseWriter.(http.Flusher)
	if !ok {
		return
	}
	messageChannel := make(chan string)
	uiManager.Broker.newClients <- messageChannel
	defer func() { uiManager.Broker.defunctClients <- messageChannel }()

	responseWriter.Header().Set("Content-Type", "text/event-stream")
	responseWriter.Header().Set("Cache-Control", "no-cache")
	responseWriter.Header().Set("Connection", "keep-alive")

	for {
		select {
		case message := <-messageChannel:
			fmt.Fprintf(responseWriter, "data: %s\n\n", message)
			flusher.Flush()
		case <-request.Context().Done():
			return
		}
	}
}

// Broker handles the distribution of messages to connected web clients.
type Broker struct {
	clients        map[chan string]bool
	newClients     chan chan string
	defunctClients chan chan string
	messages       chan string
	mutex          sync.Mutex
}

// Start begins the broker message distribution loop.
func (broker *Broker) Start(applicationContext context.Context) {
	for {
		select {
		case <-applicationContext.Done():
			return
		case clientChannel := <-broker.newClients:
			broker.addClient(clientChannel)
		case clientChannel := <-broker.defunctClients:
			broker.removeClient(clientChannel)
		case message, ok := <-broker.messages:
			if !ok {
				return
			}
			broker.broadcastMessage(message)
		}
	}
}

func (broker *Broker) addClient(clientChannel chan string) {
	broker.mutex.Lock()
	defer broker.mutex.Unlock()
	broker.clients[clientChannel] = true
}

func (broker *Broker) removeClient(clientChannel chan string) {
	broker.mutex.Lock()
	defer broker.mutex.Unlock()
	delete(broker.clients, clientChannel)
	close(clientChannel)
}

func (broker *Broker) broadcastMessage(message string) {
	broker.mutex.Lock()
	defer broker.mutex.Unlock()
	for clientChannel := range broker.clients {
		select {
		case clientChannel <- message:
		default:
		}
	}
}
