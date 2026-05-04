package store

import "context"

// ObjectStorePersistence defines the subset of object store operations required for usage snapshot persistence.
type ObjectStorePersistence interface {
	// PutObject stores data under the given key.
	PutObject(ctx context.Context, key string, data []byte) error
	// ListObjects returns all object keys under the given prefix.
	ListObjects(ctx context.Context, prefix string) ([]string, error)
	// GetObject retrieves data for the given key.
	GetObject(ctx context.Context, key string) ([]byte, error)
	// DeleteObject removes the object at the given key.
	DeleteObject(ctx context.Context, key string) error
}
