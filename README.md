# go-cache

go-cache is an in-memory key:value store/cache similar to memcached that is
suitable for applications running on a single machine. Its major advantage is
that, being essentially a thread-safe `map[string]interface{}` with expiration
times, it doesn't need to serialize or transmit its contents over the network.

Any object can be stored, for a given duration or forever, and the cache can be
safely used by multiple goroutines.

Although go-cache isn't meant to be used as a persistent datastore, the entire
cache can be saved to and loaded from a file (using `c.Items()` to retrieve the
items map to serialize, and `NewFrom()` to create a cache from a deserialized
one) to recover from downtime quickly. (See the docs for `NewFrom()` for caveats.)

基于 go-cache v2.1.0 二次开发，增加以下功能：
cachePro 结构体，新增方法：
改为泛型支持 指定类型存取
并且在删除的时候 可以调用析构函数
### 安装

`go get github.com/jxc1690/goCachePro`

### 原版方法

```go
import (
	"fmt"
	"github.com/jxc1690/goCachePro"
	"time"
)

func main() {
	// Create a cache with a default expiration time of 5 minutes, and which
	// purges expired items every 10 minutes
	c := cache.New(5*time.Minute, 10*time.Minute)

	// Set the value of the key "foo" to "bar", with the default expiration time
	c.Set("foo", "bar", cache.DefaultExpiration)

	// Set the value of the key "baz" to 42, with no expiration time
	// (the item won't be removed until it is re-set, or removed using
	// c.Delete("baz")
	c.Set("baz", 42, cache.NoExpiration)

	// Get the string associated with the key "foo" from the cache
	foo, found := c.Get("foo")
	if found {
		fmt.Println(foo)
	}

	// Since Go is statically typed, and cache values can be anything, type
	// assertion is needed when values are being passed to functions that don't
	// take arbitrary types, (i.e. interface{}). The simplest way to do this for
	// values which will only be used once--e.g. for passing to another
	// function--is:
	foo, found := c.Get("foo")
	if found {
		MyFunction(foo.(string))
	}

	// This gets tedious if the value is used several times in the same function.
	// You might do either of the following instead:
	if x, found := c.Get("foo"); found {
		foo := x.(string)
		// ...
	}
	// or
	var foo string
	if x, found := c.Get("foo"); found {
		foo = x.(string)
	}
	// ...
	// foo can then be passed around freely as a string

	// Want performance? Store pointers!
	c.Set("foo", &MyStruct, cache.DefaultExpiration)
	if x, found := c.Get("foo"); found {
		foo := x.(*MyStruct)
			// ...
	}
}
```

### 使用改进后的泛型方法

```go
import (
    "fmt"
    "github.com/jxc1690/goCachePro"
    "time"
)

func main() {
    // Create a cache with a default expiration time of 5 minutes, and which
    // purges expired items every 10 minutes
    c := cachePro.NewPro[string](5*time.Minute, 10*time.Minute, nil)

    // Set the value of the key "foo" to "bar", with the default expiration time
    c.Set("foo", "bar", cachePro.DefaultExpiration)

    // Get the string associated with the key "foo" from the cache
    foo, found := c.Get("foo")
    if found {
        fmt.Println(foo)
    }

    // Example with destructor function
    destructor := func(value string) {
        fmt.Println("Destructing value:", value)
    }
    cWithDestructor := cachePro.NewPro[string](5*time.Minute, 10*time.Minute, destructor)
    cWithDestructor.Set("key1", "value1", cachePro.DefaultExpiration)
    cWithDestructor.Delete("key1") // This will call the destructor function
}
```

### Reference

`godoc` or [http://godoc.org/github.com/patrickmn/go-cache](http://godoc.org/github.com/patrickmn/go-cache)
