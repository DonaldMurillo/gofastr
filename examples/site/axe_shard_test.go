package main

import "testing"

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
// partition: over any item count, every index lands in exactly one shard.
func TestAxeShardsPartitionTheWork(t *testing.T) {
	for _, total := range []int{1, 2, 7, 100} {
		for shards := 1; shards <= 4; shards++ {
			seen := make([]int, total)
			for shard := 0; shard < shards; shard++ {
				for i := 0; i < total; i++ {
					if i%shards == shard {
						seen[i]++
					}
				}
			}
			for i, c := range seen {
				if c != 1 {
					t.Fatalf("total=%d shards=%d: item %d selected %d times", total, shards, i, c)
				}
			}
		}
	}
}
