package proxy

import (
	"errors"
	"io"
)

func makePipe(ctx context.Contxt, ref string) (io.ReadWriteCloser, error) {
	return nil, errors.New("not implemented")
}
