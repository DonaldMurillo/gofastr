package main

import (
	"fmt"
	"testing"
)

// TestParseAxeShard pins the shard knob's contract: empty is the whole gate,
// "k/n" with 0 <= k < n selects a slice, and every other spelling is an
// error. The error arm is the one that matters: a CI matrix entry with a
// typo must fail loudly, not scan an empty slice and report green.
func TestParseAxeShard(t *testing.T) {
	valid := map[string][2]int{
		"":    {0, 1},
		"0/1": {0, 1},
		"0/2": {0, 2},
		"1/2": {1, 2},
		"3/4": {3, 4},
	}
	for in, want := range valid {
		k, n, err := parseAxeShard(in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)
			continue
		}
		if k != want[0] || n != want[1] {
			t.Errorf("%q: got %d/%d, want %d/%d", in, k, n, want[0], want[1])
		}
	}
	for _, in := range []string{"2/2", "-1/2", "0/0", "1", "a/2", "1/b", "1/2/3", "/", " 0/2"} {
		if _, _, err := parseAxeShard(in); err == nil {
			t.Errorf("%q: accepted, want an error", in)
		}
	}
}

// TestAxeShardsPartitionTheWork proves the interleaved selection is a
// partition: over any item count, every item lands in exactly one shard.
// A shard left with nothing (more shards than items) reports an error and
// contributes no items; the partition still has to cover every item once.
func TestAxeShardsPartitionTheWork(t *testing.T) {
	for _, total := range []int{1, 2, 7, 100} {
		items := make([]pageResult, total)
		for i := range items {
			items[i] = pageResult{path: fmt.Sprintf("/%d", i)}
		}
		for shards := 1; shards <= 4; shards++ {
			seen := map[string]int{}
			for shard := 0; shard < shards; shard++ {
				got, err := shardItems(items, shard, shards)
				if err != nil {
					continue
				}
				for _, it := range got {
					seen[it.path]++
				}
			}
			for _, it := range items {
				if seen[it.path] != 1 {
					t.Fatalf("total=%d shards=%d: item %s selected %d times", total, shards, it.path, seen[it.path])
				}
			}
		}
	}
}

// TestShardItemsRefusesEmptySelection makes the empty-shard guard fail: one
// item under 1/2 leaves shard 1 nothing to scan, and that is an error, not
// a pass. Shard 0 of the same split gets the item.
func TestShardItemsRefusesEmptySelection(t *testing.T) {
	one := []pageResult{{path: "/"}}
	if got, err := shardItems(one, 1, 2); err == nil {
		t.Fatalf("shard 1/2 over one item: selected %d items and returned no error", len(got))
	}
	got, err := shardItems(one, 0, 2)
	if err != nil || len(got) != 1 {
		t.Fatalf("shard 0/2 over one item: got %d items, err %v", len(got), err)
	}
}
