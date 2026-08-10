// Package pkga implements a small LRU cache, used by the realistic
// benchmark scenario to give the compiler non-trivial (but not extreme)
// work to do on every rebuild.
package pkga

import "container/list"

type entry struct {
	key   string
	value int
}

// LRUCache is a fixed-capacity least-recently-used cache.
type LRUCache struct {
	capacity int
	items    map[string]*list.Element
	order    *list.List
}

// New creates an LRUCache with the given capacity.
func New(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Get returns the value for key and marks it most-recently-used.
func (c *LRUCache) Get(key string) (int, bool) {
	el, ok := c.items[key]
	if !ok {
		return 0, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*entry).value, true
}

// Put inserts or updates key, evicting the least-recently-used entry if the
// cache is at capacity.
func (c *LRUCache) Put(key string, value int) {
	if el, ok := c.items[key]; ok {
		el.Value.(*entry).value = value
		c.order.MoveToFront(el)
		return
	}
	if c.order.Len() >= c.capacity {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*entry).key)
		}
	}
	el := c.order.PushFront(&entry{key: key, value: value})
	c.items[key] = el
}

// Len returns the current number of entries.
func (c *LRUCache) Len() int {
	return c.order.Len()
}

// Keys returns keys from most- to least-recently-used.
func (c *LRUCache) Keys() []string {
	keys := make([]string, 0, c.order.Len())
	for el := c.order.Front(); el != nil; el = el.Next() {
		keys = append(keys, el.Value.(*entry).key)
	}
	return keys
}
