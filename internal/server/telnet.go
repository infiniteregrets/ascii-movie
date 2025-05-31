package server

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"gabe565.com/ascii-movie/internal/config"
	"gabe565.com/ascii-movie/internal/movie"
	"gabe565.com/ascii-movie/internal/player"
	"gabe565.com/ascii-movie/internal/server/idleconn"
)

type TelnetServer struct {
	Server
}

func NewTelnet(conf *config.Config, info *Info) TelnetServer {
	return TelnetServer{Server: NewServer(conf, config.FlagPrefixTelnet, info)}
}

func (s *TelnetServer) Listen(ctx context.Context, m *movie.Movie) error {
	s.Log.Info("Starting telnet server", "address", s.conf.Telnet.Address)

	listen, err := net.Listen("tcp", s.conf.Telnet.Address)
	if err != nil {
		return err
	}
	defer func(listen net.Listener) {
		_ = listen.Close()
	}(listen)

	var serveGroup sync.WaitGroup
	serveCtx, serveCancel := context.WithCancel(context.Background())
	defer serveCancel()

	go func() {
		s.Info.telnetListeners++
		defer func() {
			s.Info.telnetListeners--
		}()

		for {
			conn, err := listen.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					s.Log.Error("Failed to accept connection", "error", err)
					continue
				}
			}

			serveGroup.Add(1)
			go func() {
				defer serveGroup.Done()
				ctx, cancel := context.WithCancel(serveCtx)
				conn = idleconn.New(conn, s.conf.IdleTimeout, s.conf.MaxTimeout, cancel)
				s.Handler(ctx, conn, m)
			}()
		}
	}()

	<-ctx.Done()
	s.Log.Info("Stopping Telnet server")
	defer s.Log.Info("Stopped Telnet server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	go func() {
		serveCancel()
		serveGroup.Wait()
		shutdownCancel()
	}()
	<-shutdownCtx.Done()

	return listen.Close()
}

func (s *TelnetServer) Handler(ctx context.Context, conn net.Conn, m *movie.Movie) {
	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)

	remoteIP := RemoteIP(conn.RemoteAddr())
	logger := s.Log.With("remoteIP", remoteIP)

	id, err := s.Info.StreamConnect("telnet", remoteIP)
	if err != nil {
		logger.Error("Failed to begin stream", "error", err)
		_, _ = conn.Write([]byte(ErrorText(err) + "\n"))
		return
	}
	defer s.Info.StreamDisconnect(id)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	p := player.NewSimplePlayer(m, logger, conn)
	if err := p.Play(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Movie playback failed", "error", err)
	}
}
