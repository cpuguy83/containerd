// +build !windows

package contentserver

import (
	"context"
	"io"
	"os"

	"github.com/containerd/fifo"
)

func openPipeReader(ctx context.Context, p string) (io.ReadCloser, error) {
	return fifo.OpenFifo(ctx, p, os.O_RDONLY, 0)
}
