package tgstore

import (
	"context"
	"crypto/cipher"
	"errors"
	"io"
	"os"
	"sync"
)

// Object is the unit of the `TGStore`.
type Object struct {
	ID       string
	MIMEType string
	Size     int64
	Checksum []byte

	aead         cipher.AEAD
	chunks       []*objectChunk
	chunkSets    []*objectChunkSet
	hashMidstate []byte
}

// objectMetadata is the metadata of the `Object`.
type objectMetadata struct {
	Chunks       []*objectChunk    `json:"chunks"`
	ChunkSets    []*objectChunkSet `json:"chunk_sets"`
	MIMEType     string            `json:"mime_type"`
	Size         int64             `json:"size"`
	HashMidstate string            `json:"hash_midstate"`
}

// objectChunk is the unit of the `Object`.
type objectChunk struct {
	ID   string `json:"id"`
	Size int64  `json:"size"`
}

// newReader returns a new instance of the `io.ReadCloser`.
func (oc *objectChunk) newReader(
	ctx context.Context,
	aead cipher.AEAD,
	offset int64,
) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

// objectChunkSet is the set of the `objectChunk`.
type objectChunkSet struct {
	ID    string `json:"id"`
	Size  int64  `json:"size"`
	Count int    `json:"count"`
}

// chunks returns the list of the `objectChunk` from the ocs.
func (ocs *objectChunkSet) chunks(
	ctx context.Context,
	aead cipher.AEAD,
) ([]*objectChunk, error) {
	return nil, errors.New("not implemented")
}

// NewReader returns a new instance of the `ObjectReader`.
func (o *Object) NewReader(ctx context.Context) (*ObjectReader, error) {
	return &ObjectReader{
		ctx:       ctx,
		aead:      o.aead,
		chunks:    o.chunks,
		chunkSets: o.chunkSets,
		size:      o.Size,
	}, nil
}

// ObjectReader is the reader of the `Object`.
type ObjectReader struct {
	ctx        context.Context
	mutex      sync.Mutex
	aead       cipher.AEAD
	chunks     []*objectChunk
	chunkSets  []*objectChunkSet
	size       int64
	offset     int64
	closed     bool
	readCloser io.ReadCloser
}

// Read implements the `io.Reader`.
func (or *ObjectReader) Read(b []byte) (int, error) {
	or.mutex.Lock()
	defer or.mutex.Unlock()

	if or.closed {
		return 0, os.ErrClosed
	} else if or.offset >= or.size {
		return 0, io.EOF
	}

	if or.readCloser == nil {
		pipeReader, pipeWriter := io.Pipe()
		go func() (err error) {
			defer func() {
				pipeWriter.CloseWithError(err)
			}()

			offset := or.offset
			readChunks := func(chunks []*objectChunk) error {
				for _, chunk := range chunks {
					if chunk.Size > offset {
						cr, err := chunk.newReader(
							or.ctx,
							or.aead,
							offset,
						)
						if err != nil {
							return err
						}

						_, err = io.Copy(pipeWriter, cr)
						cr.Close()
						if err != nil {
							return err
						}

						offset = 0
					} else {
						offset -= chunk.Size
					}
				}

				return nil
			}

			if err := readChunks(or.chunks); err != nil {
				return err
			}

			for _, chunkSet := range or.chunkSets {
				if chunkSet.Size > offset {
					chunks, err := chunkSet.chunks(
						or.ctx,
						or.aead,
					)
					if err != nil {
						return err
					}

					if err := readChunks(
						chunks,
					); err != nil {
						return err
					}
				} else {
					offset -= chunkSet.Size
				}
			}

			return nil
		}()

		or.readCloser = pipeReader
	}

	n, err := or.readCloser.Read(b)
	or.offset += int64(n)

	return n, err
}

// Seek implements the `io.Seeker`.
func (or *ObjectReader) Seek(offset int64, whence int) (int64, error) {
	or.mutex.Lock()
	defer or.mutex.Unlock()

	if or.closed {
		return 0, os.ErrClosed
	}

	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += or.offset
	case io.SeekEnd:
		offset += or.size
	default:
		return 0, errors.New("invalid whence")
	}

	if offset < 0 {
		return 0, errors.New("negative position")
	}

	if offset != or.offset {
		or.offset = offset
		if or.readCloser != nil {
			if err := or.readCloser.Close(); err != nil {
				return 0, err
			}

			or.readCloser = nil
		}
	}

	return or.offset, nil
}

// Close implements the `io.Closer`.
func (or *ObjectReader) Close() error {
	or.mutex.Lock()
	defer or.mutex.Unlock()

	if or.closed {
		return os.ErrClosed
	}

	if or.readCloser != nil {
		if err := or.readCloser.Close(); err != nil {
			return err
		}

		or.readCloser = nil
	}

	or.closed = true

	return nil
}
