package orderedmap

import pb33f "github.com/pb33f/ordered-map/v2"

type Pair[K comparable, V any] = pb33f.Pair[K, V]
type OrderedMap[K comparable, V any] = pb33f.OrderedMap[K, V]
type InitOption[K comparable, V any] = pb33f.InitOption[K, V]

func WithCapacity[K comparable, V any](capacity int) InitOption[K, V] {
	return pb33f.WithCapacity[K, V](capacity)
}

func WithInitialData[K comparable, V any](initialData ...Pair[K, V]) InitOption[K, V] {
	return pb33f.WithInitialData[K, V](initialData...)
}

func WithDisableHTMLEscape[K comparable, V any]() InitOption[K, V] {
	return pb33f.WithDisableHTMLEscape[K, V]()
}

func New[K comparable, V any](options ...any) *OrderedMap[K, V] {
	return pb33f.New[K, V](options...)
}
