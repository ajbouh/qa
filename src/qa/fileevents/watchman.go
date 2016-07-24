package fileevents

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type Event struct {
	Version      string `json:"version"`
	Clock        string `json:"clock"`
	Files        []File `json:"files"`
	Root         string `json:"root"`
	Subscription string `json:"subscription"`
}

type File struct {
	Name   string `json:"name"`
	New    bool   `json:"new"`
	Exists bool   `json:"exists"`
}

type Subscription struct {
	Events chan *Event
	closer io.Closer
}

func (s *Subscription) Close() error {
	return s.closer.Close()
}

type Watcher interface {
	Subscribe(root string, name string, expr interface{}) (*Subscription, error)
	Close() error
}

func tolerantDial(sockname string) (net.Conn, error) {
	for {
		conn, err := net.Dial("unix", sockname)
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

func (w *WatchmanService) Subscribe(root string, name string, expr interface{}) (*Subscription, error) {
	// Open unix socket and keep it open for the future
	conn, err := tolerantDial(w.sockname)
	if err != nil {
		return nil, err
	}

	encoder := json.NewEncoder(conn)
	message := []interface{}{"subscribe", root, name, expr}
	err = encoder.Encode(message)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(conn)
	// Expect response:
	// {
	//   "version":   "1.6",
	//   "subscribe": "mysubscriptionname"
	// }
	var response interface{}
	err = decoder.Decode(&response)
	if err != nil {
		return nil, err
	}

	c := make(chan *Event, 1)
	go func() {
		defer close(c)
		defer conn.Close()

		for {
			var event Event
			err := decoder.Decode(&event)
			if err != nil {
				break
			}
			c <- &event
		}
	}()

	return &Subscription{Events: c, closer: conn}, nil
}
