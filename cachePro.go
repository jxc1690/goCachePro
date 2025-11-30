package cache

import (
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"
)

type CachePro[T any] struct {
	*cachePro[T]
	// If this is confusing, see the comment at the bottom of New()
}
type ItemPro[T any] struct {
	Object     T
	Expiration int64
}
type cachePro[T any] struct {
	defaultExpiration time.Duration
	items             map[string]ItemPro[T]
	mu                sync.RWMutex
	onEvicted         func(string, interface{})
	janitor           *janitorPro[T]
	delFunc           func(T)
}

// 向CachePro添加一个项目，替换任何现有项目。如果持续时间为0
// (DefaultExpiration)，则使用CachePro的默认过期时间。如果为-1
// (NoExpiration)，则项目永不过期。
func (c *CachePro[T]) Set(k string, x T, d time.Duration) {
	// "Inlining" of set
	var e int64
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}
	if d > 0 {
		e = time.Now().Add(d).UnixNano()
	}
	c.mu.Lock()
	c.items[k] = Item{
		Object:     x,
		Expiration: e,
	}
	// TODO: Calls to mu.Unlock are currently not deferred because defer
	// adds ~200 ns (as of go1.)
	c.mu.Unlock()
}

func (c *cachePro[T]) set(k string, x T, d time.Duration) {
	var e int64
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}
	if d > 0 {
		e = time.Now().Add(d).UnixNano()
	}
	c.items[k] = Item{
		Object:     x,
		Expiration: e,
	}
}

// 向CachePro添加一个项目，替换任何现有项目，使用默认过期时间
func (c *CachePro[T]) SetDefault(k string, x T) {
	c.Set(k, x, DefaultExpiration)
}

// 仅当给定键不存在项目或现有项目已过期时，向CachePro添加项目
// 否则返回错误
func (c *CachePro[T]) Add(k string, x T, d time.Duration) error {
	c.mu.Lock()
	_, found := c.get(k)
	if found {
		c.mu.Unlock()
		return fmt.Errorf("Item %s already exists", k)
	}
	c.set(k, x, d)
	c.mu.Unlock()
	return nil
}

// 仅当CachePro键已存在且现有项目未过期时，设置新值
// 否则返回错误
func (c *CachePro[T]) Replace(k string, x T, d time.Duration) error {
	c.mu.Lock()
	_, found := c.get(k)
	if !found {
		c.mu.Unlock()
		return fmt.Errorf("Item %s doesn't exist", k)
	}
	c.set(k, x, d)
	c.mu.Unlock()
	return nil
}

// 从CachePro获取项目。返回项目或零值，以及一个布尔值指示是否找到键
func (c *CachePro[T]) Get(k string) (T, bool) {
	c.mu.RLock()
	// "Inlining" of get and Expired
	item, found := c.items[k]
	if !found {
		c.mu.RUnlock()
		var zero T
		return zero, false
	}
	if item.Expiration > 0 {
		if time.Now().UnixNano() > item.Expiration {
			c.mu.RUnlock()
			var zero T
			return zero, false
		}
	}
	c.mu.RUnlock()
	return item.Object.(T), true
}

// GetWithExpiration 从CachePro返回项目及其过期时间
// 返回项目或零值，如果设置了过期时间则返回过期时间（如果项目永不过期则返回time.Time的零值），
// 以及一个布尔值指示是否找到键
func (c *CachePro[T]) GetWithExpiration(k string) (T, time.Time, bool) {
	c.mu.RLock()
	// "Inlining" of get and Expired
	item, found := c.items[k]
	if !found {
		c.mu.RUnlock()
		var zero T
		return zero, time.Time{}, false
	}

	if item.Expiration > 0 {
		if time.Now().UnixNano() > item.Expiration {
			c.mu.RUnlock()
			var zero T
			return zero, time.Time{}, false
		}

		// Return the item and the expiration time
		c.mu.RUnlock()
		return item.Object.(T), time.Unix(0, item.Expiration), true
	}

	// If expiration <= 0 (i.e. no expiration time set) then return the item
	// and a zeroed time.Time
	c.mu.RUnlock()
	return item.Object.(T), time.Time{}, true
}

func (c *cachePro[T]) get(k string) (T, bool) {
	item, found := c.items[k]
	if !found {
		var zero T
		return zero, false
	}
	// "Inlining" of Expired
	if item.Expiration > 0 {
		if time.Now().UnixNano() > item.Expiration {
			var zero T
			return zero, false
		}
	}
	return item.Object.(T), true
}

// 从CachePro删除项目。如果键不在CachePro中则不执行任何操作
func (c *CachePro[T]) Delete(k string) {
	c.mu.Lock()
	v, evicted := c.delete(k)
	c.mu.Unlock()
	if evicted {
		c.onEvicted(k, v)
	}
}

func (c *cachePro[T]) delete(k string) (interface{}, bool) {
	if c.onEvicted != nil {
		if v, found := c.items[k]; found {
			if c.delFunc != nil {
				c.delFunc(v.Object.(T))
			}
			delete(c.items, k)
			return v.Object, true
		}
	}
	if v, ok := c.items[k]; ok {
		if c.delFunc != nil {
			c.delFunc(v.Object.(T))
		}
	}
	delete(c.items, k)
	return nil, false
}

type keyAndValuePro struct {
	key   string
	value interface{}
}

// 从CachePro删除所有已过期的项目
func (c *CachePro[T]) DeleteExpired() {
	var evictedItems []keyAndValuePro
	now := time.Now().UnixNano()
	c.mu.Lock()
	for k, v := range c.items {
		// "Inlining" of expired
		if v.Expiration > 0 && now > v.Expiration {
			ov, evicted := c.delete(k)
			if evicted {
				evictedItems = append(evictedItems, keyAndValuePro{k, ov})
			}
		}
	}
	c.mu.Unlock()
	for _, v := range evictedItems {
		c.onEvicted(v.key, v.value)
	}
}

// 设置一个（可选的）函数，当项目从CachePro中驱逐时调用该函数（包括手动删除时，但不包括覆盖时）
// 设置为nil以禁用
func (c *CachePro[T]) OnEvicted(f func(string, interface{})) {
	c.mu.Lock()
	c.onEvicted = f
	c.mu.Unlock()
}

// 将CachePro的项写入io.Writer（使用Gob编码）
//
// 注意：此方法已弃用，推荐使用c.Items()和NewFrom()（参见NewFrom()的文档）
func (c *CachePro[T]) Save(w io.Writer) (err error) {
	enc := gob.NewEncoder(w)
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("Error registering item types with Gob library")
		}
	}()
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, v := range c.items {
		gob.Register(v.Object)
	}
	err = enc.Encode(&c.items)
	return
}

// 将CachePro的项保存到给定文件名，如果文件不存在则创建，如果存在则覆盖
//
// 注意：此方法已弃用，推荐使用c.Items()和NewFrom()（参见NewFrom()的文档）
func (c *CachePro[T]) SaveFile(fname string) error {
	fp, err := os.Create(fname)
	if err != nil {
		return err
	}
	err = c.Save(fp)
	if err != nil {
		fp.Close()
		return err
	}
	return fp.Close()
}

// 从io.Reader添加（Gob序列化的）CachePro项，排除当前CachePro中已存在（且未过期）的键
//
// 注意：此方法已弃用，推荐使用c.Items()和NewFrom()（参见NewFrom()的文档）
func (c *CachePro[T]) Load(r io.Reader) error {
	dec := gob.NewDecoder(r)
	items := map[string]Item{}
	err := dec.Decode(&items)
	if err == nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		for k, v := range items {
			ov, found := c.items[k]
			if !found || ov.Expired() {
				c.items[k] = v
			}
		}
	}
	return err
}

// 从给定文件名加载并添加CachePro项，排除当前CachePro中已存在的键
//
// 注意：此方法已弃用，推荐使用c.Items()和NewFrom()（参见NewFrom()的文档）
func (c *CachePro[T]) LoadFile(fname string) error {
	fp, err := os.Open(fname)
	if err != nil {
		return err
	}
	err = c.Load(fp)
	if err != nil {
		fp.Close()
		return err
	}
	return fp.Close()
}

// 将所有未过期的CachePro项复制到新映射中并返回
func (c *CachePro[T]) Items() map[string]Item {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := make(map[string]Item, len(c.items))
	now := time.Now().UnixNano()
	for k, v := range c.items {
		// "Inlining" of Expired
		if v.Expiration > 0 {
			if now > v.Expiration {
				continue
			}
		}
		m[k] = v
	}
	return m
}

// 返回CachePro中的项目数。这可能包括已过期但尚未清理的项目
func (c *CachePro[T]) ItemCount() int {
	c.mu.RLock()
	n := len(c.items)
	c.mu.RUnlock()
	return n
}

// 从CachePro中删除所有项目
func (c *CachePro[T]) Flush() {
	c.mu.Lock()
	c.items = map[string]Item{}
	c.mu.Unlock()
}

type janitorPro[T any] struct {
	Interval time.Duration
	stop     chan bool
}

func (j *janitorPro[T]) Run(c *CachePro[T]) {
	ticker := time.NewTicker(j.Interval)
	for {
		select {
		case <-ticker.C:
			c.DeleteExpired()
		case <-j.stop:
			ticker.Stop()
			return
		}
	}
}

func stopJanitorPro[T any](c *CachePro[T]) {
	c.janitor.stop <- true
}

func runJanitorPro[T any](c *cachePro[T], ci time.Duration) {
	j := &janitorPro[T]{
		Interval: ci,
		stop:     make(chan bool),
	}
	c.janitor = j
	go j.Run(&CachePro[T]{c})
}

func newCachePro[T any](de time.Duration, m map[string]Item) *cachePro[T] {
	if de == 0 {
		de = -1
	}
	c := &cachePro[T]{
		defaultExpiration: de,
		items:             m,
	}
	return c
}

func newCacheProWithJanitor[T any](de time.Duration, ci time.Duration, m map[string]Item, DelFunc func(T)) *CachePro[T] {
	c := newCachePro[T](de, m)
	c.delFunc = DelFunc
	// This trick ensures that the janitor goroutine (which--granted it
	// was enabled--is running DeleteExpired on c forever) does not keep
	// the returned C object from being garbage collected. When it is
	// garbage collected, the finalizer stops the janitor goroutine, after
	// which c can be collected.
	C := &CachePro[T]{c}
	if ci > 0 {
		runJanitorPro[T](c, ci)
		runtime.SetFinalizer(C, stopJanitorPro[T])
	}
	return C
}

// 返回具有给定默认过期时间和清理间隔的新CachePro
// 如果过期时间小于1（或NoExpiration），则CachePro中的项目永不过期（默认情况下），必须手动删除
// 如果清理间隔小于1，则在调用c.DeleteExpired()之前不会从CachePro中删除过期项目
func NewPro[T any](defaultExpiration, cleanupInterval time.Duration, DelFunc func(T)) *CachePro[T] {
	items := make(map[string]Item)
	return newCacheProWithJanitor[T](defaultExpiration, cleanupInterval, items, DelFunc)
}

// 返回具有给定默认过期时间和清理间隔的新CachePro
// 如果过期时间小于1（或NoExpiration），则CachePro中的项目永不过期（默认情况下），必须手动删除
// 如果清理间隔小于1，则在调用c.DeleteExpired()之前不会从CachePro中删除过期项目
//
// NewFrom()还接受一个items映射，它将作为CachePro的基础映射
// 这对于从反序列化的CachePro开始（使用例如gob.Encode()在c.Items()上序列化），
// 或者传入例如make(map[string]Item, 500)以提高CachePro预期达到某个最小大小时的启动性能很有用
//
// 只有CachePro的方法同步访问此映射，因此不建议在创建CachePro后保留对映射的任何引用
// 如果需要，可以在以后使用c.Items()访问映射（受相同注意事项的限制）
//
// 关于序列化的注意事项：使用例如gob时，请确保在编码使用c.Items()检索的映射之前
// 注册存储在CachePro中的各个类型，并在解码包含items映射的blob之前注册相同的类型
func NewFromPro[T any](defaultExpiration, cleanupInterval time.Duration, items map[string]Item) *CachePro[T] {
	return newCacheProWithJanitor[T](defaultExpiration, cleanupInterval, items, nil)
}
