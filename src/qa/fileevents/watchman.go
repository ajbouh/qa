package fileevents

import (
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"qa/tapjio"
	"sync"
	"syscall"
	"time"
)

type SubscribeConfirmEvent struct {
	Version   string `json:"version"`
	Subscribe string `json:"subscribe"`
}

type SubscribeRequestEvent struct {
	Name   string
	Root   string
	Expr   interface{}
	Notify chan error
}

type Event struct {
	Version      string `json:"version"`
	Subscription string `json:"subscription"`
	Clock        string `json:"clock"`
	Files        []File `json:"files"`
	Root         string `json:"root"`
}

type File struct {
	Name   string `json:"name"`
	New    bool   `json:"new"`
	Exists bool   `json:"exists"`
}

type Subscription struct {
	Events                 chan *Event
	subscribeRequestOutbox chan *SubscribeRequestEvent
	closed                 bool
	closer                 io.Closer
	root                   string
	name                   string
}

type EventContentChangeFilter struct {
	digestByPath      map[string]tapjio.FileDigest
	digestByPathMutex *sync.Mutex
	newHash           func() hash.Hash
}

func NewEventContentChangeFilter(newHash func() hash.Hash) *EventContentChangeFilter {
	return &EventContentChangeFilter{
		digestByPath:      map[string]tapjio.FileDigest{},
		digestByPathMutex: &sync.Mutex{},
		newHash:           newHash,
	}
}

func (e *EventContentChangeFilter) digestFile(path string) (tapjio.FileDigest, error) {
	f, err := os.Open(path)
	var digest tapjio.FileDigest
	if err != nil {
		return digest, err
	}
	defer f.Close()

	digester := e.newHash()

	if _, err := io.Copy(digester, f); err != nil {
		return digest, err
	}

	digest = tapjio.FileDigestFromHash(digester)
	return digest, nil
}

func (e *EventContentChangeFilter) SetDigest(path string, digest tapjio.FileDigest) {
	mutex := e.digestByPathMutex
	mutex.Lock()
	defer mutex.Unlock()
	e.digestByPath[path] = digest
}

func (e *EventContentChangeFilter) FilterContentChanges(incomingEvents chan *Event) chan *Event {
	filteredEvents := make(chan *Event)

	go func() {
		defer close(filteredEvents)

		for event := range incomingEvents {
			changedFiles := make([]File, 0, len(event.Files))
			for _, file := range event.Files {
				path := filepath.Join(event.Root, file.Name)

				e.digestByPathMutex.Lock()
				prevDigest, wasPresent := e.digestByPath[path]
				e.digestByPathMutex.Unlock()
				if file.Exists {
					digest, err := e.digestFile(path)
					if err != nil {
						if os.IsNotExist(err) {
							// We just expierenced a race. Update the file to not exist.
							file.Exists = false
							file.New = false
						} else {
							fmt.Fprintf(os.Stderr, "Error digesting file %s: %#v\n", path, err.Error())
							changedFiles = append(changedFiles, file)
						}
					} else {
						if digest != prevDigest {
							if !wasPresent {
								file.New = true
							} else {
								file.New = false
							}
							e.digestByPathMutex.Lock()
							e.digestByPath[path] = digest
							e.digestByPathMutex.Unlock()
							changedFiles = append(changedFiles, file)
						}
					}
				}

				// Since we might experience a race above, reevaluate instead of using else
				if !file.Exists {
					if wasPresent {
						e.digestByPathMutex.Lock()
						delete(e.digestByPath, path)
						e.digestByPathMutex.Unlock()
						changedFiles = append(changedFiles, file)
					}
				}
			}

			if len(changedFiles) > 0 {
				event.Files = changedFiles
				filteredEvents <- event
			}
		}
	}()

	return filteredEvents
}

func newSubscription(conn *WatchmanConn, root, name string) *Subscription {
	return &Subscription{
		Events:                 conn.EventInbox,
		subscribeRequestOutbox: conn.SubscribeRequestOutbox,
		closer:                 conn.Closer,
		root:                   root,
		name:                   name,
	}
}

func (s *Subscription) Update(expr interface{}) error {
	notify := make(chan error, 1)
	req := &SubscribeRequestEvent{
		Root:   s.root,
		Name:   s.name,
		Expr:   expr,
		Notify: notify,
	}
	s.subscribeRequestOutbox <- req

	err := <-notify

	if err != nil {
		return err
	}

	return nil
}

func (s *Subscription) Close() error {
	if s.closed {
		return nil
	}

	s.closed = true
	close(s.subscribeRequestOutbox)
	return s.closer.Close()
}

type Watcher interface {
	Subscribe(root string, name string, expr interface{}) (*Subscription, error)
	Close() error
}

func tolerantDial(sockname string) (*net.UnixConn, error) {
	for {
		conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sockname, Net: "unix"})
		if err == nil {
			return conn, err
		}

		opErr, ok := err.(*net.OpError)
		if !ok {
			return conn, err
		}

		syscallError, ok := opErr.Err.(*os.SyscallError)
		if !ok {
			return conn, err
		}

		switch syscallError.Err {
		case syscall.ECONNREFUSED:
			time.Sleep(50 * time.Millisecond)
			continue
		case syscall.ENOENT:
			time.Sleep(50 * time.Millisecond)
			continue
		case syscall.ENODATA:
			time.Sleep(50 * time.Millisecond)
			continue
		}

		fmt.Fprintf(os.Stderr, "Saw error %#v\n", syscallError)
		return conn, err
	}
}

func StartWatchman(sockname string) (Watcher, error) {
	cmd := exec.Command(
		"watchman",
		"--no-save-state",
		"--foreground",
		"--sockname", sockname,
		"--logfile", "/dev/stdout")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		return nil, err
	}

	return &WatchmanService{process: cmd.Process, sockname: sockname}, nil
}

type WatchmanService struct {
	process  *os.Process
	sockname string
}

func (w *WatchmanService) Close() error {
	err := w.process.Kill()
	if err != nil {
		_, err = w.process.Wait()
	}

	return err
}

type WatchmanConn struct {
	EventInbox             chan *Event
	SubscribeConfirmInbox  chan *SubscribeConfirmEvent
	SubscribeRequestOutbox chan *SubscribeRequestEvent
	Closer                 io.Closer
}

func (w *WatchmanService) Connect() (*WatchmanConn, error) {
	// Open unix socket and keep it open for the future
	conn, err := tolerantDial(w.sockname)
	if err != nil {
		return nil, err
	}

	subscribeConfirmChan := make(chan *SubscribeConfirmEvent, 1)
	subscribeRequestChan := make(chan *SubscribeRequestEvent)
	encoder := json.NewEncoder(conn)

	go func(confirmChan chan *SubscribeConfirmEvent,
		requestChan chan *SubscribeRequestEvent) {
		defer conn.CloseRead()

		awaiting := map[string]*SubscribeRequestEvent{}
	Loop:
		for {
			select {
			case s, ok := <-requestChan:
				if !ok {
					requestChan = nil
					break Loop
				}

				awaiting[s.Name] = s
				message := []interface{}{"subscribe", s.Root, s.Name, s.Expr}
				err = encoder.Encode(message)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding message %#v: %#v\n", message, err.Error())
				}
			case subscribeConfirm, ok := <-confirmChan:
				if !ok {
					confirmChan = nil
					break Loop
				}
				s, ok := awaiting[subscribeConfirm.Subscribe]
				if ok {
					delete(awaiting, subscribeConfirm.Subscribe)
				}
				close(s.Notify)
			}
		}

		for _, s := range awaiting {
			s.Notify <- fmt.Errorf("Subscription closed without acknowledging subscription")
			close(s.Notify)
		}

		if requestChan != nil {
			for _ = range requestChan {
				// Drain requestChan...
			}
		}
	}(subscribeConfirmChan, subscribeRequestChan)

	eventChan := make(chan *Event, 1)
	decoder := json.NewDecoder(conn)
	go func() {
		defer close(eventChan)
		defer close(subscribeConfirmChan)
		defer conn.CloseWrite()

		for {
			var raw json.RawMessage
			err := decoder.Decode(&raw)
			if err == io.EOF {
				break
			}

			if err != nil {
				_, ok := err.(*net.OpError)
				if ok {
					break
				}

				fmt.Fprintf(os.Stderr, "Error decoding message: %#v\n", err.Error())
				break
			}

			var event Event
			if err = json.Unmarshal(raw, &event); err != nil {
				fmt.Fprintf(os.Stderr, "Error decoding event: %#v\n", err.Error())
				break
			}

			if event.Subscription == "" {
				var subEv SubscribeConfirmEvent
				if err := json.Unmarshal(raw, &subEv); err != nil {
					fmt.Fprintf(os.Stderr, "Error decoding subscribe: %#v\n", err.Error())
					break
				}
				subscribeConfirmChan <- &subEv
			} else {
				eventChan <- &event
			}
		}
	}()

	return &WatchmanConn{
		EventInbox:             eventChan,
		SubscribeConfirmInbox:  subscribeConfirmChan,
		SubscribeRequestOutbox: subscribeRequestChan,
		Closer:                 conn,
	}, nil
}

func (w *WatchmanService) Subscribe(root string, name string, expr interface{}) (*Subscription, error) {
	conn, err := w.Connect()
	if err != nil {
		return nil, err
	}

	sub := newSubscription(conn, root, name)
	sub.Update(expr)

	return sub, nil
}
