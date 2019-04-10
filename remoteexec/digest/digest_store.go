/* Copyright 2018 Google Inc. All Rights Reserved. */

package digest

import (
	rpb "go.chromium.org/goma/server/proto/remote-apis/build/bazel/remote/execution/v2"
)

type digestKey struct {
	hash      string
	sizeBytes int64
}

func asKey(d *rpb.Digest) digestKey {
	return digestKey{
		hash:      d.Hash,
		sizeBytes: d.SizeBytes,
	}
}

// Store works as local content addressable storage.
type Store struct {
	m map[digestKey]Data
}

// NewStore creates new local content addressable storage.
func NewStore() *Store {
	return &Store{
		m: make(map[digestKey]Data),
	}
}

// Set sets data in store.
func (s *Store) Set(data Data) {
	s.m[asKey(data.Digest())] = data
}

// Get gets data from store.
func (s *Store) Get(digest *rpb.Digest) (Data, bool) {
	v, ok := s.m[asKey(digest)]
	return v, ok
}

// GetSource gets source from store.
func (s *Store) GetSource(digest *rpb.Digest) (Source, bool) {
	v, ok := s.Get(digest)
	if !ok {
		return nil, false
	}
	switch v := v.(type) {
	case data:
		return v.source, true
	}
	return v.(Source), true
}

// List lists known digests in cas.
func (s *Store) List() []*rpb.Digest {
	var digests []*rpb.Digest
	for k := range s.m {
		digests = append(digests, &rpb.Digest{
			Hash:      k.hash,
			SizeBytes: k.sizeBytes,
		})
	}
	// sort?
	return digests
}

// TODO: data for goma FileBlob.
