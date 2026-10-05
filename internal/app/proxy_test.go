package app

import (
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/sorokin-vladimir/tele/internal/proxy"
)

const testSecret = "dd0123456789abcdef0123456789abcdef"

// listening returns the address of a listener that stays open for the test, and
// closedPort one that nobody is on.
func listening(t *testing.T) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return split(t, ln.Addr().String())
}

func closedPort(t *testing.T) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host, port = split(t, ln.Addr().String())
	require.NoError(t, ln.Close())
	return host, port
}

func split(t *testing.T, addr string) (host string, port int) {
	t.Helper()
	host, p, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err = strconv.Atoi(p)
	require.NoError(t, err)
	return host, port
}

func logged(t *testing.T) (*zap.Logger, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	return zap.New(core), logs
}

// routeOf is the route a proxy section names.
func routeOf(t *testing.T, cfg proxy.Config) proxy.Route {
	t.Helper()
	route, err := proxy.Parse(cfg)
	require.NoError(t, err)
	return route
}

// A route that dials nobody has nothing to probe, so it is ready without
// touching the network.
func TestCheckRoute_NoProxy(t *testing.T) {
	t.Setenv("ALL_PROXY", "")
	log, logs := logged(t)

	err := checkRoute(routeOf(t, proxy.Config{Type: proxy.TypeDirect}), "/tmp/config.yml", log)
	require.NoError(t, err)
	assert.Equal(t, 1, logs.FilterMessage("telegram route").Len(), "the route is named once")
}

// The proxy is dialled before anything is drawn. Without this the interface
// comes up and sits on a connecting screen while gotd reconnects forever, and
// the reason reaches the person only when they quit.
func TestCheckRoute_RefusesAProxyThatIsNotAnswering(t *testing.T) {
	host, port := closedPort(t)
	log, _ := logged(t)

	err := checkRoute(routeOf(t, proxy.Config{
		Type: proxy.TypeSOCKS5, Server: host, Port: port,
	}), "/tmp/config.yml", log)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not answering")
	assert.Contains(t, err.Error(), net.JoinHostPort(host, strconv.Itoa(port)), "what was dialled")
	assert.Contains(t, err.Error(), "/tmp/config.yml", "where it is configured")
	assert.Contains(t, err.Error(), "proxy.type: direct", "and how to connect without it")
}

func TestCheckRoute_AProxyThatAnswersIsReady(t *testing.T) {
	host, port := listening(t)
	log, logs := logged(t)

	err := checkRoute(routeOf(t, proxy.Config{
		Type: proxy.TypeMTProto, Server: host, Port: port, Secret: testSecret,
	}), "/tmp/config.yml", log)

	require.NoError(t, err)
	assert.Equal(t, 1, logs.FilterMessage("telegram route").Len())
}

// This line is the one tele writes about the connection, and it ends up in
// issues. It names the address, because a wrong port has to be findable, and
// the kind of secret, because that is what people get wrong - never the secret
// itself and never the password.
func TestCheckRoute_LogsTheRouteAndNoCredentials(t *testing.T) {
	host, port := listening(t)
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	t.Run("mtproto", func(t *testing.T) {
		log, logs := logged(t)
		err := checkRoute(routeOf(t, proxy.Config{
			Type: proxy.TypeMTProto, Server: host, Port: port, Secret: testSecret,
		}), "/tmp/config.yml", log)
		require.NoError(t, err)

		line := logs.FilterMessage("telegram route").All()[0].ContextMap()["proxy"].(string)
		assert.Contains(t, line, addr)
		assert.Contains(t, line, "secret: dd")
		assert.NotContains(t, line, testSecret)
	})

	t.Run("socks5", func(t *testing.T) {
		log, logs := logged(t)
		err := checkRoute(routeOf(t, proxy.Config{
			Type: proxy.TypeSOCKS5, Server: host, Port: port, Username: "me", Password: "hunter2",
		}), "/tmp/config.yml", log)
		require.NoError(t, err)

		line := logs.FilterMessage("telegram route").All()[0].ContextMap()["proxy"].(string)
		assert.Contains(t, line, addr)
		assert.Contains(t, line, "auth: yes")
		assert.NotContains(t, line, "hunter2")
	})
}
