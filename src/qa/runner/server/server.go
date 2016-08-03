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
	token string
	ch    chan interface{}
}

type Server struct {
	quitChan      chan struct{}
	listenAddress string
	listener      net.Listener

	cancelChan           chan string
	registerCallbackChan chan registerCallbackEntry
	visitorEntries       map[string]registerCallbackEntry

	registerChannelChan chan registerChannelEntry
	exposedChannels     map[string]chan interface{}

	isRunningMutex *sync.Mutex
	isRunning      bool
}

type acceptConnTokenEvent struct {
	token  string
	conn   net.Conn
	reader *bufio.Reader
}

func (s *Server) Close() error {
	close(s.quitChan)
	return s.listener.Close()
}

func Listen(netProto, listenAddress string) (*Server, error) {
	fmt.Fprintln(os.Stderr, "Listening on", netProto, listenAddress)
	listener, err := net.Listen(netProto, listenAddress)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error ", err)
		return nil, err
	}

	return &Server{
		quitChan:             make(chan struct{}),
		isRunning:            false,
		isRunningMutex:       &sync.Mutex{},
		listener:             listener,
		cancelChan:           make(chan string),
		registerCallbackChan: make(chan registerCallbackEntry),
		visitorEntries:       make(map[string]registerCallbackEntry),
		exposedChannels:      make(map[string]chan interface{}),
		registerChannelChan:  make(chan registerChannelEntry),
	}, nil
}

func (s *Server) Run() error {
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

	isRunningMutex.Lock()
	s.isRunning = true
	isRunningMutex.Unlock()

	for {
		select {
		case err := <-errChan:
			fmt.Fprintf(os.Stderr, "Fatal error in server: %v\n", err)
			s.listener.Close()
			return err
		case address := <-s.cancelChan:
			entry, ok := s.visitorEntries[address]
			if ok {
				delete(s.visitorEntries, address)
				reason := errors.New("Canceled")
				err := entry.visitor.End(reason)
				if err != nil {
					entry.errChan <- err
				} else {
					entry.errChan <- reason
				}
				close(entry.errChan)
			}

			_, ok = s.exposedChannels[address]
			if ok {
				delete(s.exposedChannels, address)
			}
		case entry := <-s.registerCallbackChan:
			s.visitorEntries[entry.token] = entry
		case entry := <-s.registerChannelChan:
			s.exposedChannels[entry.token] = entry.ch
		case conn, ok := <-acceptConnChan:
			if !ok {
				break
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
		case accept := <-acceptTokenChan:
			// Token is one-time use.
			token := accept.token
			entry, ok := s.visitorEntries[token]
			if ok {
				delete(s.visitorEntries, token)

				go func(decoder *json.Decoder, closer io.Closer, entry registerCallbackEntry) {
					defer close(entry.errChan)
					defer closer.Close()
					err := tapjio.Decode(decoder, entry.visitor)
					if err != nil {
						entry.errChan <- err
					}
				}(json.NewDecoder(accept.reader), accept.conn, entry)
				break
			}

			exposed, ok := s.exposedChannels[token]
			if ok {
				delete(s.exposedChannels, token)
				errChanWg.Add(1)
				go func() {
					defer errChanWg.Done()
					conn := accept.conn
					defer conn.Close()

					encoder := json.NewEncoder(conn)

					for value := range exposed {
						if err := encoder.Encode(value); err != nil {
							errChan <- err
							return
						}
					}
				}()
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
		return "", nil, errors.New("Server is not running")
	}
}

// Decode returns a server address that can be used by a test runner to
// stream tapj results.
func (s *Server) ExposeChannel(ch chan interface{}) (string, error) {
	// TODO(adamb) Fire off error handlers on callbacks if server shuts down without
	//    response.

	isRunningMutex := s.isRunningMutex
	isRunningMutex.Lock()
	defer isRunningMutex.Unlock()
	if s.isRunning {
		// Make new token
		token := randomToken(16)
		s.registerChannelChan <- registerChannelEntry{token, ch}
		address := fmt.Sprintf("%s@%s", token, s.listener.Addr().String())
		return address, nil
	} else {
		return "", errors.New("Server is not running")
	}
}
