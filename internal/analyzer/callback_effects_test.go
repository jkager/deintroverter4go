package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnresolvedCallbackEffects(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/callbacks\n\ngo 1.25.0\n",
		"prod.go": `package callbacks
func Add(a, b int) int { return a+b }
func Run(fn func()) { fn() }
type Writer interface { Write() }
type fanout []Writer
func NewFanout(writers ...Writer) Writer { return fanout(writers) }
func (writers fanout) Write() { for _, w := range writers { w.Write() } }
type Health struct { OnChange func(bool) }
func (h *Health) Set(healthy bool) { h.OnChange(healthy) }
type Hook struct { F func() }
func (h Hook) Fire() { h.F() }
type Empty struct{}
func (Empty) Each(fn func(int)) {}
`,
		"callback_test.go": `package callbacks_test
import (
 "testing"
 p "example.com/callbacks"
)
type writerFunc struct { f func() }
func (w writerFunc) Write() { w.f() }
func TestFanout(t *testing.T) {
 var calls []string
 a := writerFunc{f: func() { calls = append(calls, "a") }}
 b := writerFunc{f: func() { calls = append(calls, "b") }}
 p.NewFanout(a, b).Write()
 if len(calls) != 2 { t.Fatal("missing sink calls") }
}
func TestHealth(t *testing.T) {
 var states []bool
 h := &p.Health{OnChange: func(v bool) { states = append(states, v) }}
 h.Set(false)
 h.Set(true)
 if len(states) != 2 { t.Fatal("missing notifications") }
}
func TestEmpty(t *testing.T) {
 var m p.Empty
 called := false
 m.Each(func(int) { called = true })
 if called { t.Fatal("unexpected callback") }
}
func TestAssignedCallbackField(t *testing.T) {
 called := false
 var h p.Hook
 h.F = func() { called = true }
 h.Fire()
 if !called { t.Fatal("not called") }
}
func wrap(fn func()) p.Hook { return p.Hook{F: fn} }
func TestCallbackThroughHelper(t *testing.T) {
 called := false
 h := wrap(func() { called = true })
 h.Fire()
 if !called { t.Fatal("not called") }
}
func TestBoundCallback(t *testing.T) {
 called := false
 fn := func() { called = true }
 p.Run(fn)
 if !called { t.Fatal("not called") }
}
func TestUnrelatedValue(t *testing.T) {
 got := p.Add(1,1)
 called := false
 p.Run(func() { called = true })
 if got != 2 { t.Fatal("bad result") }
 if !called { t.Fatal("not called") }
}
func TestNotPassed(t *testing.T) {
 called := false
 fn := func() { called = true }
 _ = fn
 if called { t.Fatal("changed without being called") }
}
func TestOverwrittenCallback(t *testing.T) {
 called := false
 fn := func() { called = true }
 fn = func() {}
 p.Run(fn)
 if called { t.Fatal("discarded callback ran") }
}
func TestResetAfterCall(t *testing.T) {
 called := false
 p.Run(func() { called = true })
 called = false
 if called { t.Fatal("reset failed") }
}
func TestShadow(t *testing.T) {
 called := false
 p.Run(func() { called := false; called = true; _ = called })
 if called { t.Fatal("shadowed value changed") }
}
func TestReadOnlyScalar(t *testing.T) {
 called := false
 p.Run(func() { _ = called })
 if called { t.Fatal("read changed value") }
}
func TestCapturedAlias(t *testing.T) {
 first, second := false, false
 ptr := &first
 fn := func() { *ptr = true }
 ptr = &second
 p.Run(fn)
 if first { t.Fatal("first changed") }
 if !second { t.Fatal("second unchanged") }
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := Analyze(dir, []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string][]string{
		"TestFanout": {"questionable"}, "TestHealth": {"questionable"}, "TestEmpty": {"questionable"},
		"TestAssignedCallbackField": {"questionable"}, "TestCallbackThroughHelper": {"questionable"},
		"TestBoundCallback": {"questionable"}, "TestUnrelatedValue": {"extroverted", "questionable"},
		"TestNotPassed": {"introverted"}, "TestOverwrittenCallback": {"introverted"},
		"TestResetAfterCall": {"introverted"}, "TestShadow": {"introverted"}, "TestReadOnlyScalar": {"introverted"},
		"TestCapturedAlias": {"introverted", "questionable"},
	}
	for _, f := range findings {
		want, exists := expected[f.Test]
		if !exists {
			t.Errorf("unexpected entry %s", f.Test)
			continue
		}
		if len(f.Assertions) != len(want) {
			t.Errorf("%s: got %d assertions, want %d", f.Test, len(f.Assertions), len(want))
			continue
		}
		for i, a := range f.Assertions {
			if a.Verdict != want[i] {
				t.Errorf("%s assertion %d: got %s, want %s", f.Test, i, a.Verdict, want[i])
			}
			if want[i] == "questionable" {
				found := false
				for _, d := range a.Diagnostics {
					if d.Code == "callback-effect" && d.File != "" && d.Line > 0 {
						found = true
					}
				}
				if !found {
					t.Errorf("%s assertion %d: missing callback-effect evidence", f.Test, i)
				}
			}
		}
		delete(expected, f.Test)
	}
	for name := range expected {
		t.Errorf("missing entry %s", name)
	}
}
