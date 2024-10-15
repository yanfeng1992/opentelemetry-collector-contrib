// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package tcpcheckreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/tcpcheckreceiver"

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/golden"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatatest/pmetrictest"
)

type Server struct {
	host     string
	port     string
	listener net.Listener
}

func newTCPServer(host string, port string) *Server {
	return &Server{
		host: host,
		port: port,
	}
}

func (server *Server) runTCPServer(t *testing.T) string {
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%s", server.host, server.port))
	require.NoError(t, err)
	server.listener = listener
	go func() {
		conn, err := listener.Accept()
		require.NoError(t, err)
		go handleRequest(conn)
	}()
	return listener.Addr().String()
}

func (server *Server) shutdown() {
	server.listener.Close()
}

func handleRequest(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return
		}
		fmt.Printf("Message incoming: %s", string(message))
		conn.Write([]byte("Message received.\n"))
		conn.Close()
	}
}

func TestTimeout(t *testing.T) {
	testCases := []struct {
		name     string
		deadline time.Time
		timeout  time.Duration
		want     time.Duration
	}{
		{
			name:     "timeout is shorter",
			deadline: time.Now().Add(time.Second),
			timeout:  time.Second * 2,
			want:     time.Second,
		},
		{
			name:     "deadline is shorter",
			deadline: time.Now().Add(time.Second * 2),
			timeout:  time.Second,
			want:     time.Second,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			to := timeout(tc.deadline, tc.timeout)
			if to < (tc.want-10*time.Millisecond) || to > tc.want {
				t.Fatalf("wanted time within 10 milliseconds: %s, got: %s", time.Second, to)
			}
		})
	}
}

func TestScraper(t *testing.T) {
	s := newTCPServer("127.0.0.1", "8080")
	endpoint := s.runTCPServer(t)
	defer s.shutdown()

	testCases := []struct {
		name     string
		filename string
		endpoint string
	}{
		{
			name:     "metrics_golden",
			filename: "metrics_golden.yaml",
			endpoint: endpoint,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expectedFile := filepath.Join("testdata", "expected_metrics", tc.filename)
			expectedMetrics, err := golden.ReadMetrics(expectedFile)
			require.NoError(t, err)
			f := NewFactory()
			cfg := f.CreateDefaultConfig().(*Config)
			cfg.ControllerConfig.CollectionInterval = 100 * time.Millisecond
			cfg.Endpoint = tc.endpoint

			settings := receivertest.NewNopSettings()

			scraper := newScraper(cfg, settings)
			require.NoError(t, scraper.start(context.Background(), componenttest.NewNopHost()), "failed starting scraper")

			actualMetrics, err := scraper.scrape(context.Background())
			require.NoError(t, err, "failed scrape")
			require.NoError(
				t,
				pmetrictest.CompareMetrics(
					expectedMetrics,
					actualMetrics,
					pmetrictest.IgnoreMetricValues("tcpcheck.duration"),
					pmetrictest.IgnoreTimestamp(),
					pmetrictest.IgnoreStartTimestamp(),
				),
			)
		})
	}
}
