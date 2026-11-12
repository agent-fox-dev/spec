package gate

import "testing"

// A recorded pass is found under its own key only: a key that differs in any
// one part finds nothing.
func TestStore_FindsAPassUnderItsKeyAlone(t *testing.T) {
	t.Setenv(StateEnv, t.TempDir())
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := Key{Tree: "t1", Dir: ".", Level: "full", Linter: "make lint", AllTests: "make test"}
	if err := s.Record(Pass{Key: key, Commit: "c1", At: "2026-11-12T10:00:00Z", Seconds: 2}); err != nil {
		t.Fatal(err)
	}
	if p, ok := s.Lookup(key); !ok || p.Commit != "c1" || p.Seconds != 2 {
		t.Fatalf("Lookup(key) = %+v, %v; want the recorded pass", p, ok)
	}
	for name, change := range map[string]func(k *Key){
		"the tree":              func(k *Key) { k.Tree = "t2" },
		"the directory":         func(k *Key) { k.Dir = "svc/a" },
		"the level":             func(k *Key) { k.Level = "task" },
		"the linter string":     func(k *Key) { k.Linter = "make lint -v" },
		"the all_tests string":  func(k *Key) { k.AllTests = "make test -v" },
		"the clean environment": func(k *Key) { k.CleanEnv = true },
	} {
		other := key
		change(&other)
		if p, ok := s.Lookup(other); ok {
			t.Errorf("%s changed: found %+v", name, p)
		}
	}
}
