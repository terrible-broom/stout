package isolate

import (
	"io"

	"golang.org/x/net/context"
)

// Decoder decodes messages from Cocaine-runtime
type Decoder interface {
	Decode(interface{}) error
}

// Encoder sends replies to the Cocaine-runtime
type Encoder interface {
	Encode(interface{}) error
}

type message struct {
	Channel int
	Number  int
	Args    []interface{}
}

// Dispatcher handles incoming messages and keeps the state of the channel
type Dispatcher interface {
	Handle(msg *message) (Dispatcher, error)
}

// ConnectionHandler provides method to handle accepted connection for Listener
type ConnectionHandler struct {
	ctx            context.Context
	session        map[int]Dispatcher
	highestChannel int

	newDecoder    decoderInit
	newDispatcher dispatcherInit
}

// NewConnectionHandler creates new ConnectionHandler
func NewConnectionHandler(ctx context.Context) (*ConnectionHandler, error) {
	ctx = withArgsUnpacker(ctx, msgpackArgsDecoder{})
	return newConnectionHandler(ctx, newMsgpackDecoder, newInitialDispatch)
}

func newConnectionHandler(ctx context.Context, newDec decoderInit, newDisp dispatcherInit) (*ConnectionHandler, error) {
	return &ConnectionHandler{
		ctx:            ctx,
		session:        make(map[int]Dispatcher),
		highestChannel: 0,

		newDecoder:    newDec,
		newDispatcher: newDisp,
	}, nil
}

func (h *ConnectionHandler) HandleConn(conn io.ReadWriteCloser) {
	defer conn.Close()

	decoder := h.newDecoder(conn)

	for {
		var msg message

		err := decoder.Decode(&msg)
		if err != nil {
			GetLogger(h.ctx).WithError(err).Errorf("unable to Decode protocol message. Stop handling the connection")
			return
		}

		// NOTE: it can be the bottleneck
		dispatcher, ok := h.session[msg.Channel]
		if !ok {
			if msg.Number < h.highestChannel {
				GetLogger(h.ctx).Errorf("corrupted channel order: %d %d", msg.Number, h.highestChannel)
				continue
			}

			// TODO: refactor
			var dw = newDownstream(newMsgpackEncoder(conn), msg.Channel)
			ctx := withDownstream(h.ctx, dw)
			dispatcher = h.newDispatcher(ctx)
		}

		dispatcher, err = dispatcher.Handle(&msg)
		if err != nil {
			GetLogger(h.ctx).WithError(err).Errorf("Handle returns an error")
			continue
		}
		h.session[msg.Channel] = dispatcher
	}
}

type downstream struct {
	enc     Encoder
	channel int
}

func newDownstream(enc Encoder, channel int) Downstream {
	return &downstream{
		enc:     enc,
		channel: channel,
	}
}

func (d *downstream) Reply(code int, args ...interface{}) error {
	if args == nil {
		args = []interface{}{}
	}
	var msg = message{
		Channel: d.channel,
		Number:  code,
		Args:    args,
	}

	return d.enc.Encode(msg)
}
