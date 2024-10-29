package support

import (
	"fmt"
)

// similar to linkes hash map in Java, a map that preserves insertion order

var checkStatus = false

type Node[K comparable, V any] struct {
	key   K
	value V
	prev  *Node[K, V]
	next  *Node[K, V]
}

type LinkedMap[K comparable, V any] struct {
	first      *Node[K, V]
	last       *Node[K, V]
	collection map[K]*Node[K, V]
}

func NewLinkedMap[K comparable, V any]() *LinkedMap[K, V] {
	res := LinkedMap[K, V]{
		first:      nil,
		last:       nil,
		collection: make(map[K]*Node[K, V]),
	}
	res.check()
	return &res
}

func (m *LinkedMap[K, V]) Len() int {
	return len(m.collection)
}

func (m *LinkedMap[K, V]) Put(key K, value V) {
	defer m.check()
	newNode := &Node[K, V]{
		key:   key,
		value: value,
		prev:  m.last,
		next:  nil,
	}
	if m.first == nil {
		m.first = newNode
		m.last = m.first
		m.collection[key] = m.first
		return
	}
	m.Delete(key)
	m.last.next = newNode
	m.last = newNode
	m.collection[key] = newNode
}

func (m *LinkedMap[K, V]) Delete(key K) bool {
	defer m.check()
	node, ok := m.collection[key]
	if !ok {
		return false
	}
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		m.first = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		m.last = node.prev
	}
	delete(m.collection, key)
	return true
}

func (m *LinkedMap[K, V]) Get(key K) (V, bool) {
	defer m.check()
	v, ok := m.collection[key]
	if !ok {
		return *new(V), false
	}
	return v.value, true
}

func (m *LinkedMap[K, V]) Contains(key K) bool {
	_, ok := m.collection[key]
	return ok
}

type Entry[K comparable, V any] struct {
	Key   K
	Value V
}

func (m *LinkedMap[K, V]) RangeKeys() <-chan K {
	defer m.check()
	res := make(chan K, len(m.collection))
	for node := m.first; node != nil; node = node.next {
		res <- node.key
	}
	close(res)
	return res
}

func (m *LinkedMap[K, V]) RangeValues() <-chan V {
	defer m.check()
	res := make(chan V, len(m.collection))
	for node := m.first; node != nil; node = node.next {
		res <- node.value
	}
	close(res)
	return res
}

func (m *LinkedMap[K, V]) RangeEntries() <-chan Entry[K, V] {
	defer m.check()
	res := make(chan Entry[K, V], len(m.collection))
	for node := m.first; node != nil; node = node.next {
		res <- Entry[K, V]{
			Key:   node.key,
			Value: node.value,
		}
	}
	close(res)
	return res
}

func (m *LinkedMap[K, V]) check() {
	if !checkStatus {
		return
	}
	assert := func(c bool, text string) {
		if !c {
			panic(text)
		}
	}
	if m.first == nil {
		assert(m.last == nil, "Last should be nil")
	}
	if m.first != nil {
		assert(m.last != nil, "Last must not be nil")
	}
	if m.first == nil {
		assert(m.Len() == 0, "Len should be 0")
	}
	if m.first != nil {
		assert(m.Len() > 0, "Len should be > 0")
		count := 1
		for node := m.first; node.next != nil && count < 1000; node = node.next {
			if node.prev != nil {
				assert(node.prev.next == node, "Broken link between nodes")
			}
			count++
		}
		assert(count == m.Len(), fmt.Sprintf("Len expected %d, got %d", count, m.Len()))
	}
}
