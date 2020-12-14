package local

import (
	"context"
	"io"
	"sync/atomic"
	"syscall"

	"github.com/containerd/containerd/log"
	"github.com/opencontainers/go-digest"
	"golang.org/x/sys/unix"
)

var (
	spliceSupported int32 = 1
)

func (w *writer) ReadFrom(r io.Reader) (int64, error) {
	if atomic.LoadInt32(&spliceSupported) != 1 {
		return copyNoReaderFrom(w, r)
	}

	remain := int64(1 << 62)
	lr, ok := r.(*io.LimitedReader)
	if ok {
		remain, r = lr.N, lr.R
		if remain <= 0 {
			return 0, nil
		}
	}

	if w.total > 0 {
		remain = w.total - w.offset
	}
	if remain == 0 {
		log.G(context.TODO()).WithField("totoal", w.total).WithField("offset", w.offset).Info("Nothing to copy")
		return 0, nil
	}

	rscI, ok := r.(syscall.Conn)
	if !ok {
		return copyNoReaderFrom(w, r)
	}

	rsc, err := rscI.SyscallConn()
	if err != nil {
		if lr != nil {
			r = lr
		}
		return copyNoReaderFrom(w, r)
	}

	wsc, err := w.fp.SyscallConn()
	if err != nil {
		if lr != nil {
			r = lr
		}
		return copyNoReaderFrom(w, r)
	}

	handled, written, err := w.doRawCopy(rsc, wsc, remain)
	if err != nil {
		return written, err
	}
	if !handled {
		if lr != nil {
			r = lr
		}
		written, err = copyNoReaderFrom(w, r)
	}

	return written, err
}

func (w *writer) doRawCopy(rsc, wsc syscall.RawConn, remain int64) (handled bool, written int64, retErr error) {
	var (
		spliceErr error
		writeErr  error
	)

	log.G(context.TODO()).WithField("expected", remain).Info("Doing raw copy")
	defer func() {
		if written > 0 {
			w.offset += written
		}
		log.G(context.TODO()).WithField("w", written).WithField("expected", remain).WithError(retErr).Info("Finished raw copy")
	}()

	sockFd, err := unix.Socket(unix.AF_ALG, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		log.G(context.TODO()).WithError(err).Info("SOCKET")
		return false, 0, nil
	}
	defer unix.Close(sockFd)

	if err := unix.Bind(sockFd, &unix.SockaddrALG{
		Type: "hash",
		Name: "sha256",
	}); err != nil {
		log.G(context.TODO()).WithError(err).Info("BIND")
		return false, 0, nil
	}

	alg, _, _ := unix.Syscall(unix.SYS_ACCEPT, uintptr(sockFd), 0, 0)
	if err != nil {
		log.G(context.TODO()).WithError(err).Info("ACCEPT")
		return false, 0, nil
	}
	defer unix.Close(int(alg))

	var hashPipe [2]int
	if err := unix.Pipe2(hashPipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		log.G(context.TODO()).WithError(err).Info("PIPE2")
		return false, 0, nil
	}

	defer func() {
		unix.Close(hashPipe[0])
		unix.Close(hashPipe[1])

		if written > 0 {
			hash := make([]byte, 32)
			n, err := unix.Read(int(alg), hash)
			if err != nil {
				if retErr != nil {
					retErr = err
				}
			}

			w.digest = digest.NewDigestFromBytes(digest.SHA256, hash[:n])
			w.digester = &rawDigester{w.digester, w.digest}
			log.G(context.TODO()).WithField("read", n).WithField("digest", w.digest).WithError(err).Error("HASH READ")

		}
	}()

	// Hear the RawConn Read/Write methods allow us to utilize the go runtime
	// poller to wait for the file descriptors to be ready for reads or writes.
	//
	// Read/Write will sleep the goroutine until the file descriptor is ready.
	// Once we are inside the function we've passed in, we know the FD is ready.
	//
	// Read/Write both run the function they are passed repeatedly until the
	// function returns true.
	//
	// We always use NONBLOCK here. If the file descriptor(s) is not
	// opened with O_NONBLOCK then splice just blocks like normal.
	// If they opened with O_NONBLOCK, then `unix.Splice` returns
	// with EAGAIN when either the read or write would block.

	tee := func(rfd, wfd int, total int64) (teed int64, err error) {
		n, err := unix.Tee(int(rfd), int(wfd), int(total), unix.SPLICE_F_NONBLOCK)
		log.G(context.TODO()).WithField("teed", n).WithError(err).Error("TEE REAL")

		switch {
		case n == 0 && err == nil:
			err = io.EOF
		}

		if err == unix.EINTR {
			err = nil
		}
		if err == unix.EAGAIN {
			if n > 0 {
				err = nil
			}
		}
		return n, err
	}

	splice := func(rfd, wfd int, total int64) (written int64, err error) {
		var n int64
		for total > 0 {
			n, err = unix.Splice(rfd, nil, wfd, nil, int(total), unix.SPLICE_F_MOVE|unix.SPLICE_F_NONBLOCK)
			if n > 0 {
				written += n
				total -= n
			}

			switch err {
			case unix.ENOSYS:
				// splice not supported on kernel
				atomic.StoreInt32(&spliceSupported, 0)
				return
			case nil:
				if n == 0 {
					err = io.EOF
					return
				}
			case unix.EINTR:
				continue
			default:
				break
			}
		}
		return written, err
	}

	err = rsc.Read(func(rfd uintptr) bool {
		teed, err := tee(int(rfd), hashPipe[1], remain)
		if err != nil {
			if err != unix.EAGAIN {
				spliceErr = err
				return true
			}
			return false
		}

		// copy to underlying writer
		spliceRemain := teed
		err = wsc.Write(func(wfd uintptr) bool {
			n, err := splice(int(rfd), int(wfd), spliceRemain)
			if n > 0 {
				handled = true
				spliceRemain -= n
				written += n
			}
			if err != nil {
				if err == unix.EAGAIN && spliceRemain > 0 {
					return false
				}
				spliceErr = err
				return true
			}
			return spliceRemain == 0
		})

		// copy to hash socket
		spliceRemain = teed
		_, err = splice(hashPipe[0], int(alg), spliceRemain)
		if err != nil {
			spliceErr = err
			return true
		}
		if err != nil {
			writeErr = err
			return true
		}
		if spliceErr != nil {
			// I don't like this but it made the linter happy. All hail the mighty linter.
			// If we splice returned AGAIN, we should return false so we can
			// wait for the FD to be ready again. Otherwise we just want to exit
			// early.
			return spliceErr != unix.EAGAIN
		}

		return true
	})

	if spliceErr != nil && spliceErr != io.EOF {
		return handled, written, spliceErr
	}
	if writeErr != nil {
		return handled, written, writeErr
	}
	return handled, written, err
}

func copyNoReaderFrom(dst io.Writer, src io.Reader) (int64, error) {
	panic("Fallback")
	// If the reader has a WriteTo method, use it to do the copy.
	// Avoids an allocation and a copy.
	if wt, ok := src.(io.WriterTo); ok {
		return wt.WriteTo(dst)
	}
	return copyWithBuffer(dst, src)
}

func copyWithBuffer(dst io.Writer, src io.Reader) (written int64, err error) {
	bufRef := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufRef)
	buf := *bufRef
	for {
		nr, er := io.ReadAtLeast(src, buf, len(buf))
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			// If an EOF happens after reading fewer than the requested bytes,
			// ReadAtLeast returns ErrUnexpectedEOF.
			if er != io.EOF && er != io.ErrUnexpectedEOF {
				err = er
			}
			break
		}
	}
	return
}

type rawDigester struct {
	digest.Digester
	dgst digest.Digest
}

func (d *rawDigester) Digest() digest.Digest {
	return d.dgst
}
