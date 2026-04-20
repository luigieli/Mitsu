package common

import (
	"sync"
	"sync/atomic"
)

// LanguageChannels is a first-class collection for language notification channels.
type LanguageChannels []chan Language

// LanguageListeners is a first-class collection of language listener channels.
type LanguageListeners struct {
	Channels LanguageChannels
}

// SynchronizedLanguageListeners provides thread-safe access to language listeners.
type SynchronizedLanguageListeners struct {
	mutex     sync.Mutex
	listeners LanguageListeners
}

// Add appends a new channel to the listeners.
func (listeners *SynchronizedLanguageListeners) Add(channel chan Language) {
	listeners.mutex.Lock()
	defer listeners.mutex.Unlock()
	listeners.listeners.Channels = append(listeners.listeners.Channels, channel)
}

// Remove removes a channel from the listeners.
func (listeners *SynchronizedLanguageListeners) Remove(channel chan Language) {
	listeners.mutex.Lock()
	defer listeners.mutex.Unlock()
	for index, currentListener := range listeners.listeners.Channels {
		if currentListener == channel {
			listeners.listeners.Channels = append(listeners.listeners.Channels[:index], listeners.listeners.Channels[index+1:]...)
			close(channel)
			return
		}
	}
}

// Notify sends the current language to all registered listener channels.
func (listeners *SynchronizedLanguageListeners) Notify(language Language) {
	listeners.mutex.Lock()
	defer listeners.mutex.Unlock()
	for _, channel := range listeners.listeners.Channels {
		select {
		case channel <- language:
		default:
		}
	}
}

// LanguageController manages the current language and its listeners.
type LanguageController struct {
	current   atomic.Value // Language
	listeners *SynchronizedLanguageListeners
}

// Current returns the current system language.
func (controller *LanguageController) Current() Language {
	return controller.current.Load().(Language)
}

// SwitchTo updates the system language and notifies listeners.
func (controller *LanguageController) SwitchTo(language Language) {
	controller.current.Store(language)
	controller.listeners.Notify(language)
}

// Subscribe returns a new channel to listen for language changes.
func (controller *LanguageController) Subscribe() chan Language {
	channel := make(chan Language, 1)
	channel <- controller.Current()
	controller.listeners.Add(channel)
	return channel
}

// Unsubscribe removes a listener channel.
func (controller *LanguageController) Unsubscribe(channel chan Language) {
	controller.listeners.Remove(channel)
}

// SpeakingChannels is a first-class collection for speaking notification channels.
type SpeakingChannels []chan bool

// SpeakingListeners is a first-class collection of speaking listener channels.
type SpeakingListeners struct {
	Channels SpeakingChannels
}

// SynchronizedSpeakingListeners provides thread-safe access to speaking listeners.
type SynchronizedSpeakingListeners struct {
	mutex     sync.Mutex
	listeners SpeakingListeners
}

// Add appends a new channel to the listeners.
func (listeners *SynchronizedSpeakingListeners) Add(channel chan bool) {
	listeners.mutex.Lock()
	defer listeners.mutex.Unlock()
	listeners.listeners.Channels = append(listeners.listeners.Channels, channel)
}

// Remove removes a channel from the listeners.
func (listeners *SynchronizedSpeakingListeners) Remove(channel chan bool) {
	listeners.mutex.Lock()
	defer listeners.mutex.Unlock()
	for index, currentListener := range listeners.listeners.Channels {
		if currentListener == channel {
			listeners.listeners.Channels = append(listeners.listeners.Channels[:index], listeners.listeners.Channels[index+1:]...)
			close(channel)
			return
		}
	}
}

// Notify sends the speaking state to all registered listener channels.
func (listeners *SynchronizedSpeakingListeners) Notify(isSpeaking bool) {
	listeners.mutex.Lock()
	defer listeners.mutex.Unlock()
	for _, channel := range listeners.listeners.Channels {
		select {
		case channel <- isSpeaking:
		default:
		}
	}
}

// SpeakingController manages the speaking state notifications.
type SpeakingController struct {
	listeners *SynchronizedSpeakingListeners
}

// CoordinateSpeaking notifies listeners of a change in speaking state.
func (controller *SpeakingController) CoordinateSpeaking(isSpeaking bool) {
	controller.listeners.Notify(isSpeaking)
}

// Subscribe returns a new channel to listen for speaking state changes.
func (controller *SpeakingController) Subscribe() chan bool {
	channel := make(chan bool, 1)
	controller.listeners.Add(channel)
	return channel
}

// Unsubscribe removes a listener channel.
func (controller *SpeakingController) Unsubscribe(channel chan bool) {
	controller.listeners.Remove(channel)
}

// LanguageState aggregates the language and speaking controllers.
type LanguageState struct {
	Language *LanguageController
	Speaking *SpeakingController
}

// NewLanguageState creates a new initialized LanguageState.
func NewLanguageState(initial Language) *LanguageState {
	languageController := &LanguageController{
		listeners: &SynchronizedLanguageListeners{
			listeners: LanguageListeners{Channels: make(LanguageChannels, 0)},
		},
	}
	languageController.current.Store(initial)

	return &LanguageState{
		Language: languageController,
		Speaking: &SpeakingController{
			listeners: &SynchronizedSpeakingListeners{
				listeners: SpeakingListeners{Channels: make(SpeakingChannels, 0)},
			},
		},
	}
}

// CurrentLanguage returns the current system language.
func (state *LanguageState) CurrentLanguage() Language {
	return state.Language.Current()
}

// SwitchLanguage updates the system language.
func (state *LanguageState) SwitchLanguage(language Language) {
	state.Language.SwitchTo(language)
}

// CoordinateSpeaking updates the speaking state.
func (state *LanguageState) CoordinateSpeaking(isSpeaking bool) {
	state.Speaking.CoordinateSpeaking(isSpeaking)
}

// SubscribeToLanguage returns a channel for language change notifications.
func (state *LanguageState) SubscribeToLanguage() chan Language {
	return state.Language.Subscribe()
}

// SubscribeToSpeaking returns a channel for speaking state change notifications.
func (state *LanguageState) SubscribeToSpeaking() chan bool {
	return state.Speaking.Subscribe()
}

// UnsubscribeFromLanguage removes a language listener channel.
func (state *LanguageState) UnsubscribeFromLanguage(channel chan Language) {
	state.Language.Unsubscribe(channel)
}

// UnsubscribeFromSpeaking removes a speaking listener channel.
func (state *LanguageState) UnsubscribeFromSpeaking(channel chan bool) {
	state.Speaking.Unsubscribe(channel)
}
