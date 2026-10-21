package serve_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core"
	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/ipc/protocol"
	"github.com/sorokin-vladimir/tele/internal/ipc/serve"
	"github.com/sorokin-vladimir/tele/internal/ipc/wire"
)

var hello = wire.Hello{Protocol: wire.ProtocolVersion, Binary: "dev", Config: "/tmp/tele/config.yml"}

// helloHost is a host that says hello and holds no settings.
type helloHost struct{}

func (helloHost) Hello() wire.Hello                        { return hello }
func (helloHost) Settings() wire.HostSettings              { return wire.HostSettings{} }
func (helloHost) SetSetting(wire.SetParams) wire.SetResult { return wire.SetResult{} }
func (helloHost) Reload() wire.ReloadResult                { return wire.ReloadResult{} }
func (helloHost) Status() wire.HostStatus                  { return wire.HostStatus{} }
func (helloHost) Restart() (wire.RestartResult, func())    { return wire.RestartResult{}, nil }
func (helloHost) Uninstall() wire.UninstallResult          { return wire.UninstallResult{} }

// restartingHost is a service that, asked to restart, stops: it tells the
// client so and waits until the connection is gone, as a host does when it
// stops before the client has read the answer.
type restartingHost struct {
	helloHost
	att  *core.Attachment
	gone <-chan struct{}
}

func (h restartingHost) Restart() (wire.RestartResult, func()) {
	return wire.RestartResult{}, func() {
		h.att.Tell(core.HostStopping{Reason: core.StopRestart})
		<-h.gone
	}
}

// rawClient is a client the test plays by hand, against a host serving a
// real attachment.
type rawClient struct {
	conn   net.Conn
	dec    *wire.Decoder
	enc    *wire.Encoder
	served chan error
}

func serveRaw(t *testing.T) rawClient {
	t.Helper()
	client, host := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	served := make(chan error, 1)
	go func() { served <- serve.Conn(context.Background(), host, core.NewAttachment(), helloHost{}) }()
	return rawClient{conn: client, dec: wire.NewDecoder(client), enc: wire.NewEncoder(client), served: served}
}

// greet says hello the way a client does, and reads the host's.
func (c rawClient) greet(t *testing.T) {
	t.Helper()
	raw, err := json.Marshal(hello)
	require.NoError(t, err)
	require.NoError(t, c.enc.Encode(wire.Request(1, wire.MethodHello, raw)))
	f, err := c.dec.Decode()
	require.NoError(t, err)
	var got wire.Hello
	require.NoError(t, json.Unmarshal(f.Result, &got))
	assert.Equal(t, hello, got)
}

// refused reads the error the host goes with: one about the connection, so
// addressed to no request, naming the protocol as what broke.
func (c rawClient) refused(t *testing.T) {
	t.Helper()
	f, err := c.dec.Decode()
	require.NoError(t, err)
	require.NotNil(t, f.Error, "%+v", f)
	assert.Equal(t, uint64(0), f.ID)
	assert.Equal(t, "protocol", f.Error.Op)
	select {
	case err := <-c.served:
		assert.ErrorIs(t, err, wire.ErrProtocol)
	case <-time.After(2 * time.Second):
		t.Fatal("the host kept the connection")
	}
}

// A change to a host setting that names no setting is not something the host
// can answer, and nothing after it is trusted (#315).
func TestAHostRefusesASetThatNamesNoSetting(t *testing.T) {
	c := serveRaw(t)
	c.greet(t)

	require.NoError(t, c.enc.Encode(wire.Request(2, wire.MethodSet, json.RawMessage(`{"value":1}`))))

	c.refused(t)
}

func TestAHostRefusesALineThatIsNoFrame(t *testing.T) {
	c := serveRaw(t)
	c.greet(t)

	_, err := c.conn.Write([]byte("this is no frame\n"))
	require.NoError(t, err)

	c.refused(t)
}

// Nothing is said before hello: the host does not know yet whether the two
// understand each other.
func TestAHostRefusesACallBeforeHello(t *testing.T) {
	c := serveRaw(t)
	params, err := protocol.Calls["Chats"].Params()
	require.NoError(t, err)

	require.NoError(t, c.enc.Encode(wire.Request(1, "Chats", params)))

	c.refused(t)
}

// A service asked to restart answers before it stops: the client that asked
// hears that it agreed, then that it is stopping (#183).
func TestAServiceAnswersARestartBeforeItStops(t *testing.T) {
	client, conn := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	gone := make(chan struct{})
	att := core.NewAttachment()
	go func() { _ = serve.Conn(context.Background(), conn, att, restartingHost{att: att, gone: gone}) }()
	c := rawClient{conn: client, dec: wire.NewDecoder(client), enc: wire.NewEncoder(client)}
	c.greet(t)

	require.NoError(t, c.enc.Encode(wire.Request(2, wire.MethodRestart, json.RawMessage(`null`))))

	var frames []wire.Frame
	for {
		f, err := c.dec.Decode()
		if err != nil {
			break
		}
		frames = append(frames, f)
	}
	close(gone)
	require.Len(t, frames, 2, "%+v", frames)
	assert.Equal(t, uint64(2), frames[0].ID, "the answer comes first")
	assert.Nil(t, frames[0].Error)
	assert.NotEmpty(t, frames[1].Event, "then the host says it is stopping")
}

// A subscription id that does not grow could be read as one already
// replaced, so the client that sent it is speaking another protocol.
func TestAHostRefusesASubscriptionIDThatDoesNotGrow(t *testing.T) {
	c := serveRaw(t)
	c.greet(t)
	call := protocol.Calls[protocol.MethodSubscribe]
	second, err := call.Params(project.SubID(2), project.AccountWindow{})
	require.NoError(t, err)
	first, err := call.Params(project.SubID(1), project.AccountWindow{})
	require.NoError(t, err)

	require.NoError(t, c.enc.Encode(wire.Request(2, protocol.MethodSubscribe, second)))
	f, err := c.dec.Decode()
	require.NoError(t, err)
	require.Nil(t, f.Error, "the first one is taken")
	require.NoError(t, c.enc.Encode(wire.Request(3, protocol.MethodSubscribe, first)))

	c.refused(t)
}
