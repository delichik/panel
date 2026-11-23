package cache

import (
	"context"
	"testing"
	"time"
)

func BenchmarkGet(b *testing.B) {
	c, err := New(Config{DefaultTTL: 30 * time.Second, Shards: 1})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	if err := c.Set("key", []byte("value"), 0); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := c.Get("key"); !ok {
			b.Fatal("miss")
		}
	}
}

func BenchmarkSet(b *testing.B) {
	c, err := New(Config{DefaultTTL: 30 * time.Second, Shards: 1})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	value := []byte("value")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Set("key", value, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetOrLoadHit(b *testing.B) {
	c, err := New(Config{DefaultTTL: 30 * time.Second, Shards: 1})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	if err := c.Set("key", []byte("value"), 0); err != nil {
		b.Fatal(err)
	}
	load := func(context.Context) ([]byte, error) {
		b.Fatal("cache hit must not load")
		return nil, nil
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.GetOrLoad(context.Background(), "key", 0, load); err != nil {
			b.Fatal(err)
		}
	}
}
