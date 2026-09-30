package log

import "testing"

func add(r *Ring, msgs ...string) {
	for _, m := range msgs {
		r.Add(Entry{Message: m})
	}
}

func texts(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Message
	}
	return out
}

func TestRingTailWrap(t *testing.T) {
	r := NewRing(3)
	add(r, "a", "b", "c", "d") // "a" evicted
	got := texts(r.Tail(0))    // all
	want := []string{"b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("Tail len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Tail = %v, want %v", got, want)
		}
	}
	if last := texts(r.Tail(2)); len(last) != 2 || last[0] != "c" || last[1] != "d" {
		t.Fatalf("Tail(2) = %v, want [c d]", last)
	}
}

func TestRingSinceCursor(t *testing.T) {
	r := NewRing(10)
	add(r, "1", "2", "3")
	cur := r.Seq() // cursor after 3 entries
	add(r, "4", "5")
	got, newCur := r.Since(cur)
	if len(got) != 2 || got[0].Message != "4" || got[1].Message != "5" {
		t.Fatalf("Since = %v, want [4 5]", texts(got))
	}
	if newCur != r.Seq() {
		t.Fatalf("new cursor = %d, want %d", newCur, r.Seq())
	}
	// No new entries since newCur.
	if got, _ := r.Since(newCur); len(got) != 0 {
		t.Fatalf("Since(newCur) = %v, want empty", texts(got))
	}
}
