// +build !windows

package proxy

import (
	"context"
	"io"
	"os"

	"github.com/containerd/fifo"
	"golang.org/x/sys/unix"
)

func makePipe(ctx context.Context, p string) (io.ReadWriteCloser, error) {
	f, err := fifo.OpenFifo(ctx, p, os.O_WRONLY|unix.O_CREAT|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return &wrapPipe{f, p}, nil
}

type wrapPipe struct {
	io.ReadWriteCloser
	p string
}

func (w *wrapPipe) ReadFrom(r io.Reader) (int64, error) {
	return w.ReadWriteCloser.(io.ReaderFrom).ReadFrom(r)
}

func (w *wrapPipe) Close() error {
	err := w.ReadWriteCloser.Close()
	if err2 := os.Remove(w.p); err2 != nil && !os.IsNotExist(err2) && err == nil {
		err = err2
	}
	return err
}
