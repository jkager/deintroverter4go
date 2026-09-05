package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyze(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": `module example.com/sample

go 1.25.0

require github.com/stretchr/testify v0.0.0
replace github.com/stretchr/testify => ./testify
`,
		"prod.go": `package sample
func Add(a,b int) int { return a+b }
`,
		"testify/go.mod": `module github.com/stretchr/testify

go 1.25.0
`,
		"testify/assert/assert.go": `package assert
type TestingT interface { Errorf(string,...interface{}) }

func Equal(t TestingT, expected, actual interface{}, msgAndArgs ...interface{}) bool { return true }
type Assertions struct{}

func New(t TestingT) *Assertions { return &Assertions{} }

func (a *Assertions) Equal(expected, actual interface{}, msgAndArgs ...interface{}) bool { return true }
`,
		"prod_test.go": `package sample
import (
 "testing"
 check "github.com/stretchr/testify/assert"
 "strings"
)
func local() int { return 2 }

func TestDirect(t *testing.T) { if Add(1,1)!=2 { t.Fatal("bad") } }

func TestDerived(t *testing.T) { got:=Add(1,1); doubled:=got*2; if doubled!=4 { t.Error("bad") } }

func TestLiteral(t *testing.T) { got:=2; if got!=2 { t.Fatal("bad") } }

func TestDiscarded(t *testing.T) { Add(1,1); if 2!=2 { t.Fatal("bad") } }

func TestDiagnostic(t *testing.T) { t.Errorf("got %d",Add(1,1)) }

func TestOverwritten(t *testing.T) { got:=Add(1,1); got=2; if got!=2 { t.Fatal("bad") } }

func TestShadowed(t *testing.T) { got:=Add(1,1); { got:=2; if got!=2 { t.Fatal("bad") } }; _=got }

func TestHelper(t *testing.T) { if local()!=2 { t.Fatal("bad") } }

func TestStdlib(t *testing.T) { if strings.ToUpper("a")!="A" { t.Fatal("bad") } }

func TestEmpty(t *testing.T) { Add(1,1) }

func TestBranch(t *testing.T) { got:=2; if testing.Short() { got=Add(1,1) }; if got!=2 { t.Fatal("bad") } }

func TestSubtests(t *testing.T) {
 got:=Add(1,1)
 t.Run("production",func(t *testing.T) { if got!=2 { t.Fatal("bad") } })
 t.Run("literal",func(t *testing.T) { if 2!=2 { t.Fatal("bad") } })
}

func TestTestify(t *testing.T) { check.Equal(t,2,Add(1,1)) }

func TestTestifyMessage(t *testing.T) { check.Equal(t,2,2,"got %d",Add(1,1)) }

func TestTestifyMethod(t *testing.T) { a:=check.New(t); a.Equal(2,Add(1,1)) }

func TestLoop(t *testing.T) { for _,x:=range []int{1,2} { if Add(x,1)!=x+1 { t.Fatal("bad") } } }

func Testhelper() {}
`,
		"external_test.go": `package sample_test
import (
 "testing"
 sut "example.com/sample"
)
func TestExternal(t *testing.T) { if sut.Add(1,1)!=2 { t.Fatal("bad") } }
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := Analyze(dir, []string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"TestDirect": "extroverted", "TestDerived": "extroverted", "TestLiteral": "introverted",
		"TestDiscarded": "introverted", "TestDiagnostic": "introverted", "TestOverwritten": "introverted",
		"TestShadowed": "introverted", "TestHelper": "introverted", "TestStdlib": "introverted",
		"TestSubtests": "container", "TestEmpty": "questionable", "TestBranch": "questionable", "TestSubtests/production": "extroverted",
		"TestSubtests/literal": "introverted", "TestTestify": "extroverted", "TestTestifyMessage": "introverted",
		"TestTestifyMethod": "extroverted", "TestLoop": "extroverted", "TestExternal": "extroverted",
	}
	if len(findings) != len(want) {
		t.Errorf("got %d findings, want %d: %+v", len(findings), len(want), findings)
	}
	for _, f := range findings {
		if want[f.Test] != f.Verdict {
			t.Errorf("%s: got %s, want %s (%s)", f.Test, f.Verdict, want[f.Test], f.Reason)
		}
		delete(want, f.Test)
	}
	for name := range want {
		t.Errorf("missing %s", name)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package sample\nfunc broken("), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(dir, []string{"./..."}); err == nil {
		t.Fatal("invalid source must return an error")
	}
}
