package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"qa/tapjio"
	"strings"
)

type registerCallbackEntry struct {
	token   string
	visitor tapjio.Visitor
}

type registerChannelEntry struct {
	token string
	ch    chan interface{}
}

type Server struct {
	listenAddress string
	listener      net.Listener

	registerCallbackChan chan registerCallbackEntry
	visitors             map[string]tapjio.Visitor

	registerChannelChan chan registerChannelEntry
	exposedChannels     map[string]chan interface{}
}

type acceptResult struct {
	token string
	conn  *net.Conn
	err   error
}

func (s *Server) Close() error {
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
		listener:             listener,
		registerCallbackChan: make(chan registerCallbackEntry),
		visitors:             make(map[string]tapjio.Visitor),
		exposedChannels:      make(map[string]chan interface{}),
		registerChannelChan:  make(chan registerChannelEntry),
	}, nil
}

func (s *Server) Run() error {
	acceptChan := make(chan acceptResult)
	go func() {
		defer close(acceptChan)

		for {
			conn, err := s.listener.Accept()
			if err != nil {
				break
			}

			go func() {
				bufReader := bufio.NewReader(conn)
				token, err := bufReader.ReadString('\n')
				if err != nil {
					log.Fatal(err)
					return
				}
				token = strings.Trim(token, "\n")
				acceptChan <- acceptResult{token, &conn, err}
			}()
		}
	}()

	for acceptChan != nil {
		select {
		case entry := <-s.registerCallbackChan:
			// fmt.Fprintln(os.Stderr, "Registered", entry)
			s.visitors[entry.token] = entry.visitor
		case entry := <-s.registerChannelChan:
			// fmt.Fprintln(os.Stderr, "Registered", entry)
			s.exposedChannels[entry.token] = entry.ch
		case accept, ok := <-acceptChan:
			if !ok {
				acceptChan = nil
			}

			if accept.err != nil {
				return accept.err
			}

			// Token is one-time use.
			visitor, ok := s.visitors[accept.token]
			if ok {
				// fmt.Fprintln(os.Stderr, "Accepted", visitors, ok)
				delete(s.visitors, accept.token)

				go func() {
					tapjio.Decode(*accept.conn, visitor)
				}()
			}

			exposed, ok := s.exposedChannels[accept.token]
			if ok {
				// fmt.Fprintln(os.Stderr, "Accepted", exposed, ok)
				delete(s.exposedChannels, accept.token)
				go func() {
					conn := *accept.conn
					defer conn.Close()

					for value := range exposed {
						var s []byte
						s, err := json.Marshal(value)
						if err != nil {
							fmt.Fprintln(os.Stderr, "error marshalling value:", value)
							return
						}

						fmt.Fprintln(*accept.conn, string(s))
					}
				}()
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

// Decode returns a server address that can be used by a test runner to
// stream tapj results.
func (s *Server) Decode(callbacks tapjio.Visitor) string {
	// TODO(adamb) Fire off error handlers on callbacks if server shuts down without
	//    response.
	// Make new token
	token := randomToken(16)
	s.registerCallbackChan <- registerCallbackEntry{token, callbacks}

	address := fmt.Sprintf("%s@%s", token, s.listener.Addr().String())
	// fmt.Fprintln(os.Stderr, "Will decode address", address)

	return address
}

// Decode returns a server address that can be used by a test runner to
// stream tapj results.
func (s *Server) ExposeChannel(ch chan interface{}) string {
	// TODO(adamb) Fire off error handlers on callbacks if server shuts down without
	//    response.
	// Make new token
	token := randomToken(16)
	s.registerChannelChan <- registerChannelEntry{token, ch}

	address := fmt.Sprintf("%s@%s", token, s.listener.Addr().String())
	// fmt.Fprintln(os.Stderr, "Will expose channel at address", ch, address)

	return address
}
