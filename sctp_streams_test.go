// Copyright 2019 Wataru Ishida. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sctp

import (
	"fmt"
	"io"
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

const (
	STREAM_TEST_CLIENTS = 128
	STREAM_TEST_STREAMS = 11
)

func TestStreams(t *testing.T) {
	randomStr := func(strlen int) string {
		const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		result := make([]byte, strlen)
		for i := range result {
			result[i] = chars[rand.IntN(len(chars))]
		}
		return string(result)
	}

	addr, _ := ResolveSCTPAddr("sctp", "127.0.0.1:0")
	ln, err := ListenSCTPExt("sctp", addr, InitMsg{NumOstreams: STREAM_TEST_STREAMS, MaxInstreams: STREAM_TEST_STREAMS})
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr = ln.Addr().(*SCTPAddr)
	t.Logf("Listen on %s", ln.Addr())

	stopping := make(chan struct{})
	acceptDone := make(chan struct{})
	var clients, servers sync.WaitGroup
	var connMu sync.Mutex
	var connections []*SCTPConn
	track := func(conn *SCTPConn) bool {
		connMu.Lock()
		defer connMu.Unlock()
		select {
		case <-stopping:
			conn.Close()
			return false
		default:
			connections = append(connections, conn)
			return true
		}
	}
	isStopping := func() bool {
		select {
		case <-stopping:
			return true
		default:
			return false
		}
	}
	defer func() {
		close(stopping)
		ln.Close()
		<-acceptDone
		connMu.Lock()
		active := append([]*SCTPConn(nil), connections...)
		connMu.Unlock()
		for _, conn := range active {
			conn.Close()
		}
		clients.Wait()
		servers.Wait()
	}()

	go func() {
		defer close(acceptDone)
		for {
			sconn, err := ln.AcceptSCTP()
			if err != nil {
				if !isStopping() {
					t.Errorf("failed to accept: %v", err)
				}
				return
			}
			if !track(sconn) {
				return
			}

			servers.Go(func() {
				defer sconn.Close()
				if err := sconn.SubscribeEvents(SCTP_EVENT_DATA_IO); err != nil {
					if !isStopping() {
						t.Errorf("failed to subscribe on server: %v", err)
					}
					return
				}
				for {
					buf := make([]byte, 512)
					n, info, err := sconn.SCTPRead(buf)
					if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
						if !isStopping() {
							t.Errorf("server read failed: %v", err)
						}
						return
					}
					if n == 0 {
						return
					}
					t.Logf("server read: info: %+v, payload: %s", info, string(buf[:n]))
					n, err = sconn.SCTPWrite(buf[:n], info)
					if err != nil {
						if !isStopping() {
							t.Error(err)
						}
						return
					}
				}
			})
		}
	}()

	for test := range STREAM_TEST_CLIENTS {
		clients.Go(func() {
			conn, err := DialSCTPExt(
				"sctp", nil, addr, InitMsg{NumOstreams: STREAM_TEST_STREAMS, MaxInstreams: STREAM_TEST_STREAMS})
			if err != nil {
				t.Errorf("failed to dial address %s, test #%d: %v", addr.String(), test, err)
				return
			}
			defer conn.Close()
			if !track(conn) {
				return
			}
			if err := conn.SubscribeEvents(SCTP_EVENT_DATA_IO); err != nil {
				t.Errorf("failed to subscribe on client: %v", err)
				return
			}
			for ppid := range uint16(STREAM_TEST_STREAMS) {
				info := &SndRcvInfo{
					Stream: uint16(ppid),
					PPID:   uint32(ppid),
				}
				randomLen := rand.IntN(255)
				text := fmt.Sprintf("Test %s ***\n\t\t%d %d ***", randomStr(randomLen), test, ppid)
				n, err := conn.SCTPWrite([]byte(text), info)
				if err != nil {
					t.Errorf("failed to write %s, len: %d, err: %v, bytes written: %d", text, len(text), err, n)
					return
				}
				rn := 0
				cn := 0
				buf := make([]byte, 512)
				for {
					cn, info, err = conn.SCTPRead(buf[rn:])
					if err != nil {
						if err == io.EOF || err == io.ErrUnexpectedEOF {
							rn += cn
							break
						}
						t.Errorf("failed to read: %v", err)
						return
					}
					if info == nil {
						t.Error("missing stream info")
						return
					}
					if info.Stream != ppid {
						t.Errorf("Mismatched PPIDs: %d != %d", info.Stream, ppid)
						return
					}
					rn += cn
					if rn >= n {
						break
					}
				}
				rtext := string(buf[:rn])
				if rtext != text {
					t.Errorf("Mismatched payload: %s != %s", rtext, text)
					return
				}
			}
		})
	}
	clientsDone := make(chan struct{})
	go func() {
		clients.Wait()
		close(clientsDone)
	}()
	select {
	case <-clientsDone:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out")
	}
}
