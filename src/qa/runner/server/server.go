package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"qa/tapjio"
	"strings"
	"sync"
)

type registerCallbackEntry struct {
	token   string
	visitor tapjio.Visitor
	errChan chan error
}

type registerChannelEntry struct {
	token   string
	ch      chan interface{}
	errChan chan error
}

type Server struct {
	quitChan    chan struct{}
	isQuit      bool
	isQuitMutex *sync.Mutex

	listenAddress string
	listener      net.Listener

	cancelChan           chan string
	registerCallbackChan chan registerCallbackEntry
	visitorEntries       map[string]registerCallbackEntry

	registerChannelChan chan registerChannelEntry
	exposedChannels     map[string]registerChannelEntry

	isRunningMutex *sync.Mutex
	isRunning      bool
}

type acceptConnTokenEvent struct {
	token  string
	conn   net.Conn
	reader *bufio.Reader
}

func (s *Server) Close() error {
	m := s.isQuitMutex
	m.Lock()
	defer m.Unlock()
	if s.isQuit {
		return nil
	}

	close(s.quitChan)
	err := s.listener.Close()
	s.isQuit = true

	return err
}

func Listen(netProto, listenAddress string) (*Server, error) {
	// fmt.Fprintln(os.Stderr, "Listening on", netProto, listenAddress)
	listener, err := net.Listen(netProto, listenAddress)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error ", err)
		return nil, err
	}

	srv := &Server{
		quitChan:             make(chan struct{}),
		isQuit:               false,
		isQuitMutex:          &sync.Mutex{},
		isRunning:            true,
		isRunningMutex:       &sync.Mutex{},
		listener:             listener,
		cancelChan:           make(chan string),
		registerCallbackChan: make(chan registerCallbackEntry),
		visitorEntries:       make(map[string]registerCallbackEntry),
		exposedChannels:      make(map[string]registerChannelEntry),
		registerChannelChan:  make(chan registerChannelEntry),
	}
	go srv.run()

	return srv, nil
}

func (s *Server) run() error {
	// Should be closed by accept goroutine
	acceptConnChan := make(chan net.Conn)

	// Used (but not closed) by accept goroutine
	var errChanWg sync.WaitGroup
	errChan := make(chan error)

	errChanWg.Add(1)
	go func() {
		listener := s.listener
		defer errChanWg.Done()
		defer close(acceptConnChan)
		defer listener.Close()

		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-s.quitChan:
					return
				default:
				}
				errChan <- err

				return
			}

			acceptConnChan <- conn
		}
	}()

	// Closed once all remaining token reading goroutines have returned.
	var acceptTokenWg sync.WaitGroup
	acceptTokenChan := make(chan acceptConnTokenEvent)

	isRunningMutex := s.isRunningMutex
	defer func() {
		isRunningMutex.Lock()
		close(s.registerCallbackChan)
		close(s.registerChannelChan)
		s.isRunning = false
		isRunningMutex.Unlock()

		// Close errChan, once reads complete.
		go func() {
			errChanWg.Wait()
			close(errChan)
		}()

		// Close remaining token connection events, once their reads complete.
		go func() {
			acceptTokenWg.Wait()
			close(acceptTokenChan)
		}()

		go func() {
			reason := errors.New("No longer running")

			for entry := range s.registerChannelChan {
				entry.errChan <- reason
				close(entry.errChan)
			}
		}()

		go func() {
			reason := errors.New("No longer running")
			// Drain remaining visitor registrations
			for entry := range s.registerCallbackChan {
				err := entry.visitor.End(reason)
				if err != nil {
					entry.errChan <- err
				} else {
					entry.errChan <- reason
				}
				close(entry.errChan)
			}

			// Cancel remaining visitorEntries.
			for k, entry := range s.visitorEntries {
				delete(s.visitorEntries, k)
				err := entry.visitor.End(reason)
				if err != nil {
					entry.errChan <- err
				} else {
					entry.errChan <- reason
				}
				close(entry.errChan)
			}
		}()

		// Close remaining connections.
		go func() {
			for conn := range acceptConnChan {
				conn.Close()
			}
		}()

		go func() {
			for acceptToken := range acceptTokenChan {
				acceptToken.conn.Close()
			}
		}()

		go func() {
			for err := range errChan {
				fmt.Fprintf(os.Stderr, "Post shutdown error in server: %v\n", err)
			}
		}()
	}()

	errChanWg.Add(1)
	defer errChanWg.Done()

	acceptTokenWg.Add(1)
	defer acceptTokenWg.Done()

	for {
		select {
		case err, k := <-errChan:
			if !k {
				fmt.Fprintf(os.Stderr, "errChan unexpectedly closed: %#v\n", errChan)
			}
			fmt.Fprintf(os.Stderr, "Fatal error in server: %v\n", err)
			s.listener.Close()
			return err
		case address, k := <-s.cancelChan:
			if !k {
				fmt.Fprintf(os.Stderr, "s.cancelChan unexpectedly closed: %#v\n", s.cancelChan)
			}
			visitorEntry, ok := s.visitorEntries[address]
			if ok {
				delete(s.visitorEntries, address)
				reason := errors.New("Canceled")
				err := visitorEntry.visitor.End(reason)
				if err != nil {
					visitorEntry.errChan <- err
				} else {
					visitorEntry.errChan <- reason
				}
				close(visitorEntry.errChan)
			}

			exposedEntry, ok := s.exposedChannels[address]
			if ok {
				reason := errors.New("Canceled")
				delete(s.exposedChannels, address)
				exposedEntry.errChan <- reason
				close(exposedEntry.errChan)
			}
		case entry, k := <-s.registerCallbackChan:
			if !k {
				fmt.Fprintf(os.Stderr, "s.registerCallbackChan unexpectedly closed: %#v\n", s.registerCallbackChan)
			}
			s.visitorEntries[entry.token] = entry
		case entry, k := <-s.registerChannelChan:
			if !k {
				fmt.Fprintf(os.Stderr, "s.registerChannelChan unexpectedly closed: %#v\n", s.registerChannelChan)
			}
			s.exposedChannels[entry.token] = entry
		case conn, ok := <-acceptConnChan:
			if !ok {
				return nil
			}
			acceptTokenWg.Add(1)
			errChanWg.Add(1)
			go func(conn net.Conn) {
				defer acceptTokenWg.Done()
				defer errChanWg.Done()

				bufReader := bufio.NewReader(conn)
				token, err := bufReader.ReadString('\n')
				if err != nil {
					conn.Close()
					errChan <- err
					return
				}
				token = strings.Trim(token, "\n")
				acceptTokenChan <- acceptConnTokenEvent{token, conn, bufReader}
			}(conn)
		case accept, k := <-acceptTokenChan:
			if !k {
				fmt.Fprintf(os.Stderr, "acceptTokenChan unexpectedly closed: %#v\n", acceptTokenChan)
			}
			// Token is one-time use.
			token := accept.token
			visitorEntry, ok := s.visitorEntries[token]
			if ok {
				delete(s.visitorEntries, token)

				go func(decoder *json.Decoder, closer io.Closer, entry registerCallbackEntry) {
					defer close(entry.errChan)
					defer closer.Close()
					err := tapjio.Decode(decoder, entry.visitor)
					if err != nil {
						entry.errChan <- err
					}
				}(json.NewDecoder(accept.reader), accept.conn, visitorEntry)
				break
			}

			exposedEntry, ok := s.exposedChannels[token]
			if ok {
				delete(s.exposedChannels, token)

				go func(encoder *json.Encoder, conn net.Conn, entry registerChannelEntry) {
					defer close(entry.errChan)
					defer conn.Close()
					for value := range entry.ch {
						if err := encoder.Encode(value); err != nil {
							entry.errChan <- err
							return
						}
					}
				}(json.NewEncoder(accept.conn), accept.conn, exposedEntry)
				break
			}
		}
	}

	return nil
}

func randomToken(strlen int) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, strlen)
	for i := 0; i < strlen; i++ {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

func (s *Server) Cancel(address string) {
	split := strings.SplitN(address, "@", 2)
	token := split[0]

	isRunningMutex := s.isRunningMutex
	isRunningMutex.Lock()
	defer isRunningMutex.Unlock()
	if s.isRunning {
		s.cancelChan <- token
	}
}

// Decode returns a server address that can be used by a test runner to
// stream tapj results.
func (s *Server) Decode(callbacks tapjio.Visitor) (string, chan error, error) {
	// TODO(adamb) Fire off error handlers on callbacks if server shuts down without
	//    response.
	// Make new token
	token := randomToken(16)
	isRunningMutex := s.isRunningMutex
	isRunningMutex.Lock()
	defer isRunningMutex.Unlock()
	if s.isRunning {
		errChan := make(chan error, 1)
		s.registerCallbackChan <- registerCallbackEntry{token, callbacks, errChan}
		address := fmt.Sprintf("%s@%s", token, s.listener.Addr().String())
		return address, errChan, nil
	} else {
		return "", nil, fmt.Errorf("Server is not running; can't decode to %#v", callbacks)
	}
}

// Decode returns a server address that can be used by a test runner to
// stream tapj results.
func (s *Server) ExposeChannel(ch chan interface{}) (string, chan error, error) {
	// TODO(adamb) Fire off error handlers on callbacks if server shuts down without
	//    response.

	isRunningMutex := s.isRunningMutex
	isRunningMutex.Lock()
	defer isRunningMutex.Unlock()
	if s.isRunning {
		// Make new token
		token := randomToken(16)
		errChan := make(chan error, 1)
		s.registerChannelChan <- registerChannelEntry{token, ch, errChan}
		address := fmt.Sprintf("%s@%s", token, s.listener.Addr().String())
		return address, errChan, nil
	} else {
		return "", nil, fmt.Errorf("Server is not running; can't expose %#v", ch)
	}
}
