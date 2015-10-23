package p9pnew

import (
	"bufio"
	"fmt"
	"log"
	"time"

	"golang.org/x/net/context"

	"net"
)

type client struct {
	conn     net.Conn
	tags     *tagPool
	requests chan *fcallRequest
}

// NewSession returns a session using the connection.
func NewSession(conn net.Conn) (Session, error) {
	return &client{
		conn: conn,
	}
}

var _ Session = &client{}

func (c *client) Auth(ctx context.Context, afid Fid, uname, aname string) (Qid, error) {
	panic("not implemented")
}

func (c *client) Attach(ctx context.Context, fid, afid Fid, uname, aname string) (Qid, error) {
	panic("not implemented")
}

func (c *client) Clunk(ctx context.Context, fid Fid) error {
	panic("not implemented")
}

func (c *client) Remove(ctx context.Context, fid Fid) error {
	panic("not implemented")
}

func (c *client) Walk(ctx context.Context, fid Fid, newfid Fid, names ...string) ([]Qid, error) {
	panic("not implemented")
}

func (c *client) Read(ctx context.Context, fid Fid, p []byte, offset int64) (n int, err error) {
	panic("not implemented")
}

func (c *client) Write(ctx context.Context, fid Fid, p []byte, offset int64) (n int, err error) {
	panic("not implemented")
}

func (c *client) Open(ctx context.Context, fid Fid, mode int32) (Qid, error) {
	panic("not implemented")
}

func (c *client) Create(ctx context.Context, parent Fid, name string, perm uint32, mode uint32) (Qid, error) {
	panic("not implemented")
}

func (c *client) Stat(context.Context, Fid) (Dir, error) {
	panic("not implemented")
}

func (c *client) WStat(context.Context, Fid, Dir) error {
	panic("not implemented")
}

func (c *client) Version(ctx context.Context, msize int32, version string) (int32, string, error) {
	fcall := &Fcall{
		Type: TVersion,
		Tag:  tag,
		Message: MessageVersion{
			MSize:   msize,
			Version: Version,
		},
	}

	resp, err := c.send(ctx, fcall)
	if err != nil {
		return 0, "", err
	}

	mv, ok := resp.Message.(*MessageVersion)
	if !ok {
		return fmt.Errorf("invalid rpc response for version message: %v", resp)
	}

	return mv.MSize, mv.Version, nil
}

// send dispatches the fcall.
func (c *client) send(ctx context.Context, fc *Fcall) (*Fcall, error) {
	fc.Tag = c.tags.Get()
	defer c.tags.Put(fc.Tag)

	fcreq := newFcallRequest(ctx, fc)

	// dispatch the request.
	select {
	case <-c.closed:
		return nil, ErrClosed
	case c.requests <- fcreq:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// wait for the response.
	select {
	case <-closed:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-fcreq.response:
		return resp, nil
	}
}

type fcallRequest struct {
	ctx      context.Context
	fcall    *Fcall
	response chan *Fcall
	err      chan error
}

func newFcallRequest(ctx context.Context, fc *Fcall) fcallRequest {
	return fcallRequest{
		ctx:      ctx,
		fcall:    fc,
		response: make(chan *Fcall, 1),
		err:      make(chan err, 1),
	}
}

// handle takes messages off the wire and wakes up the waiting tag call.
func (c *client) handle() {

	var (
		responses = make(chan *Fcall)
		// outstanding provides a map of tags to outstanding requests.
		outstanding = map[Tag]*fcallRequest{}
	)

	// loop to read messages off of the connection
	go func() {
		r := bufio.NewReader(c.conn)

	loop:
		for {
			// Continuously set the read dead line pump the loop below. We can
			// probably set a connection dead threshold that can count these.
			// Usually, this would only matter when there are actually
			// outstanding requests.
			if err := c.conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				panic(fmt.Sprintf("error setting read deadline: %v", err))
			}

			fc := new(Fcall)
			if err := read9p(r, fc); err != nil {
				switch err := err.(type) {
				case net.Error:
					if err.Timeout() || err.Temporary() {
						break loop
					}
				}

				panic(fmt.Sprintf("connection read error: %v", err))
			}

			select {
			case <-closed:
				return
			case responses <- fc:
			}
		}

	}()

	w := bufio.NewWriter(c.conn)

	for {
		select {
		case <-c.closed:
			return
		case req := <-c.requests:
			outstanding[req.fcall.Tag] = req

			// use deadline to set write deadline for this request.
			deadline, ok := req.ctx.Deadline()
			if !ok {
				deadline = time.Now().Add(time.Second)
			}

			if err := c.conn.SetWriteDeadline(deadline); err != nil {
				log.Println("error setting write deadline: %v", err)
			}

			if err := write9p(w, req.fcall); err != nil {
				delete(outstanding, req.fcall.Tag)
				req.err <- err
			}
		case b := <-responses:
			req, ok := outstanding[b.Tag]
			if !ok {
				panic("unknown tag received")
			}
			delete(outstanding, req.fcall.Tag)

			req.response <- b
		}
	}
}
