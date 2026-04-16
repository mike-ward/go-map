package tile

import "testing"

func TestCachePutGet(t *testing.T) {
	c := NewCache(4)
	k := Coord{Z: 1, X: 0, Y: 0}
	c.Put(k, []byte("abc"))
	if got, ok := c.Get(k); !ok || string(got) != "abc" {
		t.Fatalf("want abc, got ok=%v data=%q", ok, got)
	}
}

func TestCacheEvictsLRU(t *testing.T) {
	c := NewCache(2)
	k1 := Coord{Z: 1, X: 0, Y: 0}
	k2 := Coord{Z: 1, X: 1, Y: 0}
	k3 := Coord{Z: 1, X: 0, Y: 1}

	c.Put(k1, []byte("1"))
	c.Put(k2, []byte("2"))
	// Touch k1 to make k2 the LRU.
	if _, ok := c.Get(k1); !ok {
		t.Fatal("k1 missing")
	}
	c.Put(k3, []byte("3"))

	if _, ok := c.Get(k2); ok {
		t.Error("k2 should have been evicted")
	}
	if _, ok := c.Get(k1); !ok {
		t.Error("k1 should survive")
	}
	if _, ok := c.Get(k3); !ok {
		t.Error("k3 should be present")
	}
}

func TestCoordValid(t *testing.T) {
	if !(Coord{Z: 0, X: 0, Y: 0}).Valid() {
		t.Error("0/0/0 valid")
	}
	if (Coord{Z: 1, X: 2, Y: 0}).Valid() {
		t.Error("1/2/0 out of range")
	}
	if (Coord{Z: 2, X: 3, Y: 3}).Valid() != true {
		t.Error("2/3/3 valid")
	}
}
