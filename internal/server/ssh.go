package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gabe565.com/ascii-movie/internal/config"
	"gabe565.com/ascii-movie/internal/movie"
	"gabe565.com/ascii-movie/internal/player"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

type SSHServer struct {
	Server
}

func NewSSH(conf *config.Config, info *Info) SSHServer {
	server := SSHServer{
		Server: NewServer(conf, config.FlagPrefixSSH, info),
	}
	if len(server.conf.SSH.HostKeyPath) == 0 && len(server.conf.SSH.HostKeyPEM) == 0 {
		server.conf.SSH.HostKeyPath = []string{"$HOME/.ssh/ascii_movie_ed25519", "$HOME/.ssh/ascii_movie_rsa"}
	}

	return server
}

func (s *SSHServer) Listen(ctx context.Context, m *movie.Movie) error {
	s.Log.Info("Starting SSH server", "address", s.conf.SSH.Address)

	sshOptions := []ssh.Option{
		wish.WithAddress(s.conf.SSH.Address),
		wish.WithIdleTimeout(s.conf.IdleTimeout),
		wish.WithMaxTimeout(s.conf.MaxTimeout),
		wish.WithMiddleware(
			bubbletea.Middleware(s.Handler(m)),
			s.TrackStream,
		),
	}

	for _, pem := range s.conf.SSH.HostKeyPEM {
		sshOptions = append(sshOptions, wish.WithHostKeyPEM([]byte(pem)))
	}

	for _, path := range s.conf.SSH.HostKeyPath {
		if strings.Contains(path, "$HOME") {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			path = strings.ReplaceAll(path, "$HOME", home)
		}
		sshOptions = append(sshOptions, wish.WithHostKeyPath(path))
	}

	server, err := wish.NewServer(sshOptions...)
	if err != nil {
		return err
	}

	for _, signer := range server.HostSigners {
		s.Log.Debug("Using host key",
			"type", signer.PublicKey().Type(),
			"fingerprint", gossh.FingerprintSHA256(signer.PublicKey()),
		)
	}

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		s.Info.sshListeners++
		defer func() {
			s.Info.sshListeners--
		}()

		if err = server.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			return fmt.Errorf("failed to start server: %w", err)
		}
		return nil
	})

	group.Go(func() error {
		<-ctx.Done()
		s.Log.Info("Stopping SSH server")
		defer s.Log.Info("Stopped SSH server")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		return server.Shutdown(shutdownCtx)
	})

	return group.Wait()
}

func (s *SSHServer) Handler(m *movie.Movie) bubbletea.Handler {
	return func(session ssh.Session) (tea.Model, []tea.ProgramOption) {
		remoteIP := RemoteIP(session.RemoteAddr())
		logger := s.Log.With(
			"remoteIP", remoteIP,
			"user", session.User(),
		)

		p := player.NewSimplePlayer(m, logger, session)
		go func() {
			<-session.Context().Done()
		}()
		go func() {
			if err := p.Play(session.Context()); err != nil {
				logger.Error("Movie playback failed", "error", err)
			}
			session.Close()
		}()
		return &noopModel{}, []tea.ProgramOption{}
	}
}

type noopModel struct{}

func (n *noopModel) Init() tea.Cmd { return tea.Quit }
func (n *noopModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return n, tea.Quit }
func (n *noopModel) View() string { return "" }

func (s *SSHServer) TrackStream(handler ssh.Handler) ssh.Handler {
	return func(session ssh.Session) {
		remoteIP := RemoteIP(session.RemoteAddr())
		id, err := s.Info.StreamConnect("ssh", remoteIP)
		if err != nil {
			s.Log.Error("Failed to begin stream",
				"remoteIP", remoteIP,
				"user", session.User(),
			)
			_, _ = session.Write([]byte(ErrorText(err) + "\n"))
			return
		}
		defer s.Info.StreamDisconnect(id)
		handler(session)
	}
}
